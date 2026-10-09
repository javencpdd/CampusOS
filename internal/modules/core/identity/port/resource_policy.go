package port

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

const ResourcePolicyContract = "campusos.resource-policy/v1"

var (
	ErrResourcePolicyInvalid       = errors.New("policy.invalid")
	ErrResourcePolicyUnknownAction = errors.New("policy.unknown_action")
	ErrResourcePolicyFactsChanged  = ErrPolicyFactsChanged
)

// ResourcePolicyFacts are supplied by the owning module. Optional fields are
// required by individual named actions; missing authority never gets a default.
// OriginVisible distinguishes absent origin facts from a known hidden origin.
type ResourcePolicyFacts struct {
	Kind              string `json:"kind"`
	ID                string `json:"id"`
	OwnerID           string `json:"owner_id,omitempty"`
	BoardID           string `json:"board_id,omitempty"`
	CollectionID      string `json:"collection_id,omitempty"`
	Status            string `json:"status"`
	PublicationStatus string `json:"publication_status,omitempty"`
	OriginVisible     *bool  `json:"origin_visible,omitempty"`
	Version           string `json:"version"`
}

type ResourcePolicyGrant struct {
	SubjectKind      string `json:"subject_kind"`
	SubjectID        string `json:"subject_id"`
	Action           string `json:"action"`
	BoardID          string `json:"board_id"`
	ExpiresAtMS      int64  `json:"expires_at_ms"`
	Status           string `json:"status"`
	RequiredStrength string `json:"required_strength"`
}

type ResourcePolicyRequest struct {
	Contract      string                `json:"contract"`
	RequestID     string                `json:"request_id"`
	Principal     PrincipalContext      `json:"principal"`
	Action        string                `json:"action"`
	Facts         ResourcePolicyFacts   `json:"facts"`
	Grants        []ResourcePolicyGrant `json:"grants"`
	EvaluatedAtMS int64                 `json:"evaluated_at_ms"`
	PolicyVersion string                `json:"policy_version"`
}

type ResourcePolicyDecision struct {
	RequestID     string   `json:"request_id"`
	Effect        string   `json:"effect"`
	Reason        string   `json:"reason"`
	FactsVersion  string   `json:"facts_version"`
	PolicyVersion string   `json:"policy_version"`
	Obligations   []string `json:"obligations"`
}

// ResourcePolicy has five registered actions under a fixed v1 contract. The
// policy version is the host's current policy revision, not a script name.
// This Port neither authenticates credentials nor loads facts/grants or commits
// writes/audits. The owning application must fulfill all returned obligations.
type ResourcePolicy interface {
	DecideResource(ResourcePolicyRequest) (ResourcePolicyDecision, error)
}

// This G1 contract uses Unicode strings of 1..128 characters for resource IDs
// and revisions, unlike the Principal contract's ASCII opaque ID constraint.
func validResourcePolicyString(s string) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= 128
}

func knownResourceAction(action string) bool {
	switch action {
	case "community.thread.edit", "community.thread.take_down", "community.post.delete", "personal.document.read", "knowledge.source.read":
		return true
	default:
		return false
	}
}

func (f ResourcePolicyFacts) Validate() error {
	switch f.Kind {
	case "thread", "post", "document", "knowledge_source":
	default:
		return ErrResourcePolicyInvalid
	}
	switch f.Status {
	case "active", "draft", "published", "private", "archived", "deleted":
	default:
		return ErrResourcePolicyInvalid
	}
	if !validResourcePolicyString(f.ID) || !validResourcePolicyString(f.Version) {
		return ErrResourcePolicyInvalid
	}
	for _, value := range []string{f.OwnerID, f.BoardID, f.CollectionID} {
		if value != "" && !validResourcePolicyString(value) {
			return ErrResourcePolicyInvalid
		}
	}
	switch f.PublicationStatus {
	case "", "draft", "published", "private":
	default:
		return ErrResourcePolicyInvalid
	}
	return nil
}

func (g ResourcePolicyGrant) Validate() error {
	if (g.SubjectKind != "user" && g.SubjectKind != "admin") || !validResourcePolicyString(g.SubjectID) || !validResourcePolicyString(g.BoardID) ||
		(g.Action != "community.thread.take_down" && g.Action != "community.post.delete") || g.ExpiresAtMS < 1 ||
		(g.Status != "active" && g.Status != "revoked") || (g.RequiredStrength != "password" && g.RequiredStrength != "mfa") {
		return ErrResourcePolicyInvalid
	}
	return nil
}

