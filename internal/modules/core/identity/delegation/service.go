// Package delegation implements the V12-02a board-governance delegation write
// path on top of the pure Policy Port. It keeps execution rights and
// delegatable ceilings separate: every proposal is evaluated from current
// host-loaded facts, then re-checked inside the write transaction.
package delegation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var (
	ErrDelegationUnavailable  = errors.New("identity delegation service is unavailable")
	ErrDelegationFactsChanged = errors.New("delegation.facts_changed")
)

// Actor is the host-verified admin context of a delegation write. Callers must
// establish the admin domain and MFA before constructing it; the service
// re-verifies management authority but never authenticates credentials.
type Actor struct {
	ID                     string
	AuthenticationStrength string
	CredentialID           string
}

// GrantCandidate is one proposed execution right. Zero window fields select
// the host default: start now and expire at the earlier of now+365 days or the
// covering bound's expiry; zero strength defaults to password. Explicit values
// are evaluated as proposed and deny closed when they exceed the bounds.
type GrantCandidate struct {
	Action           string
	BoardID          string
	NotBefore        time.Time
	ExpiresAt        time.Time
	RequiredStrength string
}

// BoardFactsProvider reloads the current board facts of the candidate boards.
// The calling host module owns category state; it is invoked once before
// evaluation and once more inside the write transaction.
type BoardFactsProvider = port.BoardFactsProvider

type UserLookup interface {
	GetByID(ctx context.Context, id string) (*domain.User, error)
}

// Service evaluates and commits board-governance delegation proposals.
type Service struct {
	delegations repository.DelegationRepository
	users       UserLookup
	catalog     repository.AuthorizationRepository
	reliable    *reliability.Service
	now         func() time.Time
	policy      port.BoardDelegationPolicy
}

func NewService(delegations repository.DelegationRepository, users UserLookup, catalog repository.AuthorizationRepository) *Service {
	return &Service{
		delegations: delegations,
		users:       users,
		catalog:     catalog,
		now:         time.Now,
		policy:      policy.BoardDelegationPolicy{},
	}
}

func (s *Service) SetReliability(reliable *reliability.Service) {
	s.reliable = reliable
	if reliable == nil {
		return
	}
	if snapshotter, ok := s.delegations.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snapshotter)
	}
}

// SetClock replaces the wall clock in tests; production keeps time.Now.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

func (s *Service) principal(actor Actor) port.PrincipalContext {
	return port.PrincipalContext{
		Contract:               port.PrincipalContract,
		Actor:                  port.PrincipalRef{Kind: "admin", ID: actor.ID},
		Audience:               "admin",
		AuthenticationStrength: actor.AuthenticationStrength,
		CredentialID:           actor.CredentialID,
	}
}

func recipientStatus(status domain.UserStatus) string {
	switch status {
	case domain.UserStatusActive:
		return "active"
	case domain.UserStatusSuspended:
		return "suspended"
	default:
		return "deleted"
	}
}

// factsVersion is a deterministic digest of every fact the judgement depends
// on. Any management/bound/recipient/board change yields a different version,
// so the in-transaction recheck detects concurrent mutation even when a row
// version was not bumped by the writer.
func factsVersion(management repository.Delegation, bounds []repository.Delegation, recipientID, recipientState string, boards []port.BoardDelegationBoard) string {
	lines := make([]string, 0, len(bounds)+len(boards)+2)
	if management.ID != "" {
		lines = append(lines, "management:"+management.ID+":"+management.Status+":"+strconv.FormatInt(management.Version, 10)+":"+strconv.FormatInt(management.NotBefore.Unix(), 10)+":"+strconv.FormatInt(management.ExpiresAt.Unix(), 10))
	}
	for _, b := range bounds {
		lines = append(lines, "bound:"+b.ID+":"+b.Status+":"+strconv.FormatInt(b.Version, 10)+":"+b.Action+":"+b.BoardID+":"+strconv.FormatInt(b.NotBefore.Unix(), 10)+":"+strconv.FormatInt(b.ExpiresAt.Unix(), 10)+":"+b.RequiredStrength+":"+strconv.FormatBool(b.Delegable))
	}
	for _, b := range boards {
		lines = append(lines, "board:"+b.ID+":"+b.Status)
	}
	lines = append(lines, "recipient:"+recipientID+":"+recipientState)
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "fv-" + hex.EncodeToString(sum[:16])
}

