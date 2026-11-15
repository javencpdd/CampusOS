package webcompanion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/security"
)

// Client only authenticates to the selected CampusOS origin. Source requests
// use a separate Egress Broker with no companion or CampusOS credentials.
type Client struct {
	base      string
	http      *http.Client
	token     string
	proxies   *security.ProxyRegistry
	ProxyMode string
}

func NewClient(server string) (*Client, error) { return NewClientWithProxy(server, "") }
func NewClientWithProxy(server, localProxy string) (*Client, error) {
	mode := "direct"
	var proxies *security.ProxyRegistry
	if localProxy != "" {
		raw, _ := json.Marshal([]map[string]string{{"id": "local", "label": "Local proxy", "url": localProxy}})
		var err error
		proxies, err = security.NewProxyRegistry(string(raw))
		if err != nil {
			return nil, ErrDenied
		}
		mode = proxies.Profiles()[0].Protocol
	}
	u, err := url.Parse(strings.TrimRight(server, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrDenied
	}
	if u.Scheme != "https" {
		ip, e := netip.ParseAddr(u.Hostname())
		if u.Scheme != "http" || e != nil || !ip.IsPrivate() && !ip.IsLoopback() {
			return nil, ErrDenied
		}
	}
	return &Client{base: u.String() + "/api/v1/web-companion/", proxies: proxies, ProxyMode: mode, http: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 14 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}}, nil
}
func (c *Client) post(ctx context.Context, path string, input, out any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+path, bytes.NewReader(raw))
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return ErrDenied
	}
	if response.StatusCode != 200 {
		return ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(body) > 8192 {
		return ErrUnavailable
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Code != 0 {
		return ErrUnavailable
	}
	if out != nil && json.Unmarshal(envelope.Data, out) != nil {
		return ErrUnavailable
	}
	return nil
}
func (c *Client) Connect(ctx context.Context, code, name string) error {
	var result struct {
		Token string `json:"token"`
	}
	if err := c.post(ctx, "connect", map[string]string{"pairing_code": code, "name": name, "proxy_mode": c.ProxyMode}, &result); err != nil {
		return err
	}
	if len(result.Token) != 64 {
		return ErrUnavailable
	}
	c.token = result.Token
	return nil
}
func ReadPublic(ctx context.Context, target string, maxBytes int64) ([]byte, error) {
	return readPublicVia(ctx, target, maxBytes, nil)
}
func readPublicVia(ctx context.Context, target string, maxBytes int64, proxies *security.ProxyRegistry) ([]byte, error) {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || maxBytes < 1 || maxBytes > 1<<20 {
		return nil, ErrDenied
	}
	profile := ""
	if proxies != nil {
		profile = "local"
	}
	broker, err := proxies.NewBroker(security.EgressPolicy{AllowedOrigins: []string{u.Scheme + "://" + u.Host}, MaxRequestBytes: 1024, MaxResponseBytes: maxBytes, Timeout: 8 * time.Second, MaxRedirects: 0}, profile)
	if err != nil {
		return nil, ErrDenied
	}
	response, err := broker.Do(ctx, security.EgressRequest{Method: "GET", URL: target, DenyRedirects: true, Headers: http.Header{"Accept": []string{"*/*"}, "User-Agent": []string{"CampusOS-Web-Companion/1.0"}}})
	if err != nil || response.StatusCode != 200 {
		return nil, ErrUnavailable
	}
	return response.Body, nil
}
func (c *Client) Run(ctx context.Context, notice func(string)) error {
	if len(c.token) != 64 {
		return ErrDenied
	}
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() { cancel(); wg.Wait() }()
	slots := make(chan struct{}, 2)
	failures := 0
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		var result struct {
			Job *Job `json:"job"`
		}
		err := c.post(ctx, "poll", struct{}{}, &result)
		if err != nil {
			<-slots
			if errors.Is(err, ErrDenied) {
				return err
			}
			failures++
			if failures >= 3 {
				return ErrUnavailable
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}
		failures = 0
		if result.Job == nil {
			<-slots
			continue
		}
		job := *result.Job
		if len(job.ID) != 32 || job.MaxBytes < 1 || job.MaxBytes > 1<<20 {
			<-slots
			return ErrDenied
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			if notice != nil {
				notice("正在通过本机读取公开内容或图片…")
			}
			body, err := readPublicVia(ctx, job.URL, job.MaxBytes, c.proxies)
			input := struct {
				JobID  string `json:"job_id"`
				Bytes  []byte `json:"bytes"`
				Failed bool   `json:"failed"`
			}{job.ID, body, err != nil}
			if err != nil {
				input.Bytes = []byte{}
			}
			if err = c.post(ctx, "result", input, nil); err != nil && notice != nil {
				notice("本次读取未能交付；请在页面检查连接状态。")
			}
		}()
	}
}
