package policy

import (
	"reflect"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// BoardDelegationPolicy implements the campusos.board-delegation/v1 judgement
// using only current facts supplied by the host. Its zero value requires no
// cache, repository, credential verifier, global loader or ambient clock.
// An allow verdict is a proposal, never a committed grant.
type BoardDelegationPolicy struct{}

var _ port.BoardDelegationPolicy = BoardDelegationPolicy{}

func delegationWindowActive(status string, notBefore, expiresAt, now int64) bool {
	return status == "active" && notBefore <= now && now < expiresAt
}

// DecideBoardDelegation applies the frozen judgement order: actor domain,
// authentication strength, management authority, recipient, boards, then a
// single complete bound witness per candidate atom. Conditions from different
// bounds are never combined; ties resolve by ASCII bound ID order so repeated
// evaluations are stable. Unknown facts deny closed.
func (BoardDelegationPolicy) DecideBoardDelegation(r port.BoardDelegationRequest) (port.BoardDelegationDecision, error) {
	if err := r.Validate(); err != nil {
		return port.BoardDelegationDecision{}, err
	}
	deny := func(reason string) port.BoardDelegationDecision {
		return port.BoardDelegationDecision{
			Contract: port.BoardDelegationContract, RequestID: r.RequestID, FactsVersion: r.FactsVersion,
			Effect: "deny", Reason: reason, Witnesses: []port.BoardDelegationWitness{}, Obligations: []string{},
		}
	}
	p := r.Principal
	if p.Actor.Kind != "admin" {
		return deny("delegation.actor_denied"), nil
	}
	if p.AuthenticationStrength != "mfa" {
		return deny("delegation.mfa_required"), nil
	}
	if r.Management.Actor != p.Actor ||
		!delegationWindowActive(r.Management.Status, r.Management.NotBefore, r.Management.ExpiresAt, r.EvaluatedAt) {
		return deny("delegation.management_denied"), nil
	}
	if r.Recipient.Subject != r.Candidate.Recipient || r.Recipient.Status != "active" {
		return deny("delegation.recipient_denied"), nil
	}
	for _, g := range r.Candidate.Grants {
		found := false
		for _, b := range r.Boards {
			if b.ID == g.BoardID && b.Status == "active" {
				found = true
				break
			}
		}
		if !found {
			return deny("delegation.board_denied"), nil
		}
	}
	witnesses := make([]port.BoardDelegationWitness, 0, len(r.Candidate.Grants))
	for i, g := range r.Candidate.Grants {
		best := ""
		for _, b := range r.Bounds {
			if b.Actor == p.Actor && b.Delegable &&
				delegationWindowActive(b.Status, b.NotBefore, b.ExpiresAt, r.EvaluatedAt) &&
				g.Action == b.Action && g.BoardID == b.BoardID &&
				g.NotBefore >= r.EvaluatedAt && g.NotBefore >= b.NotBefore && g.ExpiresAt <= b.ExpiresAt &&
				(b.RequiredStrength == "password" || g.RequiredStrength == "mfa") {
				if best == "" || b.ID < best {
					best = b.ID
				}
			}
		}
		if best == "" {
			return deny("delegation.outside_bounds"), nil
		}
		witnesses = append(witnesses, port.BoardDelegationWitness{CandidateIndex: i, BoundID: best})
	}
	return port.BoardDelegationDecision{
		Contract: port.BoardDelegationContract, RequestID: r.RequestID, FactsVersion: r.FactsVersion,
		Effect: "allow", Reason: "delegation.allowed", Witnesses: witnesses,
		Obligations: []string{"recheck_authority_in_transaction", "required_audit"},
	}, nil
}

// CheckBoardDelegationDecision models the pre-commit recheck inside the write
// transaction. The decision must equal a fresh evaluation of the original
// request; a verified deny stays denied with its stable reason. Time may only
// move forward, and request identity, facts version, candidate, actor and
// credential must be unchanged; the current snapshot is re-evaluated and must
// still allow with identical witnesses. Callers still own fresh fact loading,
// the atomic grant/audit write and lock/version discipline; this pure function
// neither acquires a database lock nor performs an audit.
func CheckBoardDelegationDecision(original port.BoardDelegationRequest, decision port.BoardDelegationDecision, current port.BoardDelegationRequest) error {
	if decision.Validate() != nil {
		return port.ErrDelegationDecisionMismatch
	}
	expected, err := (BoardDelegationPolicy{}).DecideBoardDelegation(original)
	if err != nil || !reflect.DeepEqual(expected, decision) {
		return port.ErrDelegationDecisionMismatch
	}
	if decision.Effect == "deny" {
		return &port.DelegationDenyError{Reason: decision.Reason}
	}
	if current.Validate() != nil || current.RequestID != original.RequestID ||
		current.FactsVersion != original.FactsVersion ||
		!reflect.DeepEqual(current.Candidate, original.Candidate) ||
		current.Principal.Actor != original.Principal.Actor ||
		current.Principal.CredentialID != original.Principal.CredentialID ||
		current.EvaluatedAt < original.EvaluatedAt {
		return port.ErrDelegationFactsChanged
	}
	fresh, err := (BoardDelegationPolicy{}).DecideBoardDelegation(current)
	if err != nil || fresh.Effect != "allow" || !reflect.DeepEqual(fresh.Witnesses, decision.Witnesses) {
		return port.ErrDelegationFactsChanged
	}
	return nil
}