func toBound(d repository.Delegation) port.BoardDelegationBound {
	return port.BoardDelegationBound{
		Action: d.Action, BoardID: d.BoardID,
		NotBefore: d.NotBefore.Unix(), ExpiresAt: d.ExpiresAt.Unix(),
		RequiredStrength: d.RequiredStrength, ID: d.ID,
		Actor:  port.PrincipalRef{Kind: "admin", ID: d.SubjectID},
		Status: d.Status, Delegable: d.Delegable,
	}
}

// deny builds a closed decision without calling the policy when a required
// host fact (such as management authority) does not exist at all.
func deny(requestID, version, reason string) port.BoardDelegationDecision {
	return port.BoardDelegationDecision{
		Contract: port.BoardDelegationContract, RequestID: requestID, FactsVersion: version,
		Effect: "deny", Reason: reason, Witnesses: []port.BoardDelegationWitness{}, Obligations: []string{},
	}
}

// grantDeniedError reports a deny that only became visible during the
// in-transaction recheck; the proposal is rejected with its stable reason.
type grantDeniedError struct{ reason string }

func (e *grantDeniedError) Error() string { return e.reason }

// GrantDeniedReason extracts the stable denial reason when the recheck denied.
func GrantDeniedReason(err error) (string, bool) {
	var value *grantDeniedError
	if errors.As(err, &value) {
		return value.reason, true
	}
	return "", false
}

// buildRequest loads every current fact for one evaluation. When forUpdate is
// set and the repository supports it, management/bound rows are read with row
// locks so a concurrent revocation either blocks or fails the recheck.
func (s *Service) buildRequest(ctx context.Context, actor Actor, recipientID string, atoms []port.BoardGrantAtom, boards BoardFactsProvider, requestID string, forUpdate bool) (port.BoardDelegationRequest, error) {
	listAuthority := s.delegations.ListByKindSubject
	if forUpdate {
		if locker, ok := s.delegations.(repository.DelegationAuthorityLocker); ok {
			listAuthority = locker.ListByKindSubjectForUpdate
		}
	}
	managementRows, err := listAuthority(ctx, repository.DelegationKindManagement, actor.ID)
	if err != nil {
		return port.BoardDelegationRequest{}, err
	}
	boundRows, err := listAuthority(ctx, repository.DelegationKindBound, actor.ID)
	if err != nil {
		return port.BoardDelegationRequest{}, err
	}
	user, err := s.users.GetByID(ctx, recipientID)
	if err != nil {
		return port.BoardDelegationRequest{}, err
	}
	boardFacts, err := boards(ctx)
	if err != nil {
		return port.BoardDelegationRequest{}, err
	}
	if len(managementRows) == 0 {
		version := factsVersion(repository.Delegation{}, boundRows, recipientID, recipientStatus(user.Status), boardFacts)
		return port.BoardDelegationRequest{}, &noManagementError{decision: deny(requestID, version, "delegation.management_denied")}
	}
	// At most one current management row is expected per admin; the latest
	// expiry wins so an extension never silently drops authority.
	sort.Slice(managementRows, func(i, j int) bool { return managementRows[i].ExpiresAt.After(managementRows[j].ExpiresAt) })
	management := managementRows[0]
	bounds := make([]port.BoardDelegationBound, 0, len(boundRows))
	for _, row := range boundRows {
		bounds = append(bounds, toBound(row))
	}
	request := port.BoardDelegationRequest{
		Contract:    port.BoardDelegationContract,
		RequestID:   requestID,
		Principal:   s.principal(actor),
		EvaluatedAt: s.now().Unix(),
		Management: port.BoardDelegationManagement{
			Actor:  port.PrincipalRef{Kind: "admin", ID: management.SubjectID},
			Action: management.Action, Status: management.Status,
			NotBefore: management.NotBefore.Unix(), ExpiresAt: management.ExpiresAt.Unix(),
		},
		Recipient: port.BoardDelegationRecipient{
			Subject: port.PrincipalRef{Kind: "user", ID: recipientID},
			Status:  recipientStatus(user.Status),
		},
		Boards:    boardFacts,
		Bounds:    bounds,
		Candidate: port.BoardDelegationCandidate{Recipient: port.PrincipalRef{Kind: "user", ID: recipientID}, Grants: atoms},
	}
	request.FactsVersion = factsVersion(management, boundRows, recipientID, request.Recipient.Status, boardFacts)
	return request, nil
}

