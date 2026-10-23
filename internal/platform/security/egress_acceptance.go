//go:build campusos_acceptance

package security

import (
	"context"
	"crypto/tls"
	"net"
)

// NewAcceptanceEgress exists only in explicitly tagged acceptance binaries.
// Production construction, HTTP/Host API and SDK cannot inject DNS/dial/TLS.
func NewAcceptanceEgress(policy EgressPolicy, resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}, dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}, tlsConfig *tls.Config) (*EgressBroker, error) {
	return newEgressBroker(policy, resolver, dialer, tlsConfig)
}
