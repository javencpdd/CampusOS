package port

import (
	"encoding/json"
	"errors"
	"strconv"
)

const IntegrationAccessContract = "campusos.integration-access/v1"

var (
	ErrIntegrationInputInvalid     = errors.New("integration.input_invalid")
	ErrIntegrationDecisionMismatch = errors.New("integration.decision_mismatch")
	ErrIntegrationFactsChanged     = errors.New("integration.facts_changed")
)

type IntegrationAccessDenied struct{ Reason string }

func (e *IntegrationAccessDenied) Error() string { return e.Reason }

// These DTOs contain host-verified facts, never a token or a public ingress
// request. An allow only passes the machine whitelist; the resource owner must
// independently authorize the current original resource before returning data.
type IntegrationAccessTarget struct {
	Channel  string            `json:"channel"`
	Endpoint string            `json:"endpoint"`
	Action   string            `json:"action"`
	Resource PolicyResourceRef `json:"resource"`
}

// Endpoint facts come from the host's current endpoint registry, not a key's
// proposed scope. An old Admin MCP tool cannot opt itself into Integration.
type IntegrationAccessEndpoint struct {
	Channel            string `json:"channel"`
	ID                 string `json:"id"`
	Action             string `json:"action"`
	ResourceKind       string `json:"resource_kind"`
	Enabled            bool   `json:"enabled"`
	IntegrationAllowed bool   `json:"integration_allowed"`
}

type IntegrationAccessRule struct {
	ID           string   `json:"id"`
	Channel      string   `json:"channel"`
	Endpoint     string   `json:"endpoint"`
	Action       string   `json:"action"`
	ResourceKind string   `json:"resource_kind"`
	ResourceIDs  []string `json:"resource_ids"`
	Status       string   `json:"status"`
	NotBeforeMS  int64    `json:"not_before_ms"`
	ExpiresAtMS  int64    `json:"expires_at_ms"`
}

type IntegrationAccessFacts struct {
	IntegrationID      string                    `json:"integration_id"`
	CredentialID       string                    `json:"credential_id"`
	PrincipalStatus    string                    `json:"principal_status"`
	CredentialStatus   string                    `json:"credential_status"`
	CredentialAudience string                    `json:"credential_audience"`
	NotBeforeMS        int64                     `json:"not_before_ms"`
	ExpiresAtMS        int64                     `json:"expires_at_ms"`
	FactsVersion       string                    `json:"facts_version"`
	Endpoint           IntegrationAccessEndpoint `json:"endpoint"`
	Rules              []IntegrationAccessRule   `json:"rules"`
}

type IntegrationAccessInput struct {
	Contract  string                  `json:"contract"`
	RequestID string                  `json:"request_id"`
	Principal PrincipalContext        `json:"principal"`
	Target    IntegrationAccessTarget `json:"target"`
	Facts     IntegrationAccessFacts  `json:"facts"`
	NowMS     int64                   `json:"now_ms"`
}

// Decisions are same-call private values, not client responses or reusable
// grants. They bind every fact, including the complete whitelist inventory.
type IntegrationAccessDecision struct {
	Request IntegrationAccessInput `json:"request"`
	Effect  string                 `json:"effect"`
	Reason  string                 `json:"reason"`
	RuleID  string                 `json:"rule_id,omitempty"`
}

type IntegrationAccessPolicy interface {
	DecideIntegrationAccess(IntegrationAccessInput) (IntegrationAccessDecision, error)
	CheckIntegrationAccessDecision(IntegrationAccessInput, IntegrationAccessDecision, IntegrationAccessInput) error
}

func integrationAccessIDs(values ...string) bool {
	for _, value := range values {
		if !validOpaqueID(value) {
			return false
		}
	}
	return true
}

func (r IntegrationAccessInput) Validate() error {
	f, t := r.Facts, r.Target
	if r.Contract != IntegrationAccessContract || r.Principal.Validate() != nil ||
		!integrationAccessIDs(r.RequestID, t.Channel, t.Endpoint, t.Action, t.Resource.Kind, t.Resource.ID,
			f.IntegrationID, f.CredentialID, f.CredentialAudience, f.FactsVersion,
			f.Endpoint.Channel, f.Endpoint.ID, f.Endpoint.Action, f.Endpoint.ResourceKind) ||
		r.NowMS < 0 || r.NowMS > 9007199254740991 || f.NotBeforeMS < 0 ||
		f.ExpiresAtMS <= f.NotBeforeMS || f.ExpiresAtMS > 9007199254740991 ||
		(f.PrincipalStatus != "active" && f.PrincipalStatus != "suspended" && f.PrincipalStatus != "revoked") ||
		(f.CredentialStatus != "active" && f.CredentialStatus != "revoked") || f.Rules == nil || len(f.Rules) > 64 {
		return ErrIntegrationInputInvalid
	}
	seen := make(map[string]bool, len(f.Rules))
	for _, rule := range f.Rules {
		if !integrationAccessIDs(rule.ID, rule.Channel, rule.Endpoint, rule.Action, rule.ResourceKind) || seen[rule.ID] ||
			len(rule.ResourceIDs) < 1 || len(rule.ResourceIDs) > 32 || rule.NotBeforeMS < 0 ||
			rule.ExpiresAtMS <= rule.NotBeforeMS || rule.ExpiresAtMS > 9007199254740991 ||
			(rule.Status != "active" && rule.Status != "suspended" && rule.Status != "revoked") {
			return ErrIntegrationInputInvalid
		}
		seen[rule.ID] = true
		ids := make(map[string]bool, len(rule.ResourceIDs))
		for _, id := range rule.ResourceIDs {
			if !validOpaqueID(id) || ids[id] {
				return ErrIntegrationInputInvalid
			}
			ids[id] = true
		}
	}
	return nil
}

