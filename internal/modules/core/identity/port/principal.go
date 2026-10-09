package port

import (
	"encoding/json"
	"errors"
)

const PrincipalContract = "campusos.principal/v1"

var (
	ErrPrincipalInvalid             = errors.New("principal.context_invalid")
	ErrPrincipalContractUnsupported = errors.New("principal.contract_unsupported")
)

// PrincipalRef identifies an actor in its own domain. Equal user/admin IDs do
// not denote the same principal. Anonymous has no ID.
type PrincipalRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

// PrincipalContext is a host-side fact DTO, not a credential or authentication
// result. Only a trusted domain adapter may construct it after authenticating
// and checking current credential/delegation state. Parsing it grants nothing.
type PrincipalContext struct {
	Contract               string        `json:"contract"`
	Actor                  PrincipalRef  `json:"actor"`
	Audience               string        `json:"audience"`
	AuthenticationStrength string        `json:"authentication_strength"`
	CredentialID           string        `json:"credential_id,omitempty"`
	EffectiveSubject       *PrincipalRef `json:"effective_subject,omitempty"`
	DelegationID           string        `json:"delegation_id,omitempty"`
}

func validOpaqueID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
			continue
		}
		if i > 0 && (ch == '.' || ch == '_' || ch == ':' || ch == '-') {
			continue
		}
		return false
	}
	return true
}

// Validate checks the exact v1 domain combinations, without looking up a user,
// a resource, a credential, a grant or a delegation. Workload authentication is
// not treated as stronger than MFA; each domain has its own allowed strengths.
func (p PrincipalContext) Validate() error {
	if p.Contract != PrincipalContract {
		if p.Contract == "" {
			return ErrPrincipalInvalid
		}
		return ErrPrincipalContractUnsupported
	}
	if p.Actor.Kind == "anonymous" {
		if p.Actor.ID != "" || p.Audience != "public" || p.AuthenticationStrength != "none" ||
			p.CredentialID != "" || p.EffectiveSubject != nil || p.DelegationID != "" {
			return ErrPrincipalInvalid
		}
		return nil
	}
	if !validOpaqueID(p.Actor.ID) || !validOpaqueID(p.CredentialID) {
		return ErrPrincipalInvalid
	}
	switch p.Actor.Kind {
	case "user", "admin":
		if p.Audience != p.Actor.Kind || (p.AuthenticationStrength != "password" && p.AuthenticationStrength != "mfa") {
			return ErrPrincipalInvalid
		}
	case "integration":
		if p.Audience != "integration_api" || p.AuthenticationStrength != "credential" {
			return ErrPrincipalInvalid
		}
	case "plugin_instance":
		if p.Audience != "host_api" || p.AuthenticationStrength != "workload" {
			return ErrPrincipalInvalid
		}
	case "worker":
		if p.Audience != "worker" || p.AuthenticationStrength != "workload" {
			return ErrPrincipalInvalid
		}
	default:
		return ErrPrincipalInvalid
	}
	if p.EffectiveSubject != nil || p.DelegationID != "" {
		if (p.Actor.Kind != "plugin_instance" && p.Actor.Kind != "worker") ||
			p.EffectiveSubject == nil || p.EffectiveSubject.Kind != "user" ||
			!validOpaqueID(p.EffectiveSubject.ID) || !validOpaqueID(p.DelegationID) {
			return ErrPrincipalInvalid
		}
	}
	return nil
}

// ParsePrincipalJSON validates a single bounded exact-schema object. It
// rejects explicit null/empty optional fields rather than collapsing them into
// absent values through Go's JSON zero values. Errors never echo input.
func ParsePrincipalJSON(raw []byte) (PrincipalContext, error) {
	var p PrincipalContext
	fields, err := decodeStrictObject(raw)
	if err != nil {
		return p, ErrPrincipalInvalid
	}
	var version string
	if value, exists := fields["contract"]; exists && json.Unmarshal(value, &version) == nil && version != PrincipalContract {
		// JSON null is not a string, even though unmarshalling into string
		// succeeds; only a genuine unsupported string gets this code.
		var scalar any
		if json.Unmarshal(value, &scalar) == nil {
			if _, ok := scalar.(string); ok {
				return p, ErrPrincipalContractUnsupported
			}
		}
	}
	fields, err = strictObject(raw,
		[]string{"contract", "actor", "audience", "authentication_strength"},
		[]string{"credential_id", "effective_subject", "delegation_id"})
	if err != nil {
		return p, ErrPrincipalInvalid
	}
	actor, err := strictObject(fields["actor"], []string{"kind"}, []string{"id"})
	if err != nil || json.Unmarshal(raw, &p) != nil {
		return PrincipalContext{}, ErrPrincipalInvalid
	}
	if p.Actor.Kind == "anonymous" {
		if _, exists := actor["id"]; exists {
			return PrincipalContext{}, ErrPrincipalInvalid
		}
		for _, key := range []string{"credential_id", "effective_subject", "delegation_id"} {
			if _, exists := fields[key]; exists {
				return PrincipalContext{}, ErrPrincipalInvalid
			}
		}
	} else if _, exists := actor["id"]; !exists {
		return PrincipalContext{}, ErrPrincipalInvalid
	}
	if subject, exists := fields["effective_subject"]; exists {
		if _, err := strictObject(subject, []string{"kind", "id"}, nil); err != nil {
			return PrincipalContext{}, ErrPrincipalInvalid
		}
	}
	if delegation, exists := fields["delegation_id"]; exists {
		var value string
		if json.Unmarshal(delegation, &value) != nil || !validOpaqueID(value) {
			return PrincipalContext{}, ErrPrincipalInvalid
		}
	}
	if err := p.Validate(); err != nil {
		return PrincipalContext{}, err
	}
	return p, nil
}
