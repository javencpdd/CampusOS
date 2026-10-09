package policy

import (
	"regexp"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// ScopeDelegationPolicy implements the campusos.scope-delegation/v1 judgement
// for non-board scopes using only host-supplied current facts. Its zero value
// requires no cache, repository, credential verifier or ambient clock. A nil
// result means the proposal is acceptable under one complete current bound; it
// is never a committed grant.
type ScopeDelegationPolicy struct{}

var _ port.ScopeDelegationPolicy = ScopeDelegationPolicy{}

var (
	scopeDelegationEndpointShape = regexp.MustCompile(`^https://[a-z0-9.-]+(:[1-9][0-9]*)?$`)
	scopeDelegationEndpointIP    = regexp.MustCompile(`https://[0-9.]+`)
)

// scopeDelegationActionScope maps each delegable action to its only Scope
// kind. personal.document.read has no entry: it is never delegable.
var scopeDelegationActionScope = map[string]string{
	"knowledge.source.read":      "public_collection",
	"plugin.config.system.read":  "system_config",
	"integration.webhook.invoke": "endpoint",
}

// CheckScopeDelegation applies the frozen judgement order: actor domain and
// MFA, management authority matching the recipient domain, the non-delegable
// private-document action, user recipients (handled by the board governance
// contract instead), bound currency, then exact action/scope/window/strength
// containment and per-ID endpoint shape. No conditions from other bounds or
// grants are combined; unknown facts deny closed.
func (ScopeDelegationPolicy) CheckScopeDelegation(r port.ScopeDelegationInput) error {
	if err := r.Validate(); err != nil {
		return err
	}
	deny := func(reason string) error { return &port.ScopeDelegationDenyError{Reason: reason} }
	if r.Actor.Kind != "admin" || r.Actor.AuthenticationStrength != "mfa" {
		return deny("scope.actor_denied")
	}
	requiredManagement := "identity.role.assign"
	switch r.Candidate.Recipient.Kind {
	case "plugin":
		requiredManagement = "plugin.grant.manage"
	case "integration":
		requiredManagement = "integration.grant.manage"
	}
	if r.Management.Action != requiredManagement || !r.Management.Active || r.Management.ExpiresAtMS <= r.NowMS {
		return deny("scope.management_denied")
	}
	if r.Candidate.Action == "personal.document.read" || r.Bound.Action == "personal.document.read" {
		return deny("scope.non_delegable")
	}
	if r.Candidate.Recipient.Kind == "user" {
		return deny("scope.recipient_mismatch")
	}
	if r.Bound.Status != "active" || !r.Bound.Delegable || r.Bound.NotBeforeMS > r.NowMS || r.Bound.ExpiresAtMS <= r.NowMS {
		return deny("scope.bound_inactive")
	}
	c, b := r.Candidate, r.Bound
	notBeforeFloor := r.NowMS
	if b.NotBeforeMS > notBeforeFloor {
		notBeforeFloor = b.NotBeforeMS
	}
	if scopeDelegationActionScope[c.Action] != c.Scope.Kind || scopeDelegationActionScope[b.Action] != b.Scope.Kind ||
		c.Action != b.Action || c.Scope.Kind != b.Scope.Kind ||
		c.NotBeforeMS < notBeforeFloor || c.ExpiresAtMS > b.ExpiresAtMS || c.ExpiresAtMS <= c.NotBeforeMS ||
		(b.RequiredStrength == "mfa" && c.RequiredStrength != "mfa") {
		return deny("scope.outside_bounds")
	}
	for _, id := range c.Scope.IDs {
		found := false
		for _, boundID := range b.Scope.IDs {
			if id == boundID {
				found = true
				break
			}
		}
		if !found {
			return deny("scope.outside_bounds")
		}
	}
	if c.Scope.Kind == "endpoint" {
		for _, id := range c.Scope.IDs {
			if !scopeDelegationEndpointShape.MatchString(id) || strings.Contains(id, "*") ||
				strings.Contains(id, "localhost") || scopeDelegationEndpointIP.MatchString(id) {
				return deny("scope.outside_bounds")
			}
		}
	}
	return nil
}
