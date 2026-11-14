package security

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

type ProxyProfileInfo struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Protocol string `json:"protocol"`
}

// ProxyRegistry contains operator-owned connection settings. URLs and proxy
// credentials are never accepted from plugin requests or exported as metadata.
type ProxyRegistry struct {
	profiles map[string]*url.URL
	info     []ProxyProfileInfo
}

var proxyProfileID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func NewProxyRegistry(raw string) (*ProxyRegistry, error) {
	r := &ProxyRegistry{profiles: map[string]*url.URL{}, info: []ProxyProfileInfo{}}
	if strings.TrimSpace(raw) == "" {
		return r, nil
	}
	if len(raw) > 16384 {
		return nil, ErrEgressBadPolicy
	}
	var items []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		URL   string `json:"url"`
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&items) != nil || items == nil || len(items) > 16 {
		return nil, ErrEgressBadPolicy
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, ErrEgressBadPolicy
	}
	for _, item := range items {
		u, err := url.Parse(item.URL)
		if err != nil || !proxyProfileID.MatchString(item.ID) || item.Label == "" || len([]rune(item.Label)) > 80 || len(item.URL) > 2048 ||
			u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" ||
			(u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") || r.profiles[item.ID] != nil {
			return nil, ErrEgressBadPolicy
		}
		if u.User != nil {
			password, _ := u.User.Password()
			if len(u.User.Username()) > 255 || len(password) > 255 || strings.Contains(u.User.Username(), ":") {
				return nil, ErrEgressBadPolicy
			}
		}
		port := u.Port()
		if port == "" {
			switch u.Scheme {
			case "http":
				port = "80"
			case "https":
				port = "443"
			case "socks5":
				port = "1080"
			}
		}
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return nil, ErrEgressBadPolicy
		}
		u.Host = net.JoinHostPort(u.Hostname(), port)
		r.profiles[item.ID] = u
		r.info = append(r.info, ProxyProfileInfo{item.ID, item.Label, u.Scheme})
	}
	return r, nil
}
func (r *ProxyRegistry) Profiles() []ProxyProfileInfo {
	if r == nil {
		return []ProxyProfileInfo{}
	}
	return append([]ProxyProfileInfo{}, r.info...)
}
func (r *ProxyRegistry) Has(id string) bool { return id == "" || r != nil && r.profiles[id] != nil }
func (r *ProxyRegistry) NewBroker(policy EgressPolicy, id string) (*EgressBroker, error) {
	if id == "" {
		return NewEgressBroker(policy)
	}
	if !r.Has(id) {
		return nil, ErrEgressDenied
	}
	return newEgressBroker(policy, net.DefaultResolver, &approvedProxyDialer{endpoint: r.profiles[id]}, nil)
}

// Receives only the public IP already selected by EgressBroker.dialContext.
// The proxy cannot resolve the target hostname itself to bypass DNS checks.
type approvedProxyDialer struct{ endpoint *url.URL }

func (p *approvedProxyDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if p == nil || p.endpoint == nil {
		return nil, ErrEgressDenied
	}
	u := p.endpoint
	if u.Scheme == "socks5" {
		var auth *proxy.Auth
		if u.User != nil {
			password, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: password}
		}
		dialer, err := proxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{Timeout: 8 * time.Second})
		if err != nil {
			return nil, ErrEgressDenied
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, ErrEgressDenied
		}
		return contextDialer.DialContext(ctx, network, address)
	}
	connection, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	accepted := false
	defer func() {
		if !accepted {
			connection.Close()
		}
	}()
	rawConnection := connection
	stop := context.AfterFunc(ctx, func() { rawConnection.Close() })
	defer stop()
	deadline := time.Now().Add(8 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	if err = connection.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		tunnel := tls.Client(connection, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err = tunnel.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		connection = tunnel
	}
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Host: address}, Host: address, Header: make(http.Header)}
	if u.User != nil {
		password, _ := u.User.Password()
		request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+password)))
	}
	if err = request.Write(connection); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(connection, 4096)
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return nil, ErrEgressDenied
	}
	status := strings.Fields(string(line))
	if len(status) < 2 || (status[0] != "HTTP/1.1" && status[0] != "HTTP/1.0") || status[1] != "200" {
		return nil, ErrEgressDenied
	}
	total := len(line)
	for {
		line, err = reader.ReadSlice('\n')
		total += len(line)
		if err != nil || total > 16<<10 {
			return nil, ErrEgressDenied
		}
		if string(line) == "\r\n" {
			break
		}
	}
	if err = connection.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	accepted = true
	return &proxyBufferedConnection{Conn: connection, reader: reader}, nil
}

type proxyBufferedConnection struct {
	net.Conn
	reader *bufio.Reader
}

func (c *proxyBufferedConnection) Read(buffer []byte) (int, error) { return c.reader.Read(buffer) }
