package service

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var (
	ErrThreadGovernanceDenied      = errors.New("thread governance denied")
	ErrThreadGovernanceUnavailable = errors.New("thread governance unavailable")
)

// ThreadGovernanceResult contains no private body, title, owner or bound policy
// facts. Executing a governance action does not grant permission to read them.
type ThreadGovernanceResult struct {
	ID               string                  `json:"id"`
	ModerationStatus domain.ModerationStatus `json:"moderation_status"`
}

func (s *ThreadService) SetGovernanceAuthority(reader identityport.BoardGovernanceAuthorityReader) {
	s.governanceAuthority = reader
}
func nilGovernancePort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return r.IsNil()
	}
	return false
}

func threadGovernanceError(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{identityport.ErrAdminSessionPrincipalUnavailable, identityport.ErrUserSessionPrincipalUnavailable, identityport.ErrPolicyFactsChanged, identityport.ErrResourcePolicyFactsChanged, repository.ErrThreadNotFound, repository.ErrThreadRevisionConflict, repository.ErrModerationCaseChanged, ErrThreadStateConflict, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, known) {
			return err
		}
	}
	return errors.Join(ErrThreadGovernanceUnavailable, err)
}
func takeDownPolicyRequest(t *domain.Thread, a identityport.BoardGovernanceAuthority, requestID string) (identityport.ResourcePolicyRequest, error) {
	f, err := publicThreadFacts(t, t.ID)
	if err != nil {
		return identityport.ResourcePolicyRequest{}, err
	}
	status := "active"
	if f.DeletionStatus != "active" {
		status = "deleted"
	}
	return identityport.ResourcePolicyRequest{Contract: identityport.ResourcePolicyContract, RequestID: requestID, Principal: a.Principal, Action: "community.thread.take_down", Facts: identityport.ResourcePolicyFacts{Kind: "thread", ID: t.ID, OwnerID: t.AuthorID, BoardID: t.CategoryID, Status: status, PublicationStatus: f.PublicationStatus, Version: f.FactsVersion}, Grants: a.Grants, EvaluatedAtMS: time.Now().UnixMilli(), PolicyVersion: identityport.ResourcePolicyContract}, nil
}
func sameGovernanceThread(a, b *domain.Thread) bool {
	if a == nil || b == nil {
		return false
	}
	x, y := *a, *b
	x.ViewCount, x.ReplyCount, x.LikeCount = 0, 0, 0
	y.ViewCount, y.ReplyCount, y.LikeCount = 0, 0, 0
	return reflect.DeepEqual(x, y)
}

