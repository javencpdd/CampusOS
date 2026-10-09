package port

import (
	"encoding/json"
	"errors"
)

const (
	ThreadPublicReadContract      = "campusos.policy.thread-public-read/v1"
	ThreadPublicReadPolicyVersion = "community.thread.public_read/v1"
)

var (
	ErrPolicyInputInvalid        = errors.New("policy.input_invalid")
	ErrPolicyContractUnsupported = errors.New("policy.contract_unsupported")
	ErrPolicyResourceNotPublic   = errors.New("policy.resource_not_public")
	ErrPolicyDecisionMismatch    = errors.New("policy.decision_mismatch")
	ErrPolicyFactsChanged        = errors.New("policy.facts_changed")
)

// PolicyResourceRef always compares kind together with ID. It is neither a
// loader key nor permission to fetch an arbitrary resource.
type PolicyResourceRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// ThreadPublicReadFacts must be constructed by Community from current facts
// and a content revision in one consistent read. No legacy state defaults apply.
// FactsVersion must advance for all changes, including an ABA transition.
type ThreadPublicReadFacts struct {
	Resource          PolicyResourceRef `json:"resource"`
	Owner             PrincipalRef      `json:"owner"`
	Board             PolicyResourceRef `json:"board"`
	FactsVersion      string            `json:"facts_version"`
	PublicationStatus string            `json:"publication_status"`
	ModerationStatus  string            `json:"moderation_status"`
	DeletionStatus    string            `json:"deletion_status"`
}

type ThreadPublicReadRequest struct {
	Contract  string                `json:"contract"`
	RequestID string                `json:"request_id"`
	Policy    string                `json:"policy"`
	Principal PrincipalContext      `json:"principal"`
	Facts     ThreadPublicReadFacts `json:"facts"`
}

type ThreadPublicReadDecision struct {
	Contract     string            `json:"contract"`
	RequestID    string            `json:"request_id"`
	Policy       string            `json:"policy"`
	Resource     PolicyResourceRef `json:"resource"`
	FactsVersion string            `json:"facts_version"`
	Effect       string            `json:"effect"`
	Reason       string            `json:"reason"`
	Obligations  []string          `json:"obligations"`
}

// ThreadPublicReadPolicy is a pure, named predicate. The host supplies already
// authenticated principals and authoritative facts; no DB/cache/Feature loader
// is injected. Allow does not satisfy route ACL, Scope, Grant/Consent or runtime
// checks. A decision is confined to this invocation, never a reusable grant.
type ThreadPublicReadPolicy interface {
	DecideThreadPublicRead(ThreadPublicReadRequest) (ThreadPublicReadDecision, error)
}

func (f ThreadPublicReadFacts) Validate() error {
	if f.Resource.Kind != "community.thread" || !validOpaqueID(f.Resource.ID) ||
		f.Owner.Kind != "user" || !validOpaqueID(f.Owner.ID) ||
		f.Board.Kind != "community.board" || !validOpaqueID(f.Board.ID) || !validOpaqueID(f.FactsVersion) {
		return ErrPolicyInputInvalid
	}
	switch f.PublicationStatus {
	case "draft", "published", "private":
	default:
		return ErrPolicyInputInvalid
	}
	switch f.ModerationStatus {
	case "clear", "pending", "rejected", "taken_down":
	default:
		return ErrPolicyInputInvalid
	}
	switch f.DeletionStatus {
	case "active", "trashed", "purged":
	default:
		return ErrPolicyInputInvalid
	}
	return nil
}

func (r ThreadPublicReadRequest) Validate() error {
	if r.Contract != "" && r.Contract != ThreadPublicReadContract {
		return ErrPolicyContractUnsupported
	}
	if r.Contract != ThreadPublicReadContract || r.Policy != ThreadPublicReadPolicyVersion ||
		!validOpaqueID(r.RequestID) || r.Principal.Validate() != nil {
		return ErrPolicyInputInvalid
	}
	return r.Facts.Validate()
}

func (d ThreadPublicReadDecision) Validate() error {
	if d.Contract != ThreadPublicReadContract || d.Policy != ThreadPublicReadPolicyVersion ||
		!validOpaqueID(d.RequestID) || d.Resource.Kind != "community.thread" ||
		!validOpaqueID(d.Resource.ID) || !validOpaqueID(d.FactsVersion) || d.Obligations == nil {
		return ErrPolicyDecisionMismatch
	}
	if d.Effect == "allow" && d.Reason == "policy.public_read" && len(d.Obligations) == 1 && d.Obligations[0] == "recheck_current_facts" {
		return nil
	}
	if d.Effect == "deny" && d.Reason == "policy.resource_not_public" && len(d.Obligations) == 0 {
		return nil
	}
	return ErrPolicyDecisionMismatch
}

