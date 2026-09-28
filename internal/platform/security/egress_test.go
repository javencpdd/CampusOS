package security

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type egressTestResolver struct {
	mu      sync.Mutex
	answers [][]net.IPAddr
	calls   int
}

func (r *egressTestResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.calls
	r.calls++
	if index >= len(r.answers) {
		index = len(r.answers) - 1
	}
	return append([]net.IPAddr(nil), r.answers[index]...), nil
}

func (r *egressTestResolver) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

type egressTestDialer struct {
	mu           sync.Mutex
	actualTarget string
	requested    []string
}

func (d *egressTestDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.mu.Lock()
	d.requested = append(d.requested, address)
	d.mu.Unlock()
	return (&net.Dialer{}).DialContext(ctx, network, d.actualTarget)
}

func (d *egressTestDialer) destinations() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.requested...)
}

func egressIPs(raw ...string) []net.IPAddr {
	result := make([]net.IPAddr, 0, len(raw))
	for _, value := range raw {
		result = append(result, net.IPAddr{IP: net.ParseIP(value)})
	}
	return result
}

func egressTLSFixture(t *testing.T, handler http.Handler) (*httptest.Server, *tls.Config, string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "egress fixture CA"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "api.example.com"},
		DNSNames:  []string{"api.example.com"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{serverDER, caDER}, PrivateKey: serverKey}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	origin := "https://api.example.com:" + parsed.Port()
	return server, &tls.Config{RootCAs: roots}, origin
}

func newEgressTestBroker(t *testing.T, policy EgressPolicy, resolver *egressTestResolver, target string, tlsConfig *tls.Config) (*EgressBroker, *egressTestDialer) {
	t.Helper()
	dialer := &egressTestDialer{actualTarget: target}
	broker, err := newEgressBroker(policy, resolver, dialer, tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	return broker, dialer
}

func TestEgressBrokerLocalTLSAndFreshDNSPerConnection(t *testing.T) {
	var hits atomic.Int64
	var origin string
	server, tlsConfig, fixtureOrigin := egressTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.TLS == nil || r.Host != strings.TrimPrefix(origin, "https://") || r.Header.Get("Authorization") != "Bearer synthetic-test" {
			t.Errorf("unexpected TLS request host or header")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("accepted"))
	}))
	origin = fixtureOrigin
	resolver := &egressTestResolver{answers: [][]net.IPAddr{egressIPs("1.1.1.1"), egressIPs("1.1.1.1")}}
	broker, dialer := newEgressTestBroker(t, EgressPolicy{AllowedOrigins: []string{origin}}, resolver, server.Listener.Addr().String(), tlsConfig)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	for i := 0; i < 2; i++ {
		response, err := broker.Do(context.Background(), EgressRequest{
			Method: http.MethodPost, URL: origin + "/accept", Body: []byte("safe-test"),
			Headers: http.Header{"Authorization": {"Bearer synthetic-test"}},
		})
		if err != nil || response.StatusCode != http.StatusCreated || string(response.Body) != "accepted" {
			t.Fatalf("TLS broker request %d failed: response=%+v err=%v", i, response, err)
		}
	}
	if hits.Load() != 2 || resolver.callCount() != 2 {
		t.Fatalf("expected two TLS requests and two DNS resolutions, got requests=%d DNS=%d", hits.Load(), resolver.callCount())
	}
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	for _, address := range dialer.destinations() {
		if address != net.JoinHostPort("1.1.1.1", port) {
			t.Fatalf("dialed unvalidated destination %s", address)
		}
	}
}

