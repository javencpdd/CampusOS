package service

import (
	"context"
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

func adminReviewRequest(thread *domain.Thread, authority identityport.AdminExecutionAuthority, operation, requestID string, evaluatedAt int64) (identityport.ThreadAdminReviewRequest, error) {
	if thread == nil {
		return identityport.ThreadAdminReviewRequest{}, identityport.ErrPolicyFactsChanged
	}
	facts, err := publicThreadFacts(thread, thread.ID)
	if err != nil {
		return identityport.ThreadAdminReviewRequest{}, err
	}
	return identityport.ThreadAdminReviewRequest{Contract: identityport.ThreadAdminReviewContract, PolicyVersion: identityport.ThreadAdminReviewPolicyVersion, RequestID: requestID, Operation: operation, Principal: authority.Principal, Thread: facts, ExecutionQuery: authority.Query, ExecutionAllowed: authority.Allowed, ExecutionExpiresAtMS: authority.ExpiresAtMS, EvaluatedAtMS: evaluatedAt}, nil
}

// AdminReviewThread owns the actual resource policy and history transition.
// It never returns the original content solely because review was authorized.
func (s *ThreadService) AdminReviewThread(ctx context.Context, id, operation, reason string, resolve identityport.SessionPrincipalResolver) (*ThreadGovernanceResult, error) {
	if operation != "approve" && operation != "reject" {
		return nil, identityport.ErrResourcePolicyUnknownAction
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, ErrModerationReasonRequired
	}
	if s == nil || s.reliable == nil || nilGovernancePort(s.adminExecution) || nilGovernancePort(s.governance) {
		return nil, ErrThreadGovernanceUnavailable
	}
	guarded, ok := s.repo.(repository.FactsGuardedThreadRepository)
	if !ok || nilGovernancePort(guarded) {
		return nil, ErrThreadGovernanceUnavailable
	}
	cases, ok := s.governance.(repository.ModerationCaseCommandRepository)
	if !ok || nilGovernancePort(cases) {
		return nil, ErrThreadGovernanceUnavailable
	}
	initial, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	if initial == nil {
		return nil, identityport.ErrPolicyFactsChanged
	}
	q := identityport.AdminExecutionQuery{PermissionCode: "community.thread.review", Scope: identityport.PolicyResourceRef{Kind: "community.board", ID: initial.CategoryID}}
	authority, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, false)
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	requestID := strconv.FormatInt(idgen.New(), 10)
	evaluatedAt := time.Now().UnixMilli()
	request, err := adminReviewRequest(initial, authority, operation, requestID, evaluatedAt)
	if err != nil {
		return nil, err
	}
	decision, err := (policy.ThreadAdminReviewPolicy{}).DecideThreadAdminReview(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		return nil, ErrThreadGovernanceDenied
	}
	actor := authority.Principal.Actor
	var result *ThreadGovernanceResult
	err = s.reliable.Execute(ctx, reliability.Command{Code: q.PermissionCode, ActorID: actor.ID, ActorType: actor.Kind, ResourceType: "thread", ResourceID: id, OperationCode: q.PermissionCode, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent(eventbus.EventThreadUpdated, "thread", id, result)
	}}, func(tx context.Context) error {
		currentAuthority, err := s.adminExecution.LoadAdminExecutionAuthority(tx, q, resolve, true)
		if err != nil {
			return err
		}
		if err := identityport.CheckAdminExecutionAuthority(authority, currentAuthority); err != nil {
			return err
		}
		current, err := guarded.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		currentRequest, err := adminReviewRequest(current, currentAuthority, operation, requestID, evaluatedAt)
		if err != nil {
			return err
		}
		if err := identityport.CheckThreadAdminReviewDecision(request, decision, currentRequest); err != nil {
			return err
		}
		if !sameGovernanceThread(initial, current) {
			return identityport.ErrPolicyFactsChanged
		}
		open, err := cases.LockLatestOpenCase(tx, id)
		if err != nil {
			return err
		}
		if open != nil && (open.ThreadID != id || open.ResolvedAt != nil) {
			return identityport.ErrPolicyFactsChanged
		}
		before := domain.ContentStateSummary(current)
		expectedVersion := current.FactsVersion
		now := time.Now().UTC()
		current.ModerationStatus = domain.ModerationStatusClear
		if operation == "reject" {
			current.ModerationStatus = domain.ModerationStatusRejected
		}
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
		if written == nil || written.FactsVersion <= expectedVersion || written.FactsVersion != current.FactsVersion || written.CurrentRevision != current.CurrentRevision || written.ModerationStatus != current.ModerationStatus || written.ModerationActorKind != actor.Kind || written.ModerationActorID != actor.ID {
			return identityport.ErrPolicyFactsChanged
		}
		if err := s.governance.CreateRevision(tx, &domain.ContentRevision{ID: strconv.FormatInt(idgen.New(), 10), ThreadID: id, Version: written.CurrentRevision, Title: written.Title, Content: written.Content, ContentFormat: written.ContentFormat, Tags: written.Tags, Action: operation, Reason: reason, CreatedActorKind: actor.Kind, CreatedActorID: actor.ID, CreatedAt: now}); err != nil {
			return err
		}
		var expectedCase *domain.ModerationCase
		var expectedLatestCase *domain.ModerationCase
		caseID := ""
		if open != nil {
			caseID = open.ID
			expectedCase = open
		}
		if operation == "reject" && open == nil {
			caseID = strconv.FormatInt(idgen.New(), 10)
			if err := s.governance.CreateModerationCase(tx, &domain.ModerationCase{ID: caseID, ThreadID: id, Status: "rejected", Reason: reason, OpenedActorKind: actor.Kind, OpenedActorID: actor.ID, OpenedAt: now}); err != nil {
				return err
			}
			expectedCase, err = cases.LockModerationCase(tx, caseID)
			if err != nil {
				return err
			}
			if expectedCase == nil {
				return identityport.ErrPolicyFactsChanged
			}
		}
		if operation == "approve" && open != nil {
			expectedCase, err = cases.ResolveCaseForActor(tx, open, "resolved_approve", actor.Kind, actor.ID, now)
			if err != nil {
				return err
			}
			if expectedCase == nil || expectedCase.ResolvedAt == nil || expectedCase.ResolvedActorKind != actor.Kind || expectedCase.ResolvedActorID != actor.ID || expectedCase.ResolvedBy != "" {
				return identityport.ErrPolicyFactsChanged
			}
		}
		if operation == "reject" {
			expectedLatestCase = expectedCase
		} else if open != nil {
			expectedLatestCase, err = cases.LockLatestOpenCase(tx, id)
			if err != nil {
				return err
			}
		}
		if err := s.governance.CreateModerationAction(tx, &domain.ModerationAction{ID: strconv.FormatInt(idgen.New(), 10), CaseID: caseID, ThreadID: id, Action: operation, Reason: reason, ActorKind: actor.Kind, ActorRefID: actor.ID, BeforeState: before, AfterState: domain.ContentStateSummary(written), CreatedAt: now}); err != nil {
			return err
		}
		finalAuthority, err := s.adminExecution.LoadAdminExecutionAuthority(tx, q, resolve, true)
		if err != nil {
			return err
		}
		if err := identityport.CheckAdminExecutionAuthority(currentAuthority, finalAuthority); err != nil {
			return err
		}
		final, err := guarded.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		if !sameGovernanceThread(written, final) {
			return identityport.ErrPolicyFactsChanged
		}
		latest, err := cases.LockLatestOpenCase(tx, id)
		if err != nil {
			return err
		}
		if expectedCase != nil {
			actual, err := cases.LockModerationCase(tx, caseID)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(actual, expectedCase) {
				return identityport.ErrPolicyFactsChanged
			}
		}
		if !reflect.DeepEqual(latest, expectedLatestCase) {
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