// CheckThreadPublicReadDecision checks a trusted same-call decision against
// freshly reloaded facts. It performs no read itself. The business caller owns
// the read/return ordering, ABA-safe versioning and all outer authorization.
// Passing caller-controlled JSON here does not authenticate or authorize it.
func CheckThreadPublicReadDecision(r ThreadPublicReadRequest, d ThreadPublicReadDecision, current ThreadPublicReadFacts) error {
	if err := r.Validate(); err != nil {
		return ErrPolicyInputInvalid
	}
	if d.Validate() != nil || d.RequestID != r.RequestID || d.Policy != r.Policy ||
		d.Resource != r.Facts.Resource || d.FactsVersion != r.Facts.FactsVersion {
		return ErrPolicyDecisionMismatch
	}
	if d.Effect == "deny" {
		return ErrPolicyResourceNotPublic
	}
	if r.Facts.PublicationStatus != "published" || r.Facts.ModerationStatus != "clear" || r.Facts.DeletionStatus != "active" {
		return ErrPolicyDecisionMismatch
	}
	if current.Validate() != nil || current != r.Facts {
		return ErrPolicyFactsChanged
	}
	return nil
}

// ParseThreadPublicReadRequestJSON is only a strict host adapter codec, never
// an authentication boundary or a public endpoint. Unknown versions fail first
// after parsing a single unambiguous object; unknown/missing/null fields fail closed.
func ParseThreadPublicReadRequestJSON(raw []byte) (ThreadPublicReadRequest, error) {
	// Read the version without losing ambiguity checks. The second strict pass
	// below validates the full field set and required values.
	fields, err := decodeStrictObject(raw)
	if err != nil {
		return ThreadPublicReadRequest{}, ErrPolicyInputInvalid
	}
	var version any
	if json.Unmarshal(fields["contract"], &version) == nil {
		if contract, ok := version.(string); ok && contract != ThreadPublicReadContract {
			return ThreadPublicReadRequest{}, ErrPolicyContractUnsupported
		}
	}
	fields, err = strictObject(raw, []string{"contract", "request_id", "policy", "principal", "facts"}, nil)
	if err != nil {
		return ThreadPublicReadRequest{}, ErrPolicyInputInvalid
	}
	var r ThreadPublicReadRequest
	if json.Unmarshal(raw, &r) != nil {
		return ThreadPublicReadRequest{}, ErrPolicyInputInvalid
	}
	r.Principal, err = ParsePrincipalJSON(fields["principal"])
	if err != nil {
		return ThreadPublicReadRequest{}, ErrPolicyInputInvalid
	}
	r.Facts, err = ParseThreadPublicReadFactsJSON(fields["facts"])
	if err != nil {
		return ThreadPublicReadRequest{}, ErrPolicyInputInvalid
	}
	if err = r.Validate(); err != nil {
		return ThreadPublicReadRequest{}, err
	}
	return r, nil
}

func ParseThreadPublicReadFactsJSON(raw []byte) (ThreadPublicReadFacts, error) {
	fields, err := strictObject(raw, []string{"resource", "owner", "board", "facts_version", "publication_status", "moderation_status", "deletion_status"}, nil)
	if err != nil {
		return ThreadPublicReadFacts{}, ErrPolicyInputInvalid
	}
	for _, name := range []string{"resource", "owner", "board"} {
		if _, err = strictObject(fields[name], []string{"kind", "id"}, nil); err != nil {
			return ThreadPublicReadFacts{}, ErrPolicyInputInvalid
		}
	}
	var f ThreadPublicReadFacts
	if json.Unmarshal(raw, &f) != nil || f.Validate() != nil {
		return ThreadPublicReadFacts{}, ErrPolicyInputInvalid
	}
	return f, nil
}

func ParseThreadPublicReadDecisionJSON(raw []byte) (ThreadPublicReadDecision, error) {
	fields, err := strictObject(raw, []string{"contract", "request_id", "policy", "resource", "facts_version", "effect", "reason", "obligations"}, nil)
	if err != nil {
		return ThreadPublicReadDecision{}, ErrPolicyDecisionMismatch
	}
	if _, err = strictObject(fields["resource"], []string{"kind", "id"}, nil); err != nil {
		return ThreadPublicReadDecision{}, ErrPolicyDecisionMismatch
	}
	var d ThreadPublicReadDecision
	if json.Unmarshal(raw, &d) != nil || d.Validate() != nil {
		return ThreadPublicReadDecision{}, ErrPolicyDecisionMismatch
	}
	return d, nil
}
