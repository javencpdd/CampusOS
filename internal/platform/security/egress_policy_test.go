package security

import (
	"net"
	"testing"
	"time"
)

func TestEgressPolicyExactHTTPSOrigin(t *testing.T) {
	for _, invalid := range []string{
		"", "http://api.example.com", "https://user:pass@api.example.com",
		"https://api.example.com/", "https://api.example.com/path",
		"https://api.example.com?x=1", "https://api.example.com#fragment",
		"https://*.example.com", "https://api.example.com:0", "https://api.example.com:", "https://api.example.com:65536",
		"https://127.0.0.1", "https://[::1]", "https://localhost",
		"https://api.internal", "https://api.example.com.", " https://api.example.com",
	} {
		if _, err := canonicalEgressOrigin(invalid); err == nil {
			t.Fatalf("accepted invalid Origin %q", invalid)
		}
	}
	for raw, want := range map[string]string{
		"https://api.example.com":      "https://api.example.com",
		"https://API.example.com:443":  "https://api.example.com",
		"https://api.example.com:8443": "https://api.example.com:8443",
	} {
		if got, err := canonicalEgressOrigin(raw); err != nil || got != want {
			t.Fatalf("canonical Origin %q = %q, %v; want %q", raw, got, err, want)
		}
	}
	policy, err := normalizeEgressPolicy(EgressPolicy{AllowedOrigins: []string{"https://api.example.com"}})
	if err != nil || len(policy.origins) != 1 || policy.maxRequestBytes <= 0 || policy.maxResponseBytes <= 0 || policy.timeout <= 0 {
		t.Fatalf("valid policy normalization failed: %+v, %v", policy, err)
	}
	for _, raw := range []string{"https://evil.example.com/path", "http://api.example.com/path", "https://user@api.example.com/path", "https://api.example.com:8443/path", "https://127.0.0.1/path"} {
		if got, err := canonicalEgressTarget(raw); err == nil && got == "https://api.example.com" {
			t.Fatalf("unsafe target %q matched approved Origin", raw)
		}
	}
}

func TestEgressPolicyRejectsUnboundedLimits(t *testing.T) {
	for _, policy := range []EgressPolicy{
		{MaxRequestBytes: -1}, {MaxResponseBytes: -1},
		{MaxRequestBytes: 16<<20 + 1}, {MaxResponseBytes: 16<<20 + 1},
		{Timeout: -time.Second}, {Timeout: time.Hour},
		{MaxRedirects: -1}, {MaxRedirects: 6},
	} {
		if _, err := normalizeEgressPolicy(policy); err == nil {
			t.Fatalf("accepted invalid resource limit: %+v", policy)
		}
	}
}

func TestEgressPolicyRejectsPrivateAndSpecialUseDNSResults(t *testing.T) {
	for _, raw := range []string{
		"0.0.0.0", "10.1.2.3", "100.64.1.1", "127.0.0.1",
		"169.254.169.254", "172.16.0.1", "192.168.1.1", "192.0.2.1",
		"198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1",
		"::1", "fc00::1", "fe80::1", "fec0::1", "100::1", "100:0:0:1::1",
		"2001::1", "2001:1::1", "2001:2::1", "2001:10::1", "2001:20::1", "2001:30::1",
		"2001:db8::1", "2002::1", "3fff::1", "5f00::1", "64:ff9b::a00:1",
		"::ffff:127.0.0.1",
	} {
		if safeEgressIP(net.ParseIP(raw)) {
			t.Fatalf("accepted non-public DNS result %s", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "::ffff:8.8.8.8", "2606:4700:4700::1111", "2001:4860:4860::8888"} {
		if !safeEgressIP(net.ParseIP(raw)) {
			t.Fatalf("rejected public DNS result %s", raw)
		}
	}
}
