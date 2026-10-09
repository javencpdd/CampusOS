package port

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

const ScopeDelegationContract = "campusos.scope-delegation/v1"

var ErrScopeInputInvalid = errors.New("scope.input_invalid")

// ScopeDelegationDenyError carries the stable reason of a denied delegation
// proposal. The reason codes are the registered catalog entries; an accepted
// proposal is reported as a nil error, never as a synthetic allow object.
type ScopeDelegationDenyError struct {
	Reason string
}

func (e *ScopeDelegationDenyError) Error() string { return e.Reason }

// ScopeDelegationActor is the simplified G1 actor shape for this contract: it
// is not a full PrincipalContext and proves nothing about authentication by
// itself. The host constructs it after verifying credentials and MFA.
type ScopeDelegationActor struct {
	Kind                   string `json:"kind"`
	ID                     string `json:"id"`
	AuthenticationStrength string `json:"authentication_strength"`
}

type ScopeDelegationManagement struct {
	Action      string `json:"action"`
	Active      bool   `json:"active"`
	ExpiresAtMS int64  `json:"expires_at_ms"`
}

// ScopeDelegationScope is an exact ID set. Only public_collection,
// system_config and endpoint exist; there is no wildcard or parent expansion.
type ScopeDelegationScope struct {
	Kind string   `json:"kind"`
	IDs  []string `json:"ids"`
}

type ScopeDelegationBound struct {
	Action           string               `json:"action"`
	Scope            ScopeDelegationScope `json:"scope"`
	NotBeforeMS      int64                `json:"not_before_ms"`
	ExpiresAtMS      int64                `json:"expires_at_ms"`
	RequiredStrength string               `json:"required_strength"`
	Status           string               `json:"status"`
	Delegable        bool                 `json:"delegable"`
}

type ScopeDelegationRecipient struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type ScopeDelegationCandidate struct {
	Action           string                   `json:"action"`
	Scope            ScopeDelegationScope     `json:"scope"`
	NotBeforeMS      int64                    `json:"not_before_ms"`
	ExpiresAtMS      int64                    `json:"expires_at_ms"`
	RequiredStrength string                   `json:"required_strength"`
	Recipient        ScopeDelegationRecipient `json:"recipient"`
}

// ScopeDelegationInput is constructed by the host from current facts. The
// browser or plugin may only propose candidate intent; management authority,
// bounds, the clock and facts_version are never client-supplied trusted values.
type ScopeDelegationInput struct {
	Contract     string                    `json:"contract"`
	Actor        ScopeDelegationActor      `json:"actor"`
	Management   ScopeDelegationManagement `json:"management"`
	Bound        ScopeDelegationBound      `json:"bound"`
	Candidate    ScopeDelegationCandidate  `json:"candidate"`
	NowMS        int64                     `json:"now_ms"`
	FactsVersion string                    `json:"facts_version"`
}

// ScopeDelegationPolicy evaluates one candidate against one current bound.
// A nil error means the proposal is acceptable; it is still not a committed
// grant, and the caller must re-read facts inside the write transaction.
type ScopeDelegationPolicy interface {
	CheckScopeDelegation(ScopeDelegationInput) error
}

// validScopeDelegationString mirrors the schema's 1..128 length, which Ajv
// counts as UTF-16 code units rather than bytes or runes.
func validScopeDelegationString(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	units := 0
	for _, r := range s {
		if r > 0xffff {
			units += 2
		} else {
			units++
		}
	}
	return units >= 1 && units <= 128
}

func knownScopeDelegationAction(action string) bool {
	switch action {
	case "knowledge.source.read", "plugin.config.system.read", "integration.webhook.invoke", "personal.document.read":
		return true
	default:
		return false
	}
}

func (s ScopeDelegationScope) Validate() error {
	if s.Kind != "public_collection" && s.Kind != "system_config" && s.Kind != "endpoint" {
		return ErrScopeInputInvalid
	}
	if s.IDs == nil || len(s.IDs) < 1 || len(s.IDs) > 32 {
		return ErrScopeInputInvalid
	}
	seen := make(map[string]bool, len(s.IDs))
	for _, id := range s.IDs {
		if !validScopeDelegationString(id) || seen[id] {
			return ErrScopeInputInvalid
		}
		seen[id] = true
	}
	return nil
}

func (a ScopeDelegationActor) Validate() error {
	if (a.Kind != "user" && a.Kind != "admin") || !validScopeDelegationString(a.ID) ||
		(a.AuthenticationStrength != "password" && a.AuthenticationStrength != "mfa") {
		return ErrScopeInputInvalid
	}
	return nil
}

