package port

import (
	"encoding/json"
	"errors"
)

const IdentityDomainsContract = "campusos.identity-domains/v1"

var ErrIdentityDomainInvalid = errors.New("identity.domain_invalid")

// IdentityDomainDenyError carries the stable reason of a rejected identity
// domain evaluation. Acceptance is reported as a nil error, never as a
// synthetic allow object.
type IdentityDomainDenyError struct {
	Reason string
}

func (e *IdentityDomainDenyError) Error() string { return e.Reason }

// IdentityDomainConfig pins one domain's signing and storage identity. Two
// domains sharing any of these values are not isolated.
type IdentityDomainConfig struct {
	Issuer          string `json:"issuer"`
	Audience        string `json:"audience"`
	KeyID           string `json:"key_id"`
	CredentialStore string `json:"credential_store"`
	SessionStore    string `json:"session_store"`
	RecoveryStore   string `json:"recovery_store"`
	RoleStore       string `json:"role_store"`
}

type IdentityDomainPair struct {
	User  IdentityDomainConfig `json:"user"`
	Admin IdentityDomainConfig `json:"admin"`
}

// IdentityDomainRequest describes one credential or challenge consumption.
// RecoveryStore and RoleStore are optional on the wire; evaluation only trusts
// the credential/session/issuer/audience/key binding.
type IdentityDomainRequest struct {
	Kind             string `json:"kind"`
	AccountID        string `json:"account_id"`
	Issuer           string `json:"issuer"`
	Audience         string `json:"audience"`
	KeyID            string `json:"key_id"`
	CredentialStore  string `json:"credential_store"`
	SessionStore     string `json:"session_store"`
	RecoveryStore    string `json:"recovery_store,omitempty"`
	RoleStore        string `json:"role_store,omitempty"`
	ChallengeKind    string `json:"challenge_kind"`
	ChallengeDomain  string `json:"challenge_domain"`
	TargetAccountID  string `json:"target_account_id"`
	Status           string `json:"status"`
	ActivationSource string `json:"activation_source"`
}

// IdentityDomainOperation is one admin lifecycle command. Only the host's
// current facts may construct it: security-admin counts and the bootstrap flag
// are never client-supplied.
type IdentityDomainOperation struct {
	Kind                  string `json:"kind"`
	ActorKind             string `json:"actor_kind"`
	ActorMFA              bool   `json:"actor_mfa"`
	TargetAdminID         string `json:"target_admin_id"`
	TargetIsSecurityAdmin bool   `json:"target_is_security_admin"`
	ActiveSecurityAdmins  int64  `json:"active_security_admins"`
	BootstrapEnabled      bool   `json:"bootstrap_enabled"`
	AuditReason           string `json:"audit_reason"`
}

type IdentityDomainInput struct {
	Contract  string                   `json:"contract"`
	Domains   IdentityDomainPair       `json:"domains"`
	Request   IdentityDomainRequest    `json:"request"`
	Operation *IdentityDomainOperation `json:"operation,omitempty"`
}

// IdentityDomainPolicy evaluates domain isolation, cross-domain credential and
// challenge binding, and admin lifecycle rules from host-supplied facts only.
type IdentityDomainPolicy interface {
	EvaluateIdentityDomain(IdentityDomainInput) error
}

func (c IdentityDomainConfig) Validate() error {
	for _, value := range []string{c.Issuer, c.Audience, c.KeyID, c.CredentialStore, c.SessionStore, c.RecoveryStore, c.RoleStore} {
		if !validScopeDelegationString(value) {
			return ErrIdentityDomainInvalid
		}
	}
	return nil
}