type noManagementError struct {
	decision port.BoardDelegationDecision
}

func (e *noManagementError) Error() string { return "delegation.management_denied" }

func noManagementDecision(err error) (port.BoardDelegationDecision, bool) {
	var value *noManagementError
	if errors.As(err, &value) {
		return value.decision, true
	}
	return port.BoardDelegationDecision{}, false
}

// GrantBoardDelegations evaluates a batch proposal and, when allowed, commits
// the grants, the required audit and the durable event in one transaction.
// A deny decision is returned without an error; infrastructure failures are
// errors. The caller owns HTTP mapping and any deny-side audit.
func (s *Service) GrantBoardDelegations(ctx context.Context, actor Actor, recipientID string, candidates []GrantCandidate, boards BoardFactsProvider, requestID string) (port.BoardDelegationDecision, error) {
	if s.delegations == nil || s.users == nil || boards == nil {
		return port.BoardDelegationDecision{}, ErrDelegationUnavailable
	}
	atoms := make([]port.BoardGrantAtom, 0, len(candidates))
	for _, c := range candidates {
		atom := port.BoardGrantAtom{Action: c.Action, BoardID: c.BoardID, RequiredStrength: c.RequiredStrength}
		if atom.RequiredStrength == "" {
			atom.RequiredStrength = "password"
		}
		atom.NotBefore = s.now().Unix()
		if !c.NotBefore.IsZero() {
			atom.NotBefore = c.NotBefore.Unix()
		}
		if !c.ExpiresAt.IsZero() {
			atom.ExpiresAt = c.ExpiresAt.Unix()
		}
		atoms = append(atoms, atom)
	}
	request, err := s.buildRequest(ctx, actor, recipientID, atoms, boards, requestID, false)
	if err != nil {
		if decision, ok := noManagementDecision(err); ok {
			return decision, nil
		}
		return port.BoardDelegationDecision{}, err
	}
	// Host default expiry: the earlier of now+365d and the best covering
	// bound's expiry. Explicit candidate windows are left untouched.
	for i := range request.Candidate.Grants {
		if !candidates[i].ExpiresAt.IsZero() {
			continue
		}
		atom := &request.Candidate.Grants[i]
		boundCap := int64(0)
		for _, b := range request.Bounds {
			if b.Action == atom.Action && b.BoardID == atom.BoardID && b.Status == "active" && b.Delegable && b.ExpiresAt > boundCap {
				boundCap = b.ExpiresAt
			}
		}
		defaultExpiry := s.now().Add(365 * 24 * time.Hour).Unix()
		if boundCap > 0 && boundCap < defaultExpiry {
			defaultExpiry = boundCap
		}
		atom.ExpiresAt = defaultExpiry
	}
	if err := request.Validate(); err != nil {
		return port.BoardDelegationDecision{}, err
	}
	decision, err := s.policy.DecideBoardDelegation(request)
	if err != nil {
		return port.BoardDelegationDecision{}, err
	}
	if decision.Effect != "allow" {
		return decision, nil
	}
	commitErr := s.executeGrantCommand(ctx, actor, recipientID, request, decision, boards)
	if commitErr != nil {
		if reason, ok := GrantDeniedReason(commitErr); ok {
			return deny(request.RequestID, request.FactsVersion, reason), nil
		}
		return port.BoardDelegationDecision{}, commitErr
	}
	return decision, nil
}

