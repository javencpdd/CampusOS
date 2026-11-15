package security

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestApprovedHTTPProxyPinsPublicIPAndKeepsCredentialsOffTheOrigin(t *testing.T) {
	var originHits, proxyHits atomic.Int64
	origin, tlsConfig, allowed := egressTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits.Add(1)
		if r.Header.Get("Proxy-Authorization") != "" || r.Header.Get("Authorization") != "" || r.TLS == nil {
			t.Error("proxy credentials or plaintext leaked to origin")
		}
		_, _ = w.Write([]byte("proxied content"))
	}))
	_, port, _ := net.SplitHostPort(origin.Listener.Addr().String())
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		if r.Method != "CONNECT" || r.Host != net.JoinHostPort("1.1.1.1", port) || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("fixture:owned-password")) {
			t.Error("proxy tunnel not pinned or authenticated")
		}
		remote, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		defer remote.Close()
		client, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer client.Close()
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
		_ = buffer.Flush()
		go func() { _, _ = io.Copy(client, remote); client.Close() }()
		_, _ = io.Copy(remote, buffer)
	}))
	defer proxyServer.Close()
	endpoint, _ := url.Parse(strings.Replace(proxyServer.URL, "http://", "http://fixture:owned-password@", 1))
	resolver := &egressTestResolver{answers: [][]net.IPAddr{egressIPs("1.1.1.1"), egressIPs("127.0.0.1")}}
	broker, err := newEgressBroker(EgressPolicy{AllowedOrigins: []string{allowed}}, resolver, &approvedProxyDialer{endpoint: endpoint}, tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	response, err := broker.Do(context.Background(), EgressRequest{Method: "GET", URL: allowed + "/document", DenyRedirects: true})
	if err != nil || string(response.Body) != "proxied content" {
		t.Fatal("HTTP CONNECT failed", err)
	}
	if _, err = broker.Do(context.Background(), EgressRequest{Method: "GET", URL: allowed + "/document"}); !errors.Is(err, ErrEgressUnsafeDNS) {
		t.Fatal("proxy bypassed private DNS rejection", err)
	}
	if originHits.Load() != 1 || proxyHits.Load() != 1 {
		t.Fatal("unsafe retry or direct fallback")
	}
}

func TestApprovedSOCKSProxyReceivesAnIPNotATargetHostname(t *testing.T) {
	origin, tlsConfig, allowed := egressTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy auth leaked")
		}
		_, _ = w.Write([]byte("socks content"))
	}))
	_, port, _ := net.SplitHostPort(origin.Listener.Addr().String())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	requested := make(chan string, 1)
	fail := make(chan error, 1)
	go func() {
		client, err := listener.Accept()
		if err != nil {
			fail <- err
			return
		}
		defer client.Close()
		var greeting [2]byte
		if _, err = io.ReadFull(client, greeting[:]); err != nil {
			fail <- err
			return
		}
		methods := make([]byte, int(greeting[1]))
		if _, err = io.ReadFull(client, methods); err != nil {
			fail <- err
			return
		}
		_, _ = client.Write([]byte{5, 2})
		var credentials [2]byte
		if _, err = io.ReadFull(client, credentials[:]); err != nil || credentials[0] != 1 {
			fail <- errors.New("SOCKS authentication missing")
			return
		}
		username := make([]byte, int(credentials[1]))
		_, _ = io.ReadFull(client, username)
		var length [1]byte
		_, _ = io.ReadFull(client, length[:])
		password := make([]byte, int(length[0]))
		_, _ = io.ReadFull(client, password)
		if string(username) != "fixture" || string(password) != "owned-password" {
			fail <- errors.New("incorrect SOCKS credentials")
			return
		}
		_, _ = client.Write([]byte{1, 0})
		var command [4]byte
		if _, err = io.ReadFull(client, command[:]); err != nil {
			fail <- err
			return
		}
		if command[0] != 5 || command[1] != 1 || command[3] != 1 {
			fail <- errors.New("SOCKS target was not a pinned IPv4")
			return
		}
		var address [6]byte
		if _, err = io.ReadFull(client, address[:]); err != nil {
			fail <- err
			return
		}
		requested <- net.JoinHostPort(net.IP(address[:4]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(address[4:]))))
		if binary.BigEndian.Uint16(address[4:]) == 0 {
			fail <- errors.New("SOCKS port missing")
			return
		}
		remote, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			fail <- err
			return
		}
		defer remote.Close()
		_, _ = client.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
		go func() { _, _ = io.Copy(client, remote); client.Close() }()
		_, _ = io.Copy(remote, client)
	}()
	endpoint, _ := url.Parse("socks5://fixture:owned-password@" + listener.Addr().String())
	resolver := &egressTestResolver{answers: [][]net.IPAddr{egressIPs("1.1.1.1")}}
	broker, err := newEgressBroker(EgressPolicy{AllowedOrigins: []string{allowed}}, resolver, &approvedProxyDialer{endpoint: endpoint}, tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	response, err := broker.Do(context.Background(), EgressRequest{Method: "GET", URL: allowed + "/document"})
	if err != nil || string(response.Body) != "socks content" {
		select {
		case detail := <-fail:
			t.Fatal("SOCKS fixture", detail)
		default:
			t.Fatal("SOCKS request", err)
		}
	}
	if target := <-requested; target != net.JoinHostPort("1.1.1.1", port) {
		t.Fatal("SOCKS DNS bypass", target)
	}
}

