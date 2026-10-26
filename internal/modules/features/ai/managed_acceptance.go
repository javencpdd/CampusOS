//go:build campusos_acceptance

package ai

import (
	"context"
	"crypto/tls"
	"github.com/campusos/CampusOS/internal/platform/security"
	"net"
)

// AcceptanceRemote is omitted from production builds. The acceptance host
// uses the real shared Egress with deterministic public DNS and a pinned local
// TLS peer; no permissive production option or model response mock is added.
func (m *Module) AcceptanceRemote(resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}, dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}, config *tls.Config) {
	m.managed.remote = func(policy security.EgressPolicy) (managedEgress, error) {
		return security.NewAcceptanceEgress(policy, resolver, dialer, config)
	}
}