func (r IdentityDomainRequest) Validate() error {
	if r.Kind != "user" && r.Kind != "admin" {
		return ErrIdentityDomainInvalid
	}
	for _, value := range []string{r.AccountID, r.Issuer, r.Audience, r.KeyID, r.CredentialStore, r.SessionStore, r.TargetAccountID} {
		if !validScopeDelegationString(value) {
			return ErrIdentityDomainInvalid
		}
	}
	if r.RecoveryStore != "" && !validScopeDelegationString(r.RecoveryStore) {
		return ErrIdentityDomainInvalid
	}
	if r.RoleStore != "" && !validScopeDelegationString(r.RoleStore) {
		return ErrIdentityDomainInvalid
	}
	switch r.ChallengeKind {
	case "access", "refresh", "mfa", "email", "recovery":
	default:
		return ErrIdentityDomainInvalid
	}
	if r.ChallengeDomain != "user" && r.ChallengeDomain != "admin" {
		return ErrIdentityDomainInvalid
	}
	switch r.Status {
	case "active", "suspended", "revoked":
	default:
		return ErrIdentityDomainInvalid
	}
	switch r.ActivationSource {
	case "direct", "bootstrap", "user_role":
	default:
		return ErrIdentityDomainInvalid
	}
	return nil
}

func (o IdentityDomainOperation) Validate() error {
	switch o.Kind {
	case "bootstrap", "create", "suspend", "revoke", "restore":
	default:
		return ErrIdentityDomainInvalid
	}
	if o.ActorKind != "user" && o.ActorKind != "admin" && o.ActorKind != "system" {
		return ErrIdentityDomainInvalid
	}
	if !validScopeDelegationString(o.TargetAdminID) || o.ActiveSecurityAdmins < 0 ||
		!validScopeDelegationString(o.AuditReason) || len(o.AuditReason) > 300 {
		return ErrIdentityDomainInvalid
	}
	return nil
}

func (r IdentityDomainInput) Validate() error {
	if r.Contract != IdentityDomainsContract || r.Domains.User.Validate() != nil || r.Domains.Admin.Validate() != nil ||
		r.Request.Validate() != nil {
		return ErrIdentityDomainInvalid
	}
	if r.Operation != nil && r.Operation.Validate() != nil {
		return ErrIdentityDomainInvalid
	}
	return nil
}

func parseIdentityDomainConfig(raw []byte) (IdentityDomainConfig, error) {
	fields, err := strictObject(raw, []string{"issuer", "audience", "key_id", "credential_store", "session_store", "recovery_store", "role_store"}, nil)
	if err != nil {
		return IdentityDomainConfig{}, ErrIdentityDomainInvalid
	}
	var c IdentityDomainConfig
	for key, dst := range map[string]*string{
		"issuer": &c.Issuer, "audience": &c.Audience, "key_id": &c.KeyID,
		"credential_store": &c.CredentialStore, "session_store": &c.SessionStore,
		"recovery_store": &c.RecoveryStore, "role_store": &c.RoleStore,
	} {
		if json.Unmarshal(fields[key], dst) != nil {
			return IdentityDomainConfig{}, ErrIdentityDomainInvalid
		}
	}
	if err = c.Validate(); err != nil {
		return IdentityDomainConfig{}, err
	}
	return c, nil
}

func parseIdentityDomainRequest(raw []byte) (IdentityDomainRequest, error) {
	fields, err := strictObject(raw,
		[]string{"kind", "account_id", "issuer", "audience", "key_id", "credential_store", "session_store", "challenge_kind", "challenge_domain", "target_account_id", "status", "activation_source"},
		[]string{"recovery_store", "role_store"})
	if err != nil {
		return IdentityDomainRequest{}, ErrIdentityDomainInvalid
	}
	var r IdentityDomainRequest
	for key, dst := range map[string]*string{
		"kind": &r.Kind, "account_id": &r.AccountID, "issuer": &r.Issuer, "audience": &r.Audience,
		"key_id": &r.KeyID, "credential_store": &r.CredentialStore, "session_store": &r.SessionStore,
		"challenge_kind": &r.ChallengeKind, "challenge_domain": &r.ChallengeDomain,
		"target_account_id": &r.TargetAccountID, "status": &r.Status, "activation_source": &r.ActivationSource,
	} {
		if json.Unmarshal(fields[key], dst) != nil {
			return IdentityDomainRequest{}, ErrIdentityDomainInvalid
		}
	}
	// Optional fields: when present they must still satisfy minLength 1; an
	// empty string is only legal as an absent field.
	for _, key := range []string{"recovery_store", "role_store"} {
		if value, exists := fields[key]; exists {
			var target *string
			if key == "recovery_store" {
				target = &r.RecoveryStore
			} else {
				target = &r.RoleStore
			}
			if json.Unmarshal(value, target) != nil || !validScopeDelegationString(*target) {
				return IdentityDomainRequest{}, ErrIdentityDomainInvalid
			}
		}
	}
	if err = r.Validate(); err != nil {
		return IdentityDomainRequest{}, err
	}
	return r, nil
}

