package security

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrEgressDenied     = errors.New("egress destination is not allowed")
	ErrEgressUnsafeDNS  = errors.New("egress DNS result is not allowed")
	ErrEgressTooLarge   = errors.New("egress data exceeds its size limit")
	ErrEgressRedirect   = errors.New("egress redirect is not allowed")
	ErrEgressBadPolicy  = errors.New("egress policy is invalid")
	ErrEgressBadRequest = errors.New("egress request is invalid")
)

// EgressPolicy is the already-approved, host-owned resource limit for one
// caller. It is not itself an authorization decision: the caller must still
// check package declaration, deployment ceiling, administrator grant, and
// current instance state before constructing it.
type EgressPolicy struct {
	AllowedOrigins   []string
	MaxRequestBytes  int64
	MaxResponseBytes int64
	Timeout          time.Duration
	MaxRedirects     int
}

type egressLimits struct {
	origins          map[string]struct{}
	maxRequestBytes  int64
	maxResponseBytes int64
	timeout          time.Duration
	maxRedirects     int
}

const (
	defaultEgressRequestBytes  = 1 << 20
	defaultEgressResponseBytes = 2 << 20
	maxEgressBodyBytes         = 16 << 20
	defaultEgressTimeout       = 10 * time.Second
	maxEgressTimeout           = 60 * time.Second
	maxEgressRedirects         = 5
	maxEgressURLBytes          = 8 << 10
)

func normalizeEgressPolicy(policy EgressPolicy) (egressLimits, error) {
	limits := egressLimits{
		origins:          make(map[string]struct{}, len(policy.AllowedOrigins)),
		maxRequestBytes:  policy.MaxRequestBytes,
		maxResponseBytes: policy.MaxResponseBytes,
		timeout:          policy.Timeout,
		maxRedirects:     policy.MaxRedirects,
	}
	if limits.maxRequestBytes == 0 {
		limits.maxRequestBytes = defaultEgressRequestBytes
	}
	if limits.maxResponseBytes == 0 {
		limits.maxResponseBytes = defaultEgressResponseBytes
	}
	if limits.timeout == 0 {
		limits.timeout = defaultEgressTimeout
	}
	if limits.maxRequestBytes < 1 || limits.maxRequestBytes > maxEgressBodyBytes ||
		limits.maxResponseBytes < 1 || limits.maxResponseBytes > maxEgressBodyBytes ||
		limits.timeout < time.Millisecond || limits.timeout > maxEgressTimeout ||
		limits.maxRedirects < 0 || limits.maxRedirects > maxEgressRedirects ||
		len(policy.AllowedOrigins) > 64 {
		return egressLimits{}, ErrEgressBadPolicy
	}
	for _, raw := range policy.AllowedOrigins {
		origin, err := canonicalEgressOrigin(raw)
		if err != nil {
			return egressLimits{}, ErrEgressBadPolicy
		}
		limits.origins[origin] = struct{}{}
	}
	return limits, nil
}

func canonicalEgressOrigin(raw string) (string, error) {
	if raw == "" || len(raw) > maxEgressURLBytes || raw != strings.TrimSpace(raw) || !strings.HasPrefix(raw, "https://") {
		return "", ErrEgressDenied
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || parsed.RawFragment != "" || parsed.Opaque != "" || parsed.User != nil {
		return "", ErrEgressDenied
	}
	// An Origin has no path, query, or fragment, including an empty trailing
	// slash, question mark, or hash. URL.Parse alone accepts some of these.
	if strings.ContainsAny(strings.TrimPrefix(raw, "https://"), "/?#") {
		return "", ErrEgressDenied
	}
	return canonicalEgressURL(parsed)
}

func canonicalEgressTarget(raw string) (string, error) {
	if raw == "" || len(raw) > maxEgressURLBytes || raw != strings.TrimSpace(raw) || strings.Contains(raw, "#") {
		return "", ErrEgressDenied
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Opaque != "" || parsed.User != nil ||
		parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", ErrEgressDenied
	}
	return canonicalEgressURL(parsed)
}

func canonicalEgressURL(parsed *url.URL) (string, error) {
	if parsed == nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || strings.HasSuffix(parsed.Host, ":") {
		return "", ErrEgressDenied
	}
	host := strings.ToLower(parsed.Hostname())
	if !validEgressDNSName(host) {
		return "", ErrEgressDenied
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	} else {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return "", ErrEgressDenied
		}
		port = strconv.FormatUint(value, 10)
	}
	if port == "443" {
		return "https://" + host, nil
	}
	return "https://" + net.JoinHostPort(host, port), nil
}

func validEgressDNSName(host string) bool {
	if len(host) < 4 || len(host) > 253 || net.ParseIP(host) != nil || strings.HasSuffix(host, ".") {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".home.arpa") || strings.HasSuffix(host, ".lan") {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if ch < 'a' || ch > 'z' {
				if ch < '0' || ch > '9' {
					if ch != '-' {
						return false
					}
				}
			}
		}
	}
	return true
}

// IsGlobalUnicast also accepts IPv6 outside the allocated public 2000::/3
// space. Restrict public Egress to that space, then exclude special-purpose
// allocations. The full IETF protocol block is denied conservatively, even
// where a more-specific special service is globally reachable.
// Registry: https://www.iana.org/assignments/iana-ipv6-special-registry/
var publicEgressIPv6Prefix = netip.MustParsePrefix("2000::/3")

var excludedEgressPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

func safeEgressIP(value net.IP) bool {
	address, ok := netip.AddrFromSlice(value)
	if !ok {
		return false
	}
	address = address.Unmap()
	if address.Is6() && !publicEgressIPv6Prefix.Contains(address) {
		return false
	}
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range excludedEgressPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