// executeGrantCommand reruns the whole fact load and decision binding inside
// the write transaction so a concurrent revocation or suspension fails closed.
func (s *Service) executeGrantCommand(ctx context.Context, actor Actor, recipientID string, request port.BoardDelegationRequest, decision port.BoardDelegationDecision, boards BoardFactsProvider) error {
	action := func(commandCtx context.Context) error {
		current, err := s.buildRequest(commandCtx, actor, recipientID, request.Candidate.Grants, boards, request.RequestID, true)
		if err != nil {
			if denyDecision, ok := noManagementDecision(err); ok {
				return &grantDeniedError{reason: denyDecision.Reason}
			}
			return err
		}
		if err := policy.CheckBoardDelegationDecision(request, decision, current); err != nil {
			var denyError *port.DelegationDenyError
			if errors.As(err, &denyError) {
				return &grantDeniedError{reason: denyError.Reason}
			}
			return ErrDelegationFactsChanged
		}
		grants := make([]repository.Delegation, 0, len(request.Candidate.Grants))
		for i, atom := range request.Candidate.Grants {
			grants = append(grants, repository.Delegation{
				ID:   fmt.Sprintf("grant-%s-%d", request.RequestID, i),
				Kind: repository.DelegationKindGrant, SubjectKind: "user", SubjectID: recipientID,
				Action: atom.Action, BoardID: atom.BoardID,
				NotBefore: time.Unix(atom.NotBefore, 0), ExpiresAt: time.Unix(atom.ExpiresAt, 0),
				RequiredStrength: atom.RequiredStrength, CreatedBy: actor.ID,
			})
		}
		if err := s.delegations.InsertDelegations(commandCtx, grants); err != nil {
			return err
		}
		if s.catalog != nil {
			if err := s.catalog.RecordAuthorizationAudit(commandCtx, repository.AuthorizationAudit{
				ActorKind: "admin", ActorID: actor.ID,
				PermissionCode: "identity.role.assign", OperationCode: "identity.delegation.grant",
				ResourceType: "identity_delegation", ResourceID: recipientID, Outcome: "allow",
				RequestID: request.RequestID,
			}); err != nil {
				return err
			}
		}
		return nil
	}
	if s.reliable == nil || transaction.Active(ctx) {
		return action(ctx)
	}
	event, err := reliability.NewEvent("authorization.changed", "identity_delegation", recipientID, map[string]string{
		"command": "identity.delegation.grant", "actor_id": actor.ID, "resource_type": "identity_delegation", "resource_id": recipientID,
	})
	if err != nil {
		return err
	}
	return s.reliable.Execute(ctx, reliability.Command{
		Code: "identity.delegation.grant", ActorID: actor.ID, ActorType: "admin",
		ResourceType: "identity_delegation", ResourceID: recipientID,
		OperationCode: "identity.delegation.grant", PermissionCode: "identity.role.assign",
		Event: &event,
	}, action)
}

// requireManagementAuthority gates bound administration on a current
// identity.role.assign management window held by an MFA admin.
func (s *Service) requireManagementAuthority(ctx context.Context, actor Actor) error {
	if actor.AuthenticationStrength != "mfa" {
		return errors.New("delegation.mfa_required")
	}
	rows, err := s.delegations.ListByKindSubject(ctx, repository.DelegationKindManagement, actor.ID)
	if err != nil {
		return err
	}
	now := s.now()
	for _, row := range rows {
		if row.Status == repository.DelegationStatusActive && !row.NotBefore.After(now) && now.Before(row.ExpiresAt) {
			return nil
		}
	}
	return errors.New("delegation.management_denied")
}

// CreateDelegationBound adds a new delegable ceiling for the calling admin.
// Existing bounds are never edited in place; changing a window or strength is
// a revoke plus a new bound.
func (s *Service) CreateDelegationBound(ctx context.Context, actor Actor, bound repository.Delegation) (repository.Delegation, error) {
	if err := s.requireManagementAuthority(ctx, actor); err != nil {
		return repository.Delegation{}, err
	}
	bound.Kind = repository.DelegationKindBound
	bound.SubjectKind = "admin"
	bound.SubjectID = actor.ID
	bound.Delegable = true
	bound.CreatedBy = actor.ID
	if err := s.delegations.InsertDelegations(ctx, []repository.Delegation{bound}); err != nil {
		return repository.Delegation{}, err
	}
	return s.delegations.GetDelegation(ctx, bound.ID)
}