func parseIdentityDomainOperation(raw []byte) (*IdentityDomainOperation, error) {
	fields, err := strictObject(raw, []string{"kind", "actor_kind", "actor_mfa", "target_admin_id", "target_is_security_admin", "active_security_admins", "bootstrap_enabled", "audit_reason"}, nil)
	if err != nil {
		return nil, ErrIdentityDomainInvalid
	}
	var o IdentityDomainOperation
	for key, dst := range map[string]*string{"kind": &o.Kind, "actor_kind": &o.ActorKind, "target_admin_id": &o.TargetAdminID, "audit_reason": &o.AuditReason} {
		if json.Unmarshal(fields[key], dst) != nil {
			return nil, ErrIdentityDomainInvalid
		}
	}
	if json.Unmarshal(fields["actor_mfa"], &o.ActorMFA) != nil || json.Unmarshal(fields["target_is_security_admin"], &o.TargetIsSecurityAdmin) != nil ||
		json.Unmarshal(fields["bootstrap_enabled"], &o.BootstrapEnabled) != nil {
		return nil, ErrIdentityDomainInvalid
	}
	count, err := resourcePolicyMillis(fields["active_security_admins"])
	if err != nil {
		return nil, ErrIdentityDomainInvalid
	}
	o.ActiveSecurityAdmins = count
	if err = o.Validate(); err != nil {
		return nil, err
	}
	return &o, nil
}

// ParseIdentityDomainInputJSON is a bounded host codec, not an ingress
// endpoint. This contract has no unsupported-version code: any contract
// mismatch is plain domain_invalid.
func ParseIdentityDomainInputJSON(raw []byte) (IdentityDomainInput, error) {
	if !resourceJSONUnicodeValid(raw) {
		return IdentityDomainInput{}, ErrIdentityDomainInvalid
	}
	fields, err := strictObject(raw, []string{"contract", "domains", "request"}, []string{"operation"})
	if err != nil {
		return IdentityDomainInput{}, ErrIdentityDomainInvalid
	}
	var r IdentityDomainInput
	if json.Unmarshal(fields["contract"], &r.Contract) != nil {
		return IdentityDomainInput{}, ErrIdentityDomainInvalid
	}
	if r.Domains.User, err = parseIdentityDomainConfigField(fields["domains"], "user"); err != nil {
		return IdentityDomainInput{}, err
	}
	if r.Domains.Admin, err = parseIdentityDomainConfigField(fields["domains"], "admin"); err != nil {
		return IdentityDomainInput{}, err
	}
	if r.Request, err = parseIdentityDomainRequest(fields["request"]); err != nil {
		return IdentityDomainInput{}, err
	}
	if operation, exists := fields["operation"]; exists {
		if r.Operation, err = parseIdentityDomainOperation(operation); err != nil {
			return IdentityDomainInput{}, err
		}
	}
	if err = r.Validate(); err != nil {
		return IdentityDomainInput{}, err
	}
	return r, nil
}

func parseIdentityDomainConfigField(domainsRaw []byte, name string) (IdentityDomainConfig, error) {
	fields, err := strictObject(domainsRaw, []string{"user", "admin"}, nil)
	if err != nil {
		return IdentityDomainConfig{}, ErrIdentityDomainInvalid
	}
	return parseIdentityDomainConfig(fields[name])
}