func (r ResourcePolicyRequest) Validate() error {
	if !knownResourceAction(r.Action) {
		return ErrResourcePolicyUnknownAction
	}
	if r.Contract != ResourcePolicyContract || !validResourcePolicyString(r.RequestID) || !validResourcePolicyString(r.PolicyVersion) ||
		r.Principal.Validate() != nil || r.Facts.Validate() != nil || r.Grants == nil || len(r.Grants) > 64 || r.EvaluatedAtMS < 0 {
		return ErrResourcePolicyInvalid
	}
	for _, g := range r.Grants {
		if g.Validate() != nil {
			return ErrResourcePolicyInvalid
		}
	}
	return nil
}

// Validate enforces known semantic combinations as well as the structural G1
// decision schema. Action-specific write obligations are checked by the consumer.
func (d ResourcePolicyDecision) Validate() error {
	if !validResourcePolicyString(d.RequestID) || !validResourcePolicyString(d.FactsVersion) || !validResourcePolicyString(d.PolicyVersion) || d.Obligations == nil {
		return ErrResourcePolicyInvalid
	}
	if d.Effect == "deny" && len(d.Obligations) == 0 {
		switch d.Reason {
		case "policy.resource_unavailable", "policy.principal_wrong_domain", "policy.scope_denied":
			return nil
		}
	}
	if d.Effect == "allow" && d.Reason == "ALLOW" && len(d.Obligations) >= 1 && len(d.Obligations) <= 2 && d.Obligations[0] == "recheck_facts" {
		if len(d.Obligations) == 1 || d.Obligations[1] == "required_audit" {
			return nil
		}
	}
	return ErrResourcePolicyInvalid
}

// ParseResourcePolicyRequestJSON is a bounded host codec, not an ingress
// authorization endpoint. Time is decoded as exact signed 64-bit milliseconds;
// fractional/overflow values are rejected, never rounded into a grant window.
func ParseResourcePolicyRequestJSON(raw []byte) (ResourcePolicyRequest, error) {
	if !resourceJSONUnicodeValid(raw) {
		return ResourcePolicyRequest{}, ErrResourcePolicyInvalid
	}
	fields, err := decodeStrictObject(raw)
	if err != nil {
		return ResourcePolicyRequest{}, ErrResourcePolicyInvalid
	}
	var action string
	if json.Unmarshal(fields["action"], &action) != nil || !knownResourceAction(action) {
		return ResourcePolicyRequest{}, ErrResourcePolicyUnknownAction
	}
	fields, err = strictObject(raw, []string{"contract", "request_id", "principal", "action", "facts", "grants", "evaluated_at_ms", "policy_version"}, nil)
	if err != nil {
		return ResourcePolicyRequest{}, ErrResourcePolicyInvalid
	}
	// Decode ordinary fields separately so integral JSON decimals/exponents can
	// be accepted exactly without encoding/json coercion into int64.
	var r ResourcePolicyRequest
	for key, dst := range map[string]*string{"contract": &r.Contract, "request_id": &r.RequestID, "action": &r.Action, "policy_version": &r.PolicyVersion} {
		if json.Unmarshal(fields[key], dst) != nil {
			return ResourcePolicyRequest{}, ErrResourcePolicyInvalid
		}
	}
	r.Principal, err = ParsePrincipalJSON(fields["principal"])
	if err != nil {
		return ResourcePolicyRequest{}, ErrResourcePolicyInvalid
	}
	r.Facts, err = parseResourcePolicyFacts(fields["facts"])
	if err != nil {
		return ResourcePolicyRequest{}, ErrResourcePolicyInvalid
	}
	r.EvaluatedAtMS, err = resourcePolicyMillis(fields["evaluated_at_ms"])
	if err != nil {
		return ResourcePolicyRequest{}, ErrResourcePolicyInvalid
	}
	var grants []json.RawMessage
	if json.Unmarshal(fields["grants"], &grants) != nil || grants == nil || len(grants) > 64 {
		return ResourcePolicyRequest{}, ErrResourcePolicyInvalid
	}
	r.Grants = make([]ResourcePolicyGrant, 0, len(grants))
	for _, rawGrant := range grants {
		g, err := parseResourcePolicyGrant(rawGrant)
		if err != nil {
			return ResourcePolicyRequest{}, err
		}
		r.Grants = append(r.Grants, g)
	}
	if err := r.Validate(); err != nil {
		return ResourcePolicyRequest{}, err
	}
	return r, nil
}