// RevokeDelegation permanently revokes a bound or grant under the calling
// admin's management authority. Revocation is visible to new evaluations as
// soon as the transaction commits.
func (s *Service) RevokeDelegation(ctx context.Context, actor Actor, id string) error {
	if err := s.requireManagementAuthority(ctx, actor); err != nil {
		return err
	}
	action := func(commandCtx context.Context) error {
		current, err := s.delegations.GetDelegation(commandCtx, id)
		if err != nil {
			return err
		}
		_, changed, err := s.delegations.SetStatusCAS(commandCtx, id, current.Version, repository.DelegationStatusRevoked, s.now())
		if err != nil {
			return err
		}
		if !changed {
			return repository.ErrDelegationVersionConflict
		}
		if s.catalog != nil {
			return s.catalog.RecordAuthorizationAudit(commandCtx, repository.AuthorizationAudit{
				ActorKind: "admin", ActorID: actor.ID,
				PermissionCode: "identity.role.assign", OperationCode: "identity.delegation.revoke",
				ResourceType: "identity_delegation", ResourceID: id, Outcome: "allow",
			})
		}
		return nil
	}
	if s.reliable == nil || transaction.Active(ctx) {
		return action(ctx)
	}
	event, err := reliability.NewEvent("authorization.changed", "identity_delegation", id, map[string]string{
		"command": "identity.delegation.revoke", "actor_id": actor.ID, "resource_type": "identity_delegation", "resource_id": id,
	})
	if err != nil {
		return err
	}
	return s.reliable.Execute(ctx, reliability.Command{
		Code: "identity.delegation.revoke", ActorID: actor.ID, ActorType: "admin",
		ResourceType: "identity_delegation", ResourceID: id,
		OperationCode: "identity.delegation.revoke", PermissionCode: "identity.role.assign",
		Event: &event,
	}, action)
}

// HasActiveGovernanceGrant reports the current single-witness execution right
// used by the authorization read path. A grant requiring MFA is satisfied only
// by an MFA-strength caller; without an entry-constructed strength the call
// fails closed for those grants.
func (s *Service) HasActiveGovernanceGrant(ctx context.Context, userID, action, boardID, strength string, at time.Time) (bool, error) {
	rows, err := s.delegations.ListByKindSubject(ctx, repository.DelegationKindGrant, userID)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.Action != action || row.BoardID != boardID || row.Status != repository.DelegationStatusActive ||
			row.NotBefore.After(at) || !at.Before(row.ExpiresAt) {
			continue
		}
		if row.RequiredStrength == "password" || strength == "mfa" {
			return true, nil
		}
	}
	return false, nil
}

// HasAnyActiveGovernanceGrant is the route pre-filter counterpart of
// HasActiveGovernanceGrant: any current grant for the action on any board.
func (s *Service) HasAnyActiveGovernanceGrant(ctx context.Context, userID, action, strength string, at time.Time) (bool, error) {
	rows, err := s.delegations.ListByKindSubject(ctx, repository.DelegationKindGrant, userID)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.Action != action || row.Status != repository.DelegationStatusActive ||
			row.NotBefore.After(at) || !at.Before(row.ExpiresAt) {
			continue
		}
		if row.RequiredStrength == "password" || strength == "mfa" {
			return true, nil
		}
	}
	return false, nil
}

// ListActiveGrants exposes one user's current delegation grants for moderator
// listings and the replace flow.
func (s *Service) ListActiveGrants(ctx context.Context, userID string) ([]repository.Delegation, error) {
	rows, err := s.delegations.ListByKindSubject(ctx, repository.DelegationKindGrant, userID)
	if err != nil {
		return nil, err
	}
	items := make([]repository.Delegation, 0, len(rows))
	for _, row := range rows {
		if row.Status == repository.DelegationStatusActive {
			items = append(items, row)
		}
	}
	return items, nil
}