func (r IntegrationAccessInput) Clone() IntegrationAccessInput {
	if r.Principal.EffectiveSubject != nil {
		ref := *r.Principal.EffectiveSubject
		r.Principal.EffectiveSubject = &ref
	}
	if r.Facts.Rules != nil {
		rules := make([]IntegrationAccessRule, len(r.Facts.Rules))
		copy(rules, r.Facts.Rules)
		for i := range rules {
			rules[i].ResourceIDs = append([]string(nil), rules[i].ResourceIDs...)
		}
		r.Facts.Rules = rules
	}
	return r
}

// ParseIntegrationAccessInputJSON is a bounded host codec. Shared strict JSON
// checks reject duplicates, unknown/missing/null fields, bad Unicode, excessive
// depth and trailing values. Milliseconds remain exact safe integers in Go/JS.
func ParseIntegrationAccessInputJSON(raw []byte) (IntegrationAccessInput, error) {
	bad := func() (IntegrationAccessInput, error) { return IntegrationAccessInput{}, ErrIntegrationInputInvalid }
	if !resourceJSONUnicodeValid(raw) {
		return bad()
	}
	root, err := strictObject(raw, []string{"contract", "request_id", "principal", "target", "facts", "now_ms"}, nil)
	if err != nil {
		return bad()
	}
	if _, err = ParsePrincipalJSON(root["principal"]); err != nil {
		return bad()
	}
	target, err := strictObject(root["target"], []string{"channel", "endpoint", "action", "resource"}, nil)
	if err != nil {
		return bad()
	}
	if _, err = strictObject(target["resource"], []string{"kind", "id"}, nil); err != nil {
		return bad()
	}
	facts, err := strictObject(root["facts"], []string{"integration_id", "credential_id", "principal_status", "credential_status", "credential_audience", "not_before_ms", "expires_at_ms", "facts_version", "endpoint", "rules"}, nil)
	if err != nil {
		return bad()
	}
	if _, err = strictObject(facts["endpoint"], []string{"channel", "id", "action", "resource_kind", "enabled", "integration_allowed"}, nil); err != nil {
		return bad()
	}
	var rules []json.RawMessage
	if json.Unmarshal(facts["rules"], &rules) != nil || rules == nil || len(rules) > 64 {
		return bad()
	}
	for i, rule := range rules {
		fields, err := strictObject(rule, []string{"id", "channel", "endpoint", "action", "resource_kind", "resource_ids", "status", "not_before_ms", "expires_at_ms"}, nil)
		if err != nil {
			return bad()
		}
		for _, key := range []string{"not_before_ms", "expires_at_ms"} {
			value, err := resourcePolicyMillis(fields[key])
			if err != nil || value > 9007199254740991 {
				return bad()
			}
			fields[key] = json.RawMessage(strconv.FormatInt(value, 10))
		}
		rules[i], err = json.Marshal(fields)
		if err != nil {
			return bad()
		}
	}
	facts["rules"], err = json.Marshal(rules)
	if err != nil {
		return bad()
	}
	for _, field := range []struct {
		values map[string]json.RawMessage
		key    string
	}{{root, "now_ms"}, {facts, "not_before_ms"}, {facts, "expires_at_ms"}} {
		value, err := resourcePolicyMillis(field.values[field.key])
		if err != nil || value > 9007199254740991 {
			return bad()
		}
		field.values[field.key] = json.RawMessage(strconv.FormatInt(value, 10))
	}
	root["facts"], err = json.Marshal(facts)
	if err != nil {
		return bad()
	}
	normalized, err := json.Marshal(root)
	var r IntegrationAccessInput
	if err != nil || json.Unmarshal(normalized, &r) != nil || r.Validate() != nil {
		return bad()
	}
	return r, nil
}
