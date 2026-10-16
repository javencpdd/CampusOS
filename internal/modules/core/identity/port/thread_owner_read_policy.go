package port

import (
	"errors"
	"reflect"
)

const (
	ThreadOwnerReadContract      = "campusos.policy.thread-owner-read/v1"
	ThreadOwnerReadPolicyVersion = "community.thread.owner_read/v1"
)

var ErrPolicyOwnerReadDenied = errors.New("policy.owner_read_denied")

// ThreadOwnerReadFacts shares the raw, versioned thread fact shape, not the
// public-read predicate. Only Community supplies these facts from the row that
// supplies the content; missing states never acquire compatibility defaults.
type ThreadOwnerReadFacts = ThreadPublicReadFacts

type ThreadOwnerReadRequest struct {
	Contract  string               `json:"contract"`
	RequestID string               `json:"request_id"`
	Policy    string               `json:"policy"`
	Principal PrincipalContext     `json:"principal"`
	Facts     ThreadOwnerReadFacts `json:"facts"`
}

// ThreadOwnerReadDecision remains internal. Its complete principal binding is
// a credential reference, never raw credentials, and must not be sent to HTTP
// clients. JSON tags describe the contract, not an ingress JSON parser.
type ThreadOwnerReadDecision struct {
	Contract     string            `json:"contract"`
	RequestID    string            `json:"request_id"`
	Policy       string            `json:"policy"`
	Principal    PrincipalContext  `json:"principal"`
	Resource     PolicyResourceRef `json:"resource"`
	FactsVersion string            `json:"facts_version"`
	Effect       string            `json:"effect"`
	Reason       string            `json:"reason"`
	Obligations  []string          `json:"obligations"`
}

// ThreadOwnerReadPolicy consumes authenticated user-domain principal facts.
// It neither authenticates a DTO nor loads resources, sessions, or grants.
// The owning application must refresh both resource and credential facts.
type ThreadOwnerReadPolicy interface {
	DecideThreadOwnerRead(ThreadOwnerReadRequest) (ThreadOwnerReadDecision, error)
}

func (r ThreadOwnerReadRequest) Validate() error {
	if r.Contract != "" && r.Contract != ThreadOwnerReadContract {
		return ErrPolicyContractUnsupported
	}
	if r.Contract != ThreadOwnerReadContract || r.Policy != ThreadOwnerReadPolicyVersion ||
		!validOpaqueID(r.RequestID) || r.Principal.Validate() != nil {
		return ErrPolicyInputInvalid
	}
	return r.Facts.Validate()
}

func (d ThreadOwnerReadDecision) Validate() error {
	if d.Contract != ThreadOwnerReadContract || d.Policy != ThreadOwnerReadPolicyVersion ||
		!validOpaqueID(d.RequestID) || d.Principal.Validate() != nil ||
		d.Resource.Kind != "community.thread" || !validOpaqueID(d.Resource.ID) ||
		!validOpaqueID(d.FactsVersion) || d.Obligations == nil {
		return ErrPolicyDecisionMismatch
	}
	if d.Effect == "allow" && d.Reason == "policy.owner_read" &&
		len(d.Obligations) == 1 && d.Obligations[0] == "recheck_current_facts" {
		return nil
	}
	if d.Effect == "deny" && d.Reason == "policy.owner_read_denied" && len(d.Obligations) == 0 {
		return nil
	}
	return ErrPolicyDecisionMismatch
}

// CheckThreadOwnerReadDecision binds a trusted same-call decision to the
// original principal and resource. An allow cannot invent owner status or
// borrow an admin/plugin's same numeric ID. Every current fact and the entire
// current principal must match, including credential and authentication
// strength. The caller owns fresh loading and the final read/return order.
func CheckThreadOwnerReadDecision(r ThreadOwnerReadRequest, d ThreadOwnerReadDecision, current ThreadOwnerReadFacts, currentPrincipal PrincipalContext) error {
	if r.Validate() != nil {
		return ErrPolicyInputInvalid
	}
	if d.Validate() != nil || d.RequestID != r.RequestID || d.Policy != r.Policy ||
		d.Resource != r.Facts.Resource || d.FactsVersion != r.Facts.FactsVersion ||
		!reflect.DeepEqual(d.Principal, r.Principal) {
		return ErrPolicyDecisionMismatch
	}
	if d.Effect == "deny" {
		return ErrPolicyOwnerReadDenied
	}
	if r.Principal.Actor.Kind != "user" || r.Principal.Actor.ID != r.Facts.Owner.ID ||
		(r.Facts.DeletionStatus != "active" && r.Facts.DeletionStatus != "trashed") {
		return ErrPolicyDecisionMismatch
	}
	if current.Validate() != nil || current != r.Facts || currentPrincipal.Validate() != nil ||
		!reflect.DeepEqual(currentPrincipal, r.Principal) {
		return ErrPolicyFactsChanged
	}
	return nil
}
