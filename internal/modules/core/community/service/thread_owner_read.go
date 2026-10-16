package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var ErrThreadOwnerPrincipalUnavailable = errors.New("thread owner principal unavailable")

// GetThreadForPrincipal serves the authenticated /me detail route. resolve is
// a trusted ingress adapter that rechecks a real user session and its current
// strength. A bare viewer ID, parsed DTO or caller-supplied owner is no proof.
// Public reads retain their own predicate; a public revocation never switches
// this invocation to an owner exception.
func (s *ThreadService) GetThreadForPrincipal(ctx context.Context, id string, resolve func(context.Context) (port.PrincipalContext, error)) (*domain.Thread, error) {
	return s.getThreadForPrincipal(ctx, id, resolve, true)
}

// GetFeatureThreadForPrincipal keeps the same Policy and raw-fact checks as
// /me while preventing Feature projections from incrementing view counters.
// Nil resolve is the explicit anonymous public branch, never an owner fallback.
func (s *ThreadService) GetFeatureThreadForPrincipal(ctx context.Context, id string, resolve func(context.Context) (port.PrincipalContext, error)) (*domain.Thread, error) {
	if resolve == nil {
		return s.readPublicThread(ctx, id, false)
	}
	initial, err := resolveThreadOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	if initial.Actor.Kind != "user" || initial.Audience != "user" {
		return nil, ErrThreadOwnerPrincipalUnavailable
	}
	bound := func(ctx context.Context) (port.PrincipalContext, error) {
		current, err := resolveThreadOwnerPrincipal(ctx, resolve)
		if err != nil {
			return port.PrincipalContext{}, err
		}
		if !reflect.DeepEqual(initial, current) {
			return port.PrincipalContext{}, port.ErrPolicyFactsChanged
		}
		return current, nil
	}
	thread, err := s.getThreadForPrincipal(ctx, id, bound, false)
	if err != nil {
		return nil, err
	}
	if _, err := bound(ctx); err != nil {
		return nil, err
	}
	return thread, nil
}

func (s *ThreadService) getThreadForPrincipal(ctx context.Context, id string, resolve func(context.Context) (port.PrincipalContext, error), incrementView bool) (*domain.Thread, error) {
	principal, err := resolveThreadOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	initial, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read viewer thread: %w", err)
	}
	facts, err := publicThreadFacts(initial, id)
	if err != nil {
		return nil, err
	}
	if facts.PublicationStatus == "published" && facts.ModerationStatus == "clear" && facts.DeletionStatus == "active" {
		return s.readPublicThread(ctx, id, incrementView)
	}
	snapshots, ok := s.repo.(repository.OwnerThreadSnapshotRepository)
	if !ok {
		return nil, port.ErrPolicyDecisionMismatch
	}
	engine := s.ownerPolicy
	if engine == nil {
		engine = policy.ThreadOwnerReadPolicy{}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempt > 0 {
			principal, err = resolveThreadOwnerPrincipal(ctx, resolve)
			if err != nil {
				return nil, err
			}
			initial, err = s.repo.GetByID(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("reload owner thread: %w", err)
			}
			facts, err = publicThreadFacts(initial, id)
			if err != nil {
				return nil, err
			}
		}
		request := port.ThreadOwnerReadRequest{
			Contract: port.ThreadOwnerReadContract, RequestID: strconv.FormatInt(idgen.New(), 10),
			Policy: port.ThreadOwnerReadPolicyVersion, Principal: principal, Facts: facts,
		}
		decision, err := engine.DecideThreadOwnerRead(request)
		if err != nil {
			return nil, fmt.Errorf("owner read policy: %w", err)
		}
		if err = port.CheckThreadOwnerReadDecision(request, decision, facts, principal); err != nil {
			if errors.Is(err, port.ErrPolicyOwnerReadDenied) {
				return nil, repository.ErrThreadNotFound
			}
			return nil, err
		}
		currentPrincipal, err := resolveThreadOwnerPrincipal(ctx, resolve)
		if err != nil {
			return nil, err
		}
		if err = port.CheckThreadOwnerReadDecision(request, decision, facts, currentPrincipal); errors.Is(err, port.ErrPolicyFactsChanged) {
			continue
		} else if err != nil {
			return nil, err
		}
		// The owner/version-conditional snapshot returns the same checked row
		// with its content, including recoverable trash. No view is incremented.
		current, err := snapshots.RecheckOwnerThread(ctx, id, principal.Actor.ID, initial.FactsVersion)
		if errors.Is(err, repository.ErrThreadNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("recheck owner thread: %w", err)
		}
		currentFacts, err := publicThreadFacts(current, id)
		if err != nil {
			return nil, err
		}
		currentPrincipal, err = resolveThreadOwnerPrincipal(ctx, resolve)
		if err != nil {
			return nil, err
		}
		if err = port.CheckThreadOwnerReadDecision(request, decision, currentFacts, currentPrincipal); errors.Is(err, port.ErrPolicyFactsChanged) {
			continue
		} else if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return current, nil
	}
	return nil, port.ErrPolicyFactsChanged
}

func resolveThreadOwnerPrincipal(ctx context.Context, resolve func(context.Context) (port.PrincipalContext, error)) (port.PrincipalContext, error) {
	if err := ctx.Err(); err != nil {
		return port.PrincipalContext{}, err
	}
	if resolve == nil {
		return port.PrincipalContext{}, ErrThreadOwnerPrincipalUnavailable
	}
	principal, err := resolve(ctx)
	if err != nil {
		if errors.Is(err, port.ErrPolicyFactsChanged) {
			return port.PrincipalContext{}, err
		}
		return port.PrincipalContext{}, errors.Join(ErrThreadOwnerPrincipalUnavailable, err)
	}
	if principal.Validate() != nil {
		return port.PrincipalContext{}, ErrThreadOwnerPrincipalUnavailable
	}
	return principal, nil
}