func TestApprovedHTTPProxyCancellationClosesAnIncompleteTunnel(t *testing.T) {
	connected := make(chan struct{})
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer client.Close()
		close(connected)
		_, _ = io.Copy(io.Discard, client)
		close(closed)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		conn, err := (&approvedProxyDialer{endpoint: endpoint}).DialContext(ctx, "tcp", "1.1.1.1:443")
		if conn != nil {
			conn.Close()
		}
		done <- err
	}()
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("proxy was not contacted")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled tunnel succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("proxy handshake ignored cancellation")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("canceled proxy connection leaked")
	}
}

func TestApprovedHTTPSProxyMustPassCertificateVerification(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	conn, err := (&approvedProxyDialer{endpoint: endpoint}).DialContext(context.Background(), "tcp", "1.1.1.1:443")
	if conn != nil {
		conn.Close()
	}
	if err == nil || requests.Load() != 0 {
		t.Fatal("untrusted proxy certificate was accepted")
	}
}

func TestProxyProfilesExposeOnlyIDsLabelsAndProtocols(t *testing.T) {
	registry, err := NewProxyRegistry(`[{"id":"clash","label":"Clash","url":"http://fixture:owned-password@127.0.0.1:7890"}]`)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(registry.Profiles())
	if strings.Contains(string(metadata), "owned-password") || strings.Contains(string(metadata), "127.0.0.1") || strings.Contains(string(metadata), "url") {
		t.Fatal("proxy configuration leaked", string(metadata))
	}
	if _, err = registry.NewBroker(EgressPolicy{AllowedOrigins: []string{"https://example.com"}}, "missing"); !errors.Is(err, ErrEgressDenied) {
		t.Fatal("unregistered proxy accepted")
	}
	for _, raw := range []string{`[{"id":"x","label":"bad","url":"file:///etc/passwd"}]`, `[{"id":"x","label":"bad","url":"http://127.0.0.1:0"}]`, `[{"id":"x","label":"bad","url":"http://127.0.0.1:7890/path"}]`, `[{"id":"x","label":"bad","url":"socks5h://127.0.0.1:7890"}]`, `[{"id":"x","label":"bad","url":"http://127.0.0.1:7890","owner":"forged"}]`} {
		if _, err := NewProxyRegistry(raw); err == nil {
			t.Fatal("invalid proxy profile accepted")
		}
	}
}