func (m ScopeDelegationManagement) Validate() error {
	switch m.Action {
	case "identity.role.assign", "plugin.grant.manage", "integration.grant.manage":
	default:
		return ErrScopeInputInvalid
	}
	if m.ExpiresAtMS < 1 {
		return ErrScopeInputInvalid
	}
	return nil
}

func validScopeDelegationAtom(action string, scope ScopeDelegationScope, notBeforeMS, expiresAtMS int64, strength string) error {
	if !knownScopeDelegationAction(action) || scope.Validate() != nil || notBeforeMS < 0 || expiresAtMS < 1 ||
		(strength != "password" && strength != "mfa") {
		return ErrScopeInputInvalid
	}
	return nil
}

func (b ScopeDelegationBound) Validate() error {
	if validScopeDelegationAtom(b.Action, b.Scope, b.NotBeforeMS, b.ExpiresAtMS, b.RequiredStrength) != nil ||
		(b.Status != "active" && b.Status != "suspended" && b.Status != "revoked") {
		return ErrScopeInputInvalid
	}
	return nil
}

func (c ScopeDelegationCandidate) Validate() error {
	if validScopeDelegationAtom(c.Action, c.Scope, c.NotBeforeMS, c.ExpiresAtMS, c.RequiredStrength) != nil {
		return ErrScopeInputInvalid
	}
	r := c.Recipient
	if (r.Kind != "user" && r.Kind != "integration" && r.Kind != "plugin") || !validScopeDelegationString(r.ID) {
		return ErrScopeInputInvalid
	}
	return nil
}

// Validate enforces the structural G1 schema. This contract has no separate
// unsupported-version code: any contract mismatch is plain input_invalid.
func (r ScopeDelegationInput) Validate() error {
	if r.Contract != ScopeDelegationContract || r.Actor.Validate() != nil || r.Management.Validate() != nil ||
		r.Bound.Validate() != nil || r.Candidate.Validate() != nil || r.NowMS < 0 || !validScopeDelegationString(r.FactsVersion) {
		return ErrScopeInputInvalid
	}
	return nil
}

// scopeDelegationMillis decodes an exact non-negative integer millisecond
// value. Fractional or overflowing values are rejected, never rounded into a
// grant window.
func scopeDelegationMillis(raw []byte) (int64, error) {
	value, err := resourcePolicyMillis(raw)
	if err != nil {
		return 0, ErrScopeInputInvalid
	}
	return value, nil
}

func parseScopeDelegationScope(raw []byte) (ScopeDelegationScope, error) {
	fields, err := strictObject(raw, []string{"kind", "ids"}, nil)
	if err != nil {
		return ScopeDelegationScope{}, ErrScopeInputInvalid
	}
	var s ScopeDelegationScope
	if json.Unmarshal(fields["kind"], &s.Kind) != nil || json.Unmarshal(fields["ids"], &s.IDs) != nil {
		return ScopeDelegationScope{}, ErrScopeInputInvalid
	}
	for _, id := range s.IDs {
		if !validScopeDelegationString(id) {
			return ScopeDelegationScope{}, ErrScopeInputInvalid
		}
	}
	if err = s.Validate(); err != nil {
		return ScopeDelegationScope{}, err
	}
	return s, nil
}

// ParseScopeDelegationInputJSON is a bounded host codec, not an ingress
// authorization endpoint. Errors never echo caller-supplied data.
func ParseScopeDelegationInputJSON(raw []byte) (ScopeDelegationInput, error) {
	if !resourceJSONUnicodeValid(raw) {
		return ScopeDelegationInput{}, ErrScopeInputInvalid
	}
	fields, err := strictObject(raw, []string{"contract", "actor", "management", "bound", "candidate", "now_ms", "facts_version"}, nil)
	if err != nil {
		return ScopeDelegationInput{}, ErrScopeInputInvalid
	}
	var r ScopeDelegationInput
	for key, dst := range map[string]*string{"contract": &r.Contract, "facts_version": &r.FactsVersion} {
		if json.Unmarshal(fields[key], dst) != nil {
			return ScopeDelegationInput{}, ErrScopeInputInvalid
		}
	}
	if r.NowMS, err = scopeDelegationMillis(fields["now_ms"]); err != nil {
		return ScopeDelegationInput{}, err
	}
	if r.Actor, err = parseScopeDelegationActor(fields["actor"]); err != nil {
		return ScopeDelegationInput{}, err
	}
	if r.Management, err = parseScopeDelegationManagement(fields["management"]); err != nil {
		return ScopeDelegationInput{}, err
	}
	if r.Bound, err = parseScopeDelegationBound(fields["bound"]); err != nil {
		return ScopeDelegationInput{}, err
	}
	if r.Candidate, err = parseScopeDelegationCandidate(fields["candidate"]); err != nil {
		return ScopeDelegationInput{}, err
	}
	if err = r.Validate(); err != nil {
		return ScopeDelegationInput{}, err
	}
	return r, nil
}

