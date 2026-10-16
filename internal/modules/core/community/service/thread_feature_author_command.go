package service

import (
	"context"
	"reflect"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/idgen"
)

func (s *ThreadService) LoadThreadAuthorEditProof(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver) (*domain.Thread, error) {
	if s.reliable == nil || s.governance == nil {
		return nil, ErrThreadAuthorWriteUnavailable
	}
	initial, err := resolveThreadOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	if initial.Actor.Kind != "user" || initial.Audience != "user" {
		return nil, ErrThreadAuthorWriteDenied
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		current, err := resolveThreadOwnerPrincipal(ctx, resolve)
		if err != nil {
			return current, err
		}
		if !reflect.DeepEqual(initial, current) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	initialThread, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	check, err := s.featureAuthorDecision(initialThread, initial, "edit", strconv.FormatInt(idgen.New(), 10))
	if err != nil {
		return nil, err
	}
	thread, err := s.GetFeatureThreadForPrincipal(ctx, id, bound)
	if err != nil {
		return nil, err
	}
	if err := check(thread, initial); err != nil {
		return nil, err
	}
	if _, err := bound(ctx); err != nil {
		return nil, err
	}
	return thread, nil
}

// ExecuteThreadAuthorCommand coordinates current Core authority and Feature
// facts without learning a Feature schema or accepting client facts.
func (s *ThreadService) ExecuteThreadAuthorCommand(ctx context.Context, id, operation, code, event string, resolve identityport.SessionPrincipalResolver, part communityport.ThreadAuthorParticipant) error {
	if s.reliable == nil || s.governance == nil || part == nil || reflect.ValueOf(part).Kind() == reflect.Pointer && reflect.ValueOf(part).IsNil() {
		return ErrThreadAuthorWriteUnavailable
	}
	guard, ok := s.repo.(repository.FactsGuardedThreadRepository)
	if !ok || !domain.IsKnownThreadType(part.ThreadType()) || part.ThreadType() == domain.ThreadTypeDiscussion || !strings.HasPrefix(code, "feature.") || event == "" {
		return ErrThreadAuthorWriteInvalid
	}
	p, err := resolveThreadOwnerPrincipal(ctx, resolve)
	if err != nil {
		return err
	}
	initial, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if initial == nil || initial.ThreadType != part.ThreadType() {
		return ErrThreadAuthorWriteDenied
	}
	requestID := strconv.FormatInt(idgen.New(), 10)
	// Each registered operation uses its actual named rule. Deleted resources
	// never acquire an invented active edit decision.
	check, err := s.featureAuthorDecision(initial, p, operation, requestID)
	if err != nil {
		return err
	}
	facts, err := part.LoadAuthorFacts(ctx, initial, false)
	if err != nil {
		return err
	}
	if missingFeatureAuthorFacts(facts) {
		return identityport.ErrPolicyDecisionMismatch
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
	err = s.reliable.Execute(ctx, reliability.Command{Code: code, ActorID: p.Actor.ID, ActorType: "user", ResourceType: "thread", ResourceID: id, OperationCode: code, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent(event, "thread", id, part.AuthorEventPayload())
	}}, func(tx context.Context) error {
		currentPrincipal, err := bound(tx)
		if err != nil {
			return err
		}
		current, err := guard.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		if current == nil || current.ThreadType != part.ThreadType() {
			return identityport.ErrPolicyFactsChanged
		}
		if err := check(current, currentPrincipal); err != nil {
			return err
		}
		actual, err := part.LoadAuthorFacts(tx, current, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, facts) {
			return identityport.ErrPolicyFactsChanged
		}
		commandCtx := context.WithValue(tx, authorWriteFactsKey{}, authorWriteFacts{ID: id, Version: current.FactsVersion})
		if err := part.ApplyAuthorCommand(commandCtx, current); err != nil {
			return err
		}
		final, err := guard.LockThreadForWrite(tx, id)
		if err != nil {
			return err
		}
		if final == nil || final.AuthorID != p.Actor.ID || final.CategoryID != initial.CategoryID || final.ThreadType != initial.ThreadType {
			return identityport.ErrPolicyFactsChanged
		}
		if operation == "trash" {
			if final.DeletionStatus != "trashed" {
				return identityport.ErrPolicyFactsChanged
			}
		} else if final.DeletionStatus != "active" {
			return identityport.ErrPolicyFactsChanged
		}
		if err := part.CheckAuthorResult(tx, final); err != nil {
			return err
		}
		_, err = bound(tx)
		return err
	})
	if err != nil {
		return err
	}
	s.invalidateListCache(ctx)
	return nil
}

func (s *ThreadService) featureAuthorDecision(initial *domain.Thread, p identityport.PrincipalContext, op, id string) (func(*domain.Thread, identityport.PrincipalContext) error, error) {
	if op == "edit" {
		request, err := authorEditRequest(initial, p, id)
		if err != nil {
			return nil, err
		}
		decision, err := (policy.ResourcePolicy{}).DecideResource(request)
		if err != nil {
			return nil, err
		}
		if decision.Effect != "allow" {
			return nil, ErrThreadAuthorWriteDenied
		}
		return func(current *domain.Thread, p identityport.PrincipalContext) error {
			fresh, err := authorEditRequest(current, p, id)
			if err != nil {
				return err
			}
			return policy.CheckResourceDecision(request, decision, fresh)
		}, nil
	}
	if op != "trash" && op != "restore" && op != "submit_review" {
		return nil, ErrThreadAuthorWriteInvalid
	}
	facts, err := publicThreadFacts(initial, initial.ID)
	if err != nil {
		return nil, err
	}
	request := identityport.ThreadOwnerWriteRequest{Contract: identityport.ThreadOwnerWriteContract, RequestID: id, PolicyVersion: identityport.ThreadOwnerWritePolicyVersion, Principal: p, Facts: facts, Operation: op}
	decision, err := (policy.ThreadOwnerWritePolicy{}).DecideThreadOwnerWrite(request)
	if err != nil {
		return nil, err
	}
	if decision.Effect != "allow" {
		return nil, ErrThreadAuthorWriteDenied
	}
	return func(current *domain.Thread, p identityport.PrincipalContext) error {
		facts, err := publicThreadFacts(current, current.ID)
		if err != nil {
			return err
		}
		fresh := request
		fresh.Facts, fresh.Principal = facts, p
		return policy.CheckThreadOwnerWriteDecision(request, decision, fresh)
	}, nil
}

func missingFeatureAuthorFacts(facts any) bool {
	if facts == nil {
		return true
	}
	v := reflect.ValueOf(facts)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return v.IsNil()
	}
	return false
}
