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

type ThreadAdminPurgeResult struct {
	ID             string                `json:"id"`
	DeletionStatus domain.DeletionStatus `json:"deletion_status"`
}

func adminPurgeRequest(thread *domain.Thread, authority identityport.AdminExecutionAuthority, operation, requestID string, evaluatedAt int64) (identityport.ThreadAdminPurgeRequest, error) {
	if thread == nil {
		return identityport.ThreadAdminPurgeRequest{}, identityport.ErrPolicyFactsChanged
	}
	facts, err := publicThreadFacts(thread, thread.ID)
	if err != nil {
		return identityport.ThreadAdminPurgeRequest{}, err
	}
	return identityport.ThreadAdminPurgeRequest{Contract: identityport.ThreadAdminPurgeContract, PolicyVersion: identityport.ThreadAdminPurgePolicyVersion, RequestID: requestID, Operation: operation, Principal: authority.Principal, Thread: facts, ExecutionQuery: authority.Query, ExecutionAllowed: authority.Allowed, PhysicalDeleted: thread.DeletedAt != nil, ExecutionExpiresAtMS: authority.ExpiresAtMS, EvaluatedAtMS: evaluatedAt}, nil
}

// AdminPurgeThread commits a checked tombstone and immutable typed evidence.
// The final proof uses the command snapshot even though normal reads are closed.
func (s *ThreadService) AdminPurgeThread(ctx context.Context, id, operation, reason string, resolve identityport.SessionPrincipalResolver) (*ThreadAdminPurgeResult, error) {
	if operation != "purge" {
		return nil, identityport.ErrResourcePolicyUnknownAction
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, ErrModerationReasonRequired
	}
	if s == nil || s.reliable == nil || nilGovernancePort(s.adminExecution) || nilGovernancePort(s.governance) {
		return nil, ErrThreadGovernanceUnavailable
	}
	guarded, ok := s.repo.(repository.ThreadPurgeCommandRepository)
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
	q := identityport.AdminExecutionQuery{PermissionCode: "community.thread." + operation, Scope: identityport.PolicyResourceRef{Kind: "community.board", ID: initial.CategoryID}}
	authority, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, false)
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	requestID := strconv.FormatInt(idgen.New(), 10)
	evaluatedAt := time.Now().UnixMilli()
	request, err := adminPurgeRequest(initial, authority, operation, requestID, evaluatedAt)
	if err != nil {
		return nil, err
	}
	decision, err := (policy.ThreadAdminPurgePolicy{}).DecideThreadAdminPurge(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		return nil, ErrThreadGovernanceDenied
	}
	actor := authority.Principal.Actor
	var result *ThreadAdminPurgeResult
	err = s.reliable.Execute(ctx, reliability.Command{Code: q.PermissionCode, ActorID: actor.ID, ActorType: actor.Kind, ResourceType: "thread", ResourceID: id, OperationCode: q.PermissionCode, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent(eventbus.EventThreadDeleted, "thread", id, result)
	}}, func(tx context.Context) error {
		currentAuthority, err := s.adminExecution.LoadAdminExecutionAuthority(tx, q, resolve, true)
		if err != nil {
			return err
		}
		if err := identityport.CheckAdminExecutionAuthority(authority, currentAuthority); err != nil {
			return err
		}
		current, err := guarded.LockThreadGovernanceSnapshot(tx, id)
		if err != nil {
			return err
		}
		currentRequest, err := adminPurgeRequest(current, currentAuthority, operation, requestID, evaluatedAt)
		if err != nil {
			return err
		}
		if err := identityport.CheckThreadAdminPurgeDecision(request, decision, currentRequest); err != nil {
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
		written, err := guarded.PurgeThreadIfFacts(tx, id, expectedVersion, current.CurrentRevision)
		if err != nil {
			return err
		}
		if written == nil || written.DeletedAt == nil || written.DeletionStatus != domain.DeletionStatusPurged || written.CurrentRevision != current.CurrentRevision+1 || written.FactsVersion <= expectedVersion {
			return identityport.ErrPolicyFactsChanged
		}
		expected := *current
		expected.DeletedAt = written.DeletedAt
		expected.DeletionStatus = domain.DeletionStatusPurged
		expected.Status = domain.ThreadStatusArchived
		expected.CurrentRevision = written.CurrentRevision
		expected.FactsVersion = written.FactsVersion
		expected.UpdatedAt = written.UpdatedAt
		if !sameGovernanceThread(&expected, written) {
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
		expectedLatestCase = expectedCase

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
		final, err := guarded.LockThreadGovernanceSnapshot(tx, id)
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
		result = &ThreadAdminPurgeResult{ID: id, DeletionStatus: final.DeletionStatus}
		return nil
	})
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	s.invalidateListCache(ctx)
	return result, nil
}