func parseScopeDelegationActor(raw []byte) (ScopeDelegationActor, error) {
	fields, err := strictObject(raw, []string{"kind", "id", "authentication_strength"}, nil)
	if err != nil {
		return ScopeDelegationActor{}, ErrScopeInputInvalid
	}
	var a ScopeDelegationActor
	for key, dst := range map[string]*string{"kind": &a.Kind, "id": &a.ID, "authentication_strength": &a.AuthenticationStrength} {
		if json.Unmarshal(fields[key], dst) != nil {
			return ScopeDelegationActor{}, ErrScopeInputInvalid
		}
	}
	if err = a.Validate(); err != nil {
		return ScopeDelegationActor{}, err
	}
	return a, nil
}

func parseScopeDelegationManagement(raw []byte) (ScopeDelegationManagement, error) {
	fields, err := strictObject(raw, []string{"action", "active", "expires_at_ms"}, nil)
	if err != nil {
		return ScopeDelegationManagement{}, ErrScopeInputInvalid
	}
	var m ScopeDelegationManagement
	if json.Unmarshal(fields["action"], &m.Action) != nil || json.Unmarshal(fields["active"], &m.Active) != nil {
		return ScopeDelegationManagement{}, ErrScopeInputInvalid
	}
	if m.ExpiresAtMS, err = scopeDelegationMillis(fields["expires_at_ms"]); err != nil {
		return ScopeDelegationManagement{}, err
	}
	if err = m.Validate(); err != nil {
		return ScopeDelegationManagement{}, err
	}
	return m, nil
}

func parseScopeDelegationBound(raw []byte) (ScopeDelegationBound, error) {
	fields, err := strictObject(raw, []string{"action", "scope", "not_before_ms", "expires_at_ms", "required_strength", "status", "delegable"}, nil)
	if err != nil {
		return ScopeDelegationBound{}, ErrScopeInputInvalid
	}
	var b ScopeDelegationBound
	for key, dst := range map[string]*string{"action": &b.Action, "required_strength": &b.RequiredStrength, "status": &b.Status} {
		if json.Unmarshal(fields[key], dst) != nil {
			return ScopeDelegationBound{}, ErrScopeInputInvalid
		}
	}
	if b.Scope, err = parseScopeDelegationScope(fields["scope"]); err != nil {
		return ScopeDelegationBound{}, err
	}
	if b.NotBeforeMS, err = scopeDelegationMillis(fields["not_before_ms"]); err != nil {
		return ScopeDelegationBound{}, err
	}
	if b.ExpiresAtMS, err = scopeDelegationMillis(fields["expires_at_ms"]); err != nil {
		return ScopeDelegationBound{}, err
	}
	if json.Unmarshal(fields["delegable"], &b.Delegable) != nil || b.Validate() != nil {
		return ScopeDelegationBound{}, ErrScopeInputInvalid
	}
	return b, nil
}

func parseScopeDelegationCandidate(raw []byte) (ScopeDelegationCandidate, error) {
	fields, err := strictObject(raw, []string{"action", "scope", "not_before_ms", "expires_at_ms", "required_strength", "recipient"}, nil)
	if err != nil {
		return ScopeDelegationCandidate{}, ErrScopeInputInvalid
	}
	var c ScopeDelegationCandidate
	for key, dst := range map[string]*string{"action": &c.Action, "required_strength": &c.RequiredStrength} {
		if json.Unmarshal(fields[key], dst) != nil {
			return ScopeDelegationCandidate{}, ErrScopeInputInvalid
		}
	}
	if c.Scope, err = parseScopeDelegationScope(fields["scope"]); err != nil {
		return ScopeDelegationCandidate{}, err
	}
	if c.NotBeforeMS, err = scopeDelegationMillis(fields["not_before_ms"]); err != nil {
		return ScopeDelegationCandidate{}, err
	}
	if c.ExpiresAtMS, err = scopeDelegationMillis(fields["expires_at_ms"]); err != nil {
		return ScopeDelegationCandidate{}, err
	}
	recipient, err := strictObject(fields["recipient"], []string{"kind", "id"}, nil)
	if err != nil {
		return ScopeDelegationCandidate{}, ErrScopeInputInvalid
	}
	for key, dst := range map[string]*string{"kind": &c.Recipient.Kind, "id": &c.Recipient.ID} {
		if json.Unmarshal(recipient[key], dst) != nil {
			return ScopeDelegationCandidate{}, ErrScopeInputInvalid
		}
	}
	if err = c.Validate(); err != nil {
		return ScopeDelegationCandidate{}, err
	}
	return c, nil
}
