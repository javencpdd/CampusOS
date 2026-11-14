package security

import (
	"context"
	"net"
	"net/url"
)

// ValidatePublicHTTPS performs a preflight without making an HTTP request.
// Consumers on another machine must independently validate their own DNS/dial.
func ValidatePublicHTTPS(ctx context.Context, target string) error {
	u, err := url.Parse(target)
	if err != nil || len(target) > 2048 || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return ErrEgressDenied
	}
	if _, err = canonicalEgressTarget(target); err != nil {
		return ErrEgressDenied
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil || len(addresses) == 0 {
		return ErrEgressUnsafeDNS
	}
	for _, address := range addresses {
		if address.Zone != "" || !safeEgressIP(address.IP) {
			return ErrEgressUnsafeDNS
		}
	}
	return nil
}