func TestEgressBrokerDNSRebindingFailsBeforeDial(t *testing.T) {
	var hits atomic.Int64
	server, tlsConfig, origin := egressTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	resolver := &egressTestResolver{answers: [][]net.IPAddr{egressIPs("1.1.1.1"), egressIPs("127.0.0.1")}}
	broker, dialer := newEgressTestBroker(t, EgressPolicy{AllowedOrigins: []string{origin}}, resolver, server.Listener.Addr().String(), tlsConfig)
	request := EgressRequest{Method: http.MethodGet, URL: origin + "/resource"}
	if _, err := broker.Do(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Do(context.Background(), request); !errors.Is(err, ErrEgressUnsafeDNS) {
		t.Fatalf("DNS rebinding returned %v, want unsafe DNS", err)
	}
	if hits.Load() != 1 || resolver.callCount() != 2 || len(dialer.destinations()) != 1 {
		t.Fatalf("DNS rebinding reached network: hits=%d DNS=%d dials=%v", hits.Load(), resolver.callCount(), dialer.destinations())
	}
	mixed := &egressTestResolver{answers: [][]net.IPAddr{egressIPs("1.1.1.1", "10.0.0.1")}}
	mixedBroker, mixedDialer := newEgressTestBroker(t, EgressPolicy{AllowedOrigins: []string{origin}}, mixed, server.Listener.Addr().String(), tlsConfig)
	if _, err := mixedBroker.Do(context.Background(), request); !errors.Is(err, ErrEgressUnsafeDNS) || len(mixedDialer.destinations()) != 0 {
		t.Fatalf("mixed public/private answer was accepted: err=%v dials=%v", err, mixedDialer.destinations())
	}
}

func TestEgressBrokerRedirectNeverForwardsSecretCrossOrigin(t *testing.T) {
	var leaked atomic.Int64
	server, tlsConfig, origin := egressTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
		case "/cross":
			_, port, _ := net.SplitHostPort(r.Host)
			w.Header().Set("Location", "https://other.example.com:"+port+"/stolen")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/final":
			_, _ = w.Write([]byte("same-origin"))
		case "/stolen":
			leaked.Add(1)
		}
	}))
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	otherOrigin := "https://other.example.com:" + port
	resolver := &egressTestResolver{answers: [][]net.IPAddr{egressIPs("1.1.1.1")}}
	broker, dialer := newEgressTestBroker(t, EgressPolicy{AllowedOrigins: []string{origin, otherOrigin}, MaxRedirects: 1}, resolver, server.Listener.Addr().String(), tlsConfig)
	response, err := broker.Do(context.Background(), EgressRequest{Method: http.MethodGet, URL: origin + "/same"})
	if err != nil || string(response.Body) != "same-origin" || resolver.callCount() != 2 {
		t.Fatalf("same-Origin redirect failed: response=%+v err=%v DNS=%d", response, err, resolver.callCount())
	}
	_, err = broker.Do(context.Background(), EgressRequest{
		Method: http.MethodPost, URL: origin + "/cross", Body: []byte("synthetic-body-secret"),
		Headers: http.Header{"Authorization": {"Bearer synthetic-test"}, "X-API-Key": {"synthetic-header-secret"}},
	})
	if !errors.Is(err, ErrEgressRedirect) || leaked.Load() != 0 || len(dialer.destinations()) != 3 {
		t.Fatalf("cross-Origin redirect leaked or returned wrong error: err=%v leaked=%d dials=%v", err, leaked.Load(), dialer.destinations())
	}
	noRedirect, noRedirectDialer := newEgressTestBroker(t, EgressPolicy{AllowedOrigins: []string{origin}}, resolver, server.Listener.Addr().String(), tlsConfig)
	if _, err := noRedirect.Do(context.Background(), EgressRequest{Method: http.MethodGet, URL: origin + "/same"}); !errors.Is(err, ErrEgressRedirect) || len(noRedirectDialer.destinations()) != 1 {
		t.Fatalf("zero redirect limit was not enforced: err=%v dials=%v", err, noRedirectDialer.destinations())
	}
}

