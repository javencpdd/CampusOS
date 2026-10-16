package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

type ThreadUserFlagResult = communityport.ThreadFlagResult

func (s *ThreadService) SetUserScopedExecutionAuthority(reader identityport.UserScopedExecutionAuthorityReader) {
	s.userExecution = reader
}

func userFlagCode(operation string) (string, error) {
	switch operation {
	case "pin", "unpin":
		return "community.thread.pin", nil
	case "lock", "unlock":
		return "community.thread.lock", nil
	default:
		return "", identityport.ErrResourcePolicyUnknownAction
	}
}

func userFlagRequest(thread *domain.Thread, authority identityport.UserScopedExecutionAuthority, operation, requestID string, evaluatedAt int64) (identityport.ThreadUserFlagRequest, error) {
	if thread == nil {
		return identityport.ThreadUserFlagRequest{}, identityport.ErrPolicyFactsChanged
	}
	facts, err := publicThreadFacts(thread, thread.ID)
	if err != nil {
		return identityport.ThreadUserFlagRequest{}, err
	}
	return identityport.ThreadUserFlagRequest{Contract: identityport.ThreadUserFlagContract, PolicyVersion: identityport.ThreadUserFlagPolicyVersion, RequestID: requestID, Operation: operation, Principal: authority.Principal, Thread: facts, Pinned: thread.IsPinned, Locked: thread.IsLocked, ExecutionQuery: authority.Query, ExecutionAllowed: authority.Allowed, ExecutionExpiresAtMS: authority.ExpiresAtMS, EvaluatedAtMS: evaluatedAt}, nil
}

func userFlagState(thread *domain.Thread) string {
	return fmt.Sprintf("%s,pinned:%t,locked:%t", domain.ContentStateSummary(thread), thread.IsPinned, thread.IsLocked)
}

// UserSetThreadFlag binds the current User, actual scoped assignment and
// resource policy. Module config and mandatory audit participate in the same transaction.
func (s *ThreadService) UserSetThreadFlag(ctx context.Context, id, operation string, resolve identityport.SessionPrincipalResolver, participant communityport.ThreadFlagGovernanceParticipant) (*ThreadUserFlagResult, error) {
	code, err := userFlagCode(operation)
	if err != nil {
		return nil, err
	}
	if nilGovernancePort(participant) || s == nil || s.reliable == nil || nilGovernancePort(s.userExecution) || nilGovernancePort(s.governance) {
		return nil, ErrThreadGovernanceUnavailable
	}
	guarded, ok := s.repo.(repository.FactsGuardedThreadRepository)
	if !ok || nilGovernancePort(guarded) {
		return nil, ErrThreadGovernanceUnavailable
	}
	if err := participant.CheckThreadFlagGovernance(ctx, operation); err != nil {
		return nil, err
	}
	if snapshot, ok := participant.(transaction.Snapshotter); ok {
		s.reliable.RegisterMemorySnapshotters(snapshot)
	}
	initial, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	if initial == nil {
		return nil, identityport.ErrPolicyFactsChanged
	}
	q := identityport.UserScopedExecutionQuery{PermissionCode: code, Scope: identityport.PolicyResourceRef{Kind: "community.board", ID: initial.CategoryID}}
	authority, err := s.userExecution.LoadUserScopedExecutionAuthority(ctx, q, resolve, false)
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	requestID := strconv.FormatInt(idgen.New(), 10)
	evaluatedAt := time.Now().UnixMilli()
	request, err := userFlagRequest(initial, authority, operation, requestID, evaluatedAt)
	if err != nil {
		return nil, err
	}
	decision, err := (policy.ThreadUserFlagPolicy{}).DecideThreadUserFlag(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		return nil, ErrThreadGovernanceDenied
	}
	actor := authority.Principal.Actor
	var result *ThreadUserFlagResult
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: actor.ID, ActorType: actor.Kind, ResourceType: "thread", ResourceID: id, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent(eventbus.EventThreadUpdated, "thread", id, result)
	}}, func(tx context.Context) error {
		currentAuthority, err := s.userExecution.LoadUserScopedExecutionAuthority(tx, q, resolve, true)
		if err != nil {
			return err
		}
		if err := identityport.CheckUserScopedExecutionAuthority(authority, currentAuthority); err != nil {
			return err
		}
		current, err := guarded.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		currentRequest, err := userFlagRequest(current, currentAuthority, operation, requestID, evaluatedAt)
		if err != nil {
			return err
		}
		if err := identityport.CheckThreadUserFlagDecision(request, decision, currentRequest); err != nil {
			return err
		}
		if !sameGovernanceThread(initial, current) {
			return identityport.ErrPolicyFactsChanged
		}
		if err := participant.CheckThreadFlagGovernance(tx, operation); err != nil {
			return err
		}
		beforeThread := detachedGovernanceThread(current)
		before := userFlagState(current)
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
		if err := s.governance.CreateRevision(tx, &domain.ContentRevision{ID: strconv.FormatInt(idgen.New(), 10), ThreadID: id, Version: written.CurrentRevision, Title: written.Title, Content: written.Content, ContentFormat: written.ContentFormat, Tags: written.Tags, Action: operation, CreatedBy: actor.ID, CreatedActorKind: actor.Kind, CreatedActorID: actor.ID, CreatedAt: now}); err != nil {
			return err
		}
		if err := s.governance.CreateModerationAction(tx, &domain.ModerationAction{ID: strconv.FormatInt(idgen.New(), 10), ThreadID: id, Action: operation, ActorID: actor.ID, ActorKind: actor.Kind, ActorRefID: actor.ID, BeforeState: before, AfterState: userFlagState(written), CreatedAt: now}); err != nil {
			return err
		}
		if err := participant.RecordThreadFlagGovernance(tx, authority.Principal, operation, detachedGovernanceThread(beforeThread), detachedGovernanceThread(written)); err != nil {
			return err
		}
		if err := participant.CheckThreadFlagGovernance(tx, operation); err != nil {
			return err
		}
		finalAuthority, err := s.userExecution.LoadUserScopedExecutionAuthority(tx, q, resolve, true)
		if err != nil {
			return err
		}
		if err := identityport.CheckUserScopedExecutionAuthority(currentAuthority, finalAuthority); err != nil {
			return err
		}
		final, err := guarded.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		if !sameGovernanceThread(written, final) {
			return identityport.ErrPolicyFactsChanged
		}
		result = &ThreadUserFlagResult{ID: id, Pinned: final.IsPinned, Locked: final.IsLocked}
		return nil
	})
	if err != nil {
		return nil, threadGovernanceError(err)
	}
	s.invalidateListCache(ctx)
	return result, nil
}
