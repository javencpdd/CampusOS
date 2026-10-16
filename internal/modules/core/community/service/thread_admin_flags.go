package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

type ThreadAdminFlagResult struct {
	ID     string `json:"id"`
	Pinned bool   `json:"is_pinned"`
	Locked bool   `json:"is_locked"`
}

func (s *ThreadService) SetAdminExecutionAuthority(reader identityport.AdminExecutionAuthorityReader) {
	s.adminExecution = reader
}

func adminFlagCode(operation string) (string, error) {
	switch operation {
	case "pin", "unpin":
		return "community.thread.pin", nil
	case "lock", "unlock":
		return "community.thread.lock", nil
	default:
		return "", identityport.ErrResourcePolicyUnknownAction
	}
}

func adminFlagRequest(thread *domain.Thread, authority identityport.AdminExecutionAuthority, operation, requestID string, evaluatedAt int64) (identityport.ThreadAdminFlagRequest, error) {
	if thread == nil {
		return identityport.ThreadAdminFlagRequest{}, identityport.ErrPolicyFactsChanged
	}
	facts, err := publicThreadFacts(thread, thread.ID)
	if err != nil {
		return identityport.ThreadAdminFlagRequest{}, err
	}
	return identityport.ThreadAdminFlagRequest{Contract: identityport.ThreadAdminFlagContract, PolicyVersion: identityport.ThreadAdminFlagPolicyVersion, RequestID: requestID, Operation: operation, Principal: authority.Principal, Thread: facts, Pinned: thread.IsPinned, Locked: thread.IsLocked, ExecutionQuery: authority.Query, ExecutionAllowed: authority.Allowed, ExecutionExpiresAtMS: authority.ExpiresAtMS, EvaluatedAtMS: evaluatedAt}, nil
}

func adminFlagState(thread *domain.Thread) string {
	return fmt.Sprintf("%s,pinned:%t,locked:%t", domain.ContentStateSummary(thread), thread.IsPinned, thread.IsLocked)
}

// AdminSetThreadFlag authenticates an independent current Admin. The operation
// selects a fixed named permission; scope always comes from the actual row.
func (s *ThreadService) AdminSetThreadFlag(ctx context.Context, id, operation string, resolve identityport.SessionPrincipalResolver) (*ThreadAdminFlagResult, error) {
	code, err := adminFlagCode(operation)
	if err != nil {
		return nil, err
	}
	if s == nil || s.reliable == nil || nilGovernancePort(s.adminExecution) || nilGovernancePort(s.governance) {
		return nil, ErrThreadGovernanceUnavailable
	}
	guarded, ok := s.repo.(repository.FactsGuardedThreadRepository)
	if !ok || nilGovernancePort(guarded) {
		return nil, ErrThreadGovernanceUnavailable
	}
	initial, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	if initial == nil {
		return nil, identityport.ErrPolicyFactsChanged
	}
	q := identityport.AdminExecutionQuery{PermissionCode: code, Scope: identityport.PolicyResourceRef{Kind: "community.board", ID: initial.CategoryID}}
	authority, err := s.adminExecution.LoadAdminExecutionAuthority(ctx, q, resolve, false)
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	requestID := strconv.FormatInt(idgen.New(), 10)
	evaluatedAt := time.Now().UnixMilli()
	request, err := adminFlagRequest(initial, authority, operation, requestID, evaluatedAt)
	if err != nil {
		return nil, err
	}
	decision, err := (policy.ThreadAdminFlagPolicy{}).DecideThreadAdminFlag(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		return nil, ErrThreadGovernanceDenied
	}
	actor := authority.Principal.Actor
	var result *ThreadAdminFlagResult
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: actor.ID, ActorType: actor.Kind, ResourceType: "thread", ResourceID: id, OperationCode: code, EventFactory: func() (reliability.Event, error) {
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
		currentRequest, err := adminFlagRequest(current, currentAuthority, operation, requestID, evaluatedAt)
		if err != nil {
			return err
		}
		if err := identityport.CheckThreadAdminFlagDecision(request, decision, currentRequest); err != nil {
			return err
		}
		if !sameGovernanceThread(initial, current) {
			return identityport.ErrPolicyFactsChanged
		}
		before := adminFlagState(current)
		expectedVersion := current.FactsVersion
		now := time.Now().UTC()
		switch operation {
		case "pin":
			current.IsPinned = true
		case "unpin":
			current.IsPinned = false
		case "lock":
			current.IsLocked = true
		case "unlock":
			current.IsLocked = false
		}
		current.CurrentRevision++
		current.UpdatedAt = now
		if err := guarded.UpdateIfFactsVersion(tx, current, expectedVersion); err != nil {
			return err
		}
		written, err := s.repo.GetByID(tx, id)
		if err != nil {
			return err
		}
		if written == nil || written.FactsVersion <= expectedVersion || written.FactsVersion != current.FactsVersion || written.IsPinned != current.IsPinned || written.IsLocked != current.IsLocked || written.CurrentRevision != current.CurrentRevision {
			return identityport.ErrPolicyFactsChanged
		}
		if err := s.governance.CreateRevision(tx, &domain.ContentRevision{ID: strconv.FormatInt(idgen.New(), 10), ThreadID: id, Version: written.CurrentRevision, Title: written.Title, Content: written.Content, ContentFormat: written.ContentFormat, Tags: written.Tags, Action: operation, CreatedActorKind: actor.Kind, CreatedActorID: actor.ID, CreatedAt: now}); err != nil {
			return err
		}
		if err := s.governance.CreateModerationAction(tx, &domain.ModerationAction{ID: strconv.FormatInt(idgen.New(), 10), ThreadID: id, Action: operation, ActorKind: actor.Kind, ActorRefID: actor.ID, BeforeState: before, AfterState: adminFlagState(written), CreatedAt: now}); err != nil {
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
		result = &ThreadAdminFlagResult{ID: id, Pinned: final.IsPinned, Locked: final.IsLocked}
		return nil
	})
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	s.invalidateListCache(ctx)
	return result, nil
}