func TestEgressBrokerExactTargetRejectsSameOriginCredentialRedirect(t *testing.T) {
	var approvedHits atomic.Int64
	var echoHits atomic.Int64
	server, tlsConfig, origin := egressTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/approved":
			approvedHits.Add(1)
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer exact-target-secret" {
				t.Error("approved endpoint did not receive expected authenticated POST")
			}
			w.Header().Set("Location", "/unapproved-echo")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/unapproved-echo":
			echoHits.Add(1)
			_, _ = w.Write([]byte(r.Header.Get("Authorization")))
		default:
			t.Errorf("unexpected endpoint %q", r.URL.Path)
		}
	}))
	resolver := &egressTestResolver{answers: [][]net.IPAddr{egressIPs("1.1.1.1")}}
	broker, dialer := newEgressTestBroker(t, EgressPolicy{
		AllowedOrigins: []string{origin}, MaxRedirects: 1,
	}, resolver, server.Listener.Addr().String(), tlsConfig)
	_, err := broker.Do(context.Background(), EgressRequest{
		Method: http.MethodPost, URL: origin + "/approved", Body: []byte("{}"),
		Headers: http.Header{"Authorization": {"Bearer exact-target-secret"}}, DenyRedirects: true,
	})
	if !errors.Is(err, ErrEgressRedirect) || approvedHits.Load() != 1 || echoHits.Load() != 0 ||
		resolver.callCount() != 1 || len(dialer.destinations()) != 1 {
		t.Fatalf("exact target redirect escaped approval: err=%v approved=%d echo=%d DNS=%d dials=%v",
			err, approvedHits.Load(), echoHits.Load(), resolver.callCount(), dialer.destinations())
	}
}

func TestEgressBrokerBoundsAndTLSVerification(t *testing.T) {
	server, tlsConfig, origin := egressTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/large":
			_, _ = w.Write([]byte("12345"))
		case "/slow":
			<-r.Context().Done()
		case "/headers":
			w.Header().Set("X-Padding", strings.Repeat("x", 40<<10))
			_, _ = w.Write([]byte("ok"))
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	resolver := &egressTestResolver{answers: [][]net.IPAddr{egressIPs("1.1.1.1")}}
	broker, dialer := newEgressTestBroker(t, EgressPolicy{
		AllowedOrigins: []string{origin}, MaxRequestBytes: 4, MaxResponseBytes: 4,
		Timeout: 50 * time.Millisecond,
	}, resolver, server.Listener.Addr().String(), tlsConfig)
	if _, err := broker.Do(context.Background(), EgressRequest{Method: http.MethodPost, URL: origin + "/", Body: []byte("12345")}); !errors.Is(err, ErrEgressBadRequest) {
		t.Fatalf("oversized request returned %v", err)
	}
	if _, err := broker.Do(context.Background(), EgressRequest{Method: http.MethodGet, URL: origin + "/large"}); !errors.Is(err, ErrEgressTooLarge) {
		t.Fatalf("oversized response returned %v", err)
	}
	if _, err := broker.Do(context.Background(), EgressRequest{Method: http.MethodGet, URL: origin + "/slow"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out response returned %v", err)
	}
	if _, err := broker.Do(context.Background(), EgressRequest{Method: http.MethodGet, URL: origin + "/headers"}); err == nil {
		t.Fatal("oversized response headers were accepted")
	}
	if len(dialer.destinations()) != 3 {
		t.Fatalf("preflight request limit still dialed: %v", dialer.destinations())
	}
	if _, err := broker.Do(context.Background(), EgressRequest{Method: http.MethodGet, URL: origin + "/" + strings.Repeat("x", maxEgressURLBytes)}); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("oversized URL returned %v", err)
	}
	if _, err := broker.Do(context.Background(), EgressRequest{Method: http.MethodGet, URL: "https://127.0.0.1/"}); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("literal IP target returned %v", err)
	}
	if _, err := broker.Do(context.Background(), EgressRequest{Method: http.MethodGet, URL: origin + "/", Headers: http.Header{"Host": {"attacker"}}}); !errors.Is(err, ErrEgressBadRequest) {
		t.Fatalf("Host override returned %v", err)
	}
	badTLS := tlsConfig.Clone()
	badTLS.RootCAs = x509.NewCertPool()
	badBroker, _ := newEgressTestBroker(t, EgressPolicy{AllowedOrigins: []string{origin}}, resolver, server.Listener.Addr().String(), badTLS)
	if _, err := badBroker.Do(context.Background(), EgressRequest{Method: http.MethodGet, URL: origin + "/"}); err == nil {
		t.Fatal("untrusted TLS certificate was accepted")
	}
}

func TestEgressBrokerDefaultDeny(t *testing.T) {
	broker, err := NewEgressBroker(EgressPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Do(context.Background(), EgressRequest{Method: http.MethodGet, URL: "https://api.example.com/"}); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("empty approval set returned %v, want denial", err)
	}
}