func parseResourcePolicyFacts(raw []byte) (ResourcePolicyFacts, error) {
	fields, err := strictObject(raw, []string{"kind", "id", "status", "version"}, []string{"owner_id", "board_id", "collection_id", "publication_status", "origin_visible"})
	if err != nil {
		return ResourcePolicyFacts{}, ErrResourcePolicyInvalid
	}
	for _, name := range []string{"owner_id", "board_id", "collection_id", "publication_status"} {
		if value, exists := fields[name]; exists {
			var s string
			if json.Unmarshal(value, &s) != nil || !validResourcePolicyString(s) {
				return ResourcePolicyFacts{}, ErrResourcePolicyInvalid
			}
		}
	}
	var f ResourcePolicyFacts
	if json.Unmarshal(raw, &f) != nil || f.Validate() != nil {
		return ResourcePolicyFacts{}, ErrResourcePolicyInvalid
	}
	return f, nil
}

func parseResourcePolicyGrant(raw []byte) (ResourcePolicyGrant, error) {
	fields, err := strictObject(raw, []string{"subject_kind", "subject_id", "action", "board_id", "expires_at_ms", "status", "required_strength"}, nil)
	if err != nil {
		return ResourcePolicyGrant{}, ErrResourcePolicyInvalid
	}
	var g ResourcePolicyGrant
	for key, dst := range map[string]*string{"subject_kind": &g.SubjectKind, "subject_id": &g.SubjectID, "action": &g.Action, "board_id": &g.BoardID, "status": &g.Status, "required_strength": &g.RequiredStrength} {
		if json.Unmarshal(fields[key], dst) != nil {
			return ResourcePolicyGrant{}, ErrResourcePolicyInvalid
		}
	}
	g.ExpiresAtMS, err = resourcePolicyMillis(fields["expires_at_ms"])
	if err != nil || g.Validate() != nil {
		return ResourcePolicyGrant{}, ErrResourcePolicyInvalid
	}
	return g, nil
}

func resourcePolicyMillis(raw []byte) (int64, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if dec.Decode(&value) != nil {
		return 0, ErrResourcePolicyInvalid
	}
	number, ok := value.(json.Number)
	if !ok || len(number.String()) > 128 {
		return 0, ErrResourcePolicyInvalid
	}
	// Bound big.Rat allocation even for a tiny token such as 1e999999999.
	if parts := strings.Split(strings.ToLower(number.String()), "e"); len(parts) == 2 {
		exponent, err := strconv.ParseInt(parts[1], 10, 32)
		if err != nil || exponent < -1000 || exponent > 1000 {
			return 0, ErrResourcePolicyInvalid
		}
	}
	rational, ok := new(big.Rat).SetString(number.String())
	if !ok || !rational.IsInt() || !rational.Num().IsInt64() || rational.Sign() < 0 {
		return 0, ErrResourcePolicyInvalid
	}
	return rational.Num().Int64(), nil
}

func ParseResourcePolicyDecisionJSON(raw []byte) (ResourcePolicyDecision, error) {
	if !resourceJSONUnicodeValid(raw) {
		return ResourcePolicyDecision{}, ErrResourcePolicyInvalid
	}
	if _, err := strictObject(raw, []string{"request_id", "effect", "reason", "facts_version", "policy_version", "obligations"}, nil); err != nil {
		return ResourcePolicyDecision{}, ErrResourcePolicyInvalid
	}
	var d ResourcePolicyDecision
	if json.Unmarshal(raw, &d) != nil || d.Validate() != nil {
		return ResourcePolicyDecision{}, ErrResourcePolicyInvalid
	}
	return d, nil
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Reject these
// before decoding so distinct resource/revision strings never collapse.
func resourceJSONUnicodeValid(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
