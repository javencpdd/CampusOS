package security

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
)

const (
	maxEgressRequestHeaderBytes  = 16 << 10
	maxEgressResponseHeaderBytes = 32 << 10
)

// EgressRequest is built by trusted host code after resource authorization.
// A Secret Broker may inject credentials into Headers; this broker never
// resolves Secret references or returns plaintext to a plugin.
type EgressRequest struct {
	Method  string
	URL     string
	Headers http.Header
	Body    []byte
	// DenyRedirects preserves a host approval bound to this exact target URL.
	// It is stricter than the origin-level redirect limit in EgressPolicy.
	DenyRedirects bool
}

type EgressResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

type egressResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type egressDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

// EgressBroker enforces one host-owned policy at URL, redirect, DNS, dial,
// transport, and response boundaries. Production construction does not allow
// callers to replace the resolver, dialer, or TLS verification configuration.
type EgressBroker struct {
	limits    egressLimits
	resolver  egressResolver
	dialer    egressDialer
	transport *http.Transport
}

func NewEgressBroker(policy EgressPolicy) (*EgressBroker, error) {
	return newEgressBroker(policy, net.DefaultResolver, &net.Dialer{}, nil)
}

// The injected constructor is package-private for deterministic loopback TLS
// tests. It must not be exposed through Host API, SDK, or runtime options.
func newEgressBroker(policy EgressPolicy, resolver egressResolver, dialer egressDialer, tlsConfig *tls.Config) (*EgressBroker, error) {
	limits, err := normalizeEgressPolicy(policy)
	if err != nil {
		return nil, err
	}
	if resolver == nil || dialer == nil {
		return nil, ErrEgressBadPolicy
	}
	broker := &EgressBroker{limits: limits, resolver: resolver, dialer: dialer}
	broker.transport = &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		DisableCompression:     true,
		MaxConnsPerHost:        1,
		MaxResponseHeaderBytes: maxEgressResponseHeaderBytes,
		ResponseHeaderTimeout:  limits.timeout,
		TLSHandshakeTimeout:    limits.timeout,
		TLSClientConfig:        tlsConfig,
		DialContext:            broker.dialContext,
		// Disable HTTP/2 multiplexing so a new request obtains a fresh DNS
		// answer and a newly validated destination IP.
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	return broker, nil
}

// Do returns a fully buffered, bounded response. It never exposes the raw
// network response stream, which would let callers bypass the body limit.
func (b *EgressBroker) Do(ctx context.Context, input EgressRequest) (EgressResponse, error) {
	if b == nil || b.transport == nil || input.Method == "" || len(input.Body) > int(b.limits.maxRequestBytes) {
		return EgressResponse{}, ErrEgressBadRequest
	}
	origin, err := canonicalEgressTarget(input.URL)
	if err != nil {
		return EgressResponse{}, ErrEgressDenied
	}
	if _, allowed := b.limits.origins[origin]; !allowed {
		return EgressResponse{}, ErrEgressDenied
	}
	if !validEgressRequestHeaders(input.Headers) {
		return EgressResponse{}, ErrEgressBadRequest
	}
	req, err := http.NewRequestWithContext(ctx, input.Method, input.URL, bytes.NewReader(input.Body))
	if err != nil {
		return EgressResponse{}, ErrEgressBadRequest
	}
	req.Header = input.Headers.Clone()
	client := &http.Client{
		Transport: b.transport,
		Timeout:   b.limits.timeout,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if input.DenyRedirects || len(via) > b.limits.maxRedirects {
				return ErrEgressRedirect
			}
			nextOrigin, err := canonicalEgressTarget(next.URL.String())
			if err != nil || nextOrigin != origin {
				// The header and body that may contain a Secret have not been
				// transmitted to the new target at this point.
				return ErrEgressRedirect
			}
			if _, allowed := b.limits.origins[nextOrigin]; !allowed {
				return ErrEgressRedirect
			}
			return nil
		},
	}
	response, err := client.Do(req)
	if err != nil {
		return EgressResponse{}, sanitizeEgressNetworkError(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, b.limits.maxResponseBytes+1))
	if err != nil {
		return EgressResponse{}, sanitizeEgressNetworkError(err)
	}
	if int64(len(body)) > b.limits.maxResponseBytes {
		return EgressResponse{}, ErrEgressTooLarge
	}
	return EgressResponse{StatusCode: response.StatusCode, Headers: response.Header.Clone(), Body: body}, nil
}

func validEgressRequestHeaders(headers http.Header) bool {
	var size int
	for key, values := range headers {
		if key == "" || strings.ContainsAny(key, "\r\n\x00") {
			return false
		}
		switch strings.ToLower(key) {
		case "host", "connection", "proxy-authorization", "proxy-connection", "transfer-encoding", "upgrade", "te", "trailer":
			return false
		}
		size += len(key)
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n\x00") {
				return false
			}
			size += len(value)
		}
		if size > maxEgressRequestHeaderBytes {
			return false
		}
	}
	return true
}

func (b *EgressBroker) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if b == nil || (network != "tcp" && network != "tcp4" && network != "tcp6") {
		return nil, ErrEgressDenied
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrEgressDenied
	}
	origin, err := canonicalEgressOrigin("https://" + net.JoinHostPort(host, port))
	if err != nil {
		return nil, ErrEgressDenied
	}
	if _, allowed := b.limits.origins[origin]; !allowed {
		return nil, ErrEgressDenied
	}
	addresses, err := b.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, ErrEgressUnsafeDNS
	}
	// Reject the whole answer if one record is unsafe. A mixed public/private
	// answer is a DNS rebinding attempt, not an acceptable fallback set.
	for _, address := range addresses {
		if address.Zone != "" || !safeEgressIP(address.IP) {
			return nil, ErrEgressUnsafeDNS
		}
	}
	for _, address := range addresses {
		connection, err := b.dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if err == nil {
			return connection, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("egress connection failed")
}

func sanitizeEgressNetworkError(err error) error {
	for _, known := range []error{context.Canceled, context.DeadlineExceeded, ErrEgressDenied, ErrEgressUnsafeDNS, ErrEgressTooLarge, ErrEgressRedirect} {
		if errors.Is(err, known) {
			return known
		}
	}
	return errors.New("egress request failed")
}