func (s *ThreadService) TakeDownForPrincipal(ctx context.Context, id, reason string, resolve identityport.SessionPrincipalResolver) (*ThreadGovernanceResult, error) {
	if s == nil || s.reliable == nil || nilGovernancePort(s.governance) || nilGovernancePort(s.governanceAuthority) {
		return nil, ErrThreadGovernanceUnavailable
	}
	guarded, ok := s.repo.(repository.FactsGuardedThreadRepository)
	if !ok || nilGovernancePort(guarded) {
		return nil, ErrThreadGovernanceUnavailable
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, ErrModerationReasonRequired
	}
	initial, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	if initial == nil {
		return nil, identityport.ErrPolicyFactsChanged
	}
	if err := checkFeatureGovernanceInitial(ctx, initial); err != nil {
		return nil, err
	}
	q := identityport.BoardGovernanceQuery{BoardID: initial.CategoryID, Action: "community.thread.take_down"}
	authority, err := s.governanceAuthority.LoadBoardGovernanceAuthority(ctx, q, resolve, false)
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	requestID := strconv.FormatInt(idgen.New(), 10)
	request, err := takeDownPolicyRequest(initial, authority, requestID)
	if err != nil {
		return nil, err
	}
	decision, err := (policy.ResourcePolicy{}).DecideResource(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		return nil, ErrThreadGovernanceDenied
	}
	if initial.ModerationStatus == domain.ModerationStatusTakenDown {
		return nil, ErrThreadStateConflict
	}
	actor := authority.Principal.Actor
	var result *ThreadGovernanceResult
	err = s.reliable.Execute(ctx, reliability.Command{Code: q.Action, ActorID: actor.ID, ActorType: actor.Kind, ResourceType: "thread", ResourceID: id, OperationCode: q.Action, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent(eventbus.EventThreadUpdated, "thread", id, result)
	}}, func(tx context.Context) error {
		currentAuthority, err := s.governanceAuthority.LoadBoardGovernanceAuthority(tx, q, resolve, true)
		if err != nil {
			return err
		}
		if err := identityport.CheckBoardGovernanceAuthority(authority, currentAuthority); err != nil {
			return err
		}
		current, err := guarded.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		currentRequest, err := takeDownPolicyRequest(current, currentAuthority, requestID)
		if err != nil {
			return err
		}
		if err := policy.CheckResourceDecision(request, decision, currentRequest); err != nil {
			return err
		}
		if current.ModerationStatus == domain.ModerationStatusTakenDown {
			return ErrThreadStateConflict
		}
		if err := lockFeatureGovernance(tx, current); err != nil {
			return err
		}
		before := domain.ContentStateSummary(current)
		expectedVersion := current.FactsVersion
		now := time.Now().UTC()
		current.ModerationStatus = domain.ModerationStatusTakenDown
		current.ModerationReason = reason
		current.SetModerationActor(actor.Kind, actor.ID)
		current.ModerationAt = &now
		current.CurrentRevision++
		current.UpdatedAt = now
		current.SyncLegacyStatus()
		if err := guarded.UpdateIfFactsVersion(tx, current, expectedVersion); err != nil {
			return err
		}
		written, err := s.repo.GetByID(tx, id)
		if err != nil {
			return err
		}
		if written == nil || written.FactsVersion != current.FactsVersion || written.ModerationActorKind != actor.Kind || written.ModerationActorID != actor.ID || written.ModerationStatus != domain.ModerationStatusTakenDown {
			return identityport.ErrPolicyFactsChanged
		}
		if err := applyFeatureGovernance(tx, written, actor); err != nil {
			return err
		}
		legacyUser := ""
		if actor.Kind == "user" {
			legacyUser = actor.ID
		}
		if err := s.governance.CreateRevision(tx, &domain.ContentRevision{ID: strconv.FormatInt(idgen.New(), 10), ThreadID: id, Version: written.CurrentRevision, Title: written.Title, Content: written.Content, ContentFormat: written.ContentFormat, Tags: written.Tags, Action: "take_down", Reason: reason, CreatedBy: legacyUser, CreatedActorKind: actor.Kind, CreatedActorID: actor.ID, CreatedAt: now}); err != nil {
			return err
		}
		existing, err := s.governance.LatestOpenCase(tx, id)
		if err != nil {
			return err
		}
		caseID := ""
		if existing != nil {
			caseID = existing.ID
		} else {
			caseID = strconv.FormatInt(idgen.New(), 10)
			if err := s.governance.CreateModerationCase(tx, &domain.ModerationCase{ID: caseID, ThreadID: id, Status: "taken_down", Reason: reason, OpenedBy: legacyUser, OpenedActorKind: actor.Kind, OpenedActorID: actor.ID, OpenedAt: now}); err != nil {
				return err
			}
		}
		if err := s.governance.CreateModerationAction(tx, &domain.ModerationAction{ID: strconv.FormatInt(idgen.New(), 10), CaseID: caseID, ThreadID: id, Action: "take_down", Reason: reason, ActorID: legacyUser, ActorKind: actor.Kind, ActorRefID: actor.ID, BeforeState: before, AfterState: domain.ContentStateSummary(written), CreatedAt: now}); err != nil {
			return err
		}
		if s.notifications != nil && (actor.Kind != "user" || actor.ID != written.AuthorID) {
			if err := s.notifications.NotifyThreadTakenDown(tx, written.AuthorID, id, written.Title, reason); err != nil {
				return err
			}
		}
		if err := checkFeatureGovernanceFinal(tx, written, actor); err != nil {
			return err
		}
		finalAuthority, err := s.governanceAuthority.LoadBoardGovernanceAuthority(tx, q, resolve, true)
		if err != nil {
			return err
		}
		if err := identityport.CheckBoardGovernanceAuthority(currentAuthority, finalAuthority); err != nil {
			return err
		}
		final, err := guarded.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		if !sameGovernanceThread(written, final) {
			return identityport.ErrPolicyFactsChanged
		}
		result = &ThreadGovernanceResult{ID: id, ModerationStatus: final.ModerationStatus}
		return nil
	})
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	s.invalidateListCache(ctx)
	return result, nil
}
