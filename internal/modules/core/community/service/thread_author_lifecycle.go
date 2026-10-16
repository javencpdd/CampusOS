package service

import (
	"context"
	"reflect"
	"strconv"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/eventbus"
	"github.com/campusos/CampusOS/pkg/idgen"
)

func (s *ThreadService) AuthorLifecycleForPrincipal(ctx context.Context, id, operation string, resolve identityport.SessionPrincipalResolver) (*domain.Thread, error) {
	if s.reliable == nil || s.governance == nil {
		return nil, ErrThreadAuthorWriteUnavailable
	}
	guarded, ok := s.repo.(repository.FactsGuardedThreadRepository)
	if !ok {
		return nil, ErrThreadAuthorWriteUnavailable
	}
	p, err := resolveThreadOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	initial, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	facts, err := publicThreadFacts(initial, id)
	if err != nil {
		return nil, err
	}
	request := identityport.ThreadOwnerWriteRequest{Contract: identityport.ThreadOwnerWriteContract, RequestID: strconv.FormatInt(idgen.New(), 10), PolicyVersion: identityport.ThreadOwnerWritePolicyVersion, Principal: p, Facts: facts, Operation: operation}
	decision, err := (policy.ThreadOwnerWritePolicy{}).DecideThreadOwnerWrite(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		return nil, ErrThreadAuthorWriteDenied
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		current, err := resolveThreadOwnerPrincipal(ctx, resolve)
		if err != nil {
			return current, err
		}
		if !reflect.DeepEqual(current, p) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	ctx = identityport.WithSessionPrincipalResolver(ctx, bound)
	var result *domain.Thread
	code, event := "community.thread.author_trash", eventbus.EventThreadDeleted
	if operation == "restore" {
		code, event = "community.thread.author_restore", eventbus.EventThreadUpdated
	}
	if operation == "submit_review" {
		code, event = "community.thread.author_submit", eventbus.EventThreadUpdated
	}
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: p.Actor.ID, ActorType: "user", ResourceType: "thread", ResourceID: id, OperationCode: code, EventFactory: func() (reliability.Event, error) { return reliability.NewEvent(event, "thread", id, result) }}, func(tx context.Context) error {
		currentPrincipal, err := bound(tx)
		if err != nil {
			return err
		}
		current, err := guarded.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		currentFacts, err := publicThreadFacts(current, id)
		if err != nil {
			return err
		}
		currentRequest := request
		currentRequest.Principal, currentRequest.Facts = currentPrincipal, currentFacts
		if err := policy.CheckThreadOwnerWriteDecision(request, decision, currentRequest); err != nil {
			return err
		}
		commandCtx := context.WithValue(tx, authorWriteFactsKey{}, authorWriteFacts{ID: id, Version: current.FactsVersion})
		switch operation {
		case "trash":
			if err := s.TrashThread(commandCtx, id, p.Actor.ID, "author_delete", "author moved content to trash"); err != nil {
				return err
			}
			result, err = s.repo.GetByID(tx, id)
		case "restore":
			result, err = s.RestoreFromTrash(commandCtx, id, p.Actor.ID)
		case "submit_review":
			result, err = s.SubmitForReview(commandCtx, id, p.Actor.ID)
		}
		if err != nil {
			return err
		}
		if _, err := bound(tx); err != nil {
			return err
		}
		final, err := guarded.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		if result == nil || final.FactsVersion != result.FactsVersion || final.AuthorID != p.Actor.ID || final.CategoryID != initial.CategoryID {
			return identityport.ErrPolicyFactsChanged
		}
		if operation == "trash" && final.DeletionStatus != "trashed" {
			return identityport.ErrPolicyFactsChanged
		}
		if operation != "trash" && final.DeletionStatus != "active" {
			return identityport.ErrPolicyFactsChanged
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateListCache(ctx)
	return result, nil
}
