package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/idgen"
)

// ListThreadsForPrincipal returns the current user's active threads, including
// private/draft/moderated states. Each repository read enforces owner and raw
// known states before forming a single page/count snapshot. Empty pages also
// require current identity and a final count recheck. No view is incremented.
func (s *ThreadService) ListThreadsForPrincipal(ctx context.Context, resolve func(context.Context) (port.PrincipalContext, error), filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	principal, err := resolveAuthorListPrincipal(ctx, resolve)
	if err != nil {
		return nil, 0, err
	}
	ownerID := principal.Actor.ID
	filter, err = s.prepareThreadListFilter(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	filter.AuthorID = ownerID
	filter.Status, filter.PublicationStatus, filter.ModerationStatus = "", "", ""
	filter.DeletionStatus = string(domain.DeletionStatusActive)
	filter.IncludeTrashed = false
	if int64(filter.Page-1) > math.MaxInt64/int64(filter.PageSize) {
		return nil, 0, repository.ErrThreadPaginationOverflow
	}
	snapshots, ok := s.repo.(repository.OwnerThreadListSnapshotRepository)
	if !ok {
		return nil, 0, port.ErrPolicyDecisionMismatch
	}
	offset := int64(filter.Page-1) * int64(filter.PageSize)
	engine := s.ownerPolicy
	if engine == nil {
		engine = policy.ThreadOwnerReadPolicy{}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if attempt > 0 {
			principal, err = resolveAuthorListPrincipal(ctx, resolve)
			if err != nil {
				return nil, 0, err
			}
			if principal.Actor.ID != ownerID {
				return nil, 0, port.ErrUserSessionPrincipalUnavailable
			}
		}
		initial, total, err := snapshots.ListOwnerThreadSnapshot(ctx, ownerID, filter)
		if err != nil {
			return nil, 0, fmt.Errorf("list owner threads: %w", err)
		}
		if err := validatePublicThreadPage(initial, total, offset, filter.PageSize); err != nil {
			return nil, 0, err
		}
		requests := make([]port.ThreadOwnerReadRequest, len(initial))
		decisions := make([]port.ThreadOwnerReadDecision, len(initial))
		for i, thread := range initial {
			facts, err := authorListThreadFacts(thread, ownerID)
			if err != nil {
				return nil, 0, err
			}
			requests[i] = port.ThreadOwnerReadRequest{Contract: port.ThreadOwnerReadContract,
				RequestID: strconv.FormatInt(idgen.New(), 10), Policy: port.ThreadOwnerReadPolicyVersion,
				Principal: principal, Facts: facts}
			decisions[i], err = engine.DecideThreadOwnerRead(requests[i])
			if err != nil {
				return nil, 0, fmt.Errorf("owner list policy: %w", err)
			}
			if err = port.CheckThreadOwnerReadDecision(requests[i], decisions[i], facts, principal); err != nil {
				return nil, 0, err
			}
		}
		before, err := resolveAuthorListPrincipal(ctx, resolve)
		if err != nil {
			return nil, 0, err
		}
		if before.Actor.ID != ownerID {
			return nil, 0, port.ErrUserSessionPrincipalUnavailable
		}
		if !reflect.DeepEqual(principal, before) {
			continue
		}
		current, currentTotal, err := snapshots.ListOwnerThreadSnapshot(ctx, ownerID, filter)
		if err != nil {
			return nil, 0, fmt.Errorf("recheck owner thread page: %w", err)
		}
		after, err := resolveAuthorListPrincipal(ctx, resolve)
		if err != nil {
			return nil, 0, err
		}
		if after.Actor.ID != ownerID {
			return nil, 0, port.ErrUserSessionPrincipalUnavailable
		}
		if !reflect.DeepEqual(principal, after) {
			continue
		}
		if err := validatePublicThreadPage(current, currentTotal, offset, filter.PageSize); err != nil {
			return nil, 0, err
		}
		if currentTotal != total || len(current) != len(initial) {
			continue
		}
		changed := false
		for i, thread := range current {
			if thread.ID != requests[i].Facts.Resource.ID {
				changed = true
				break
			}
			facts, err := authorListThreadFacts(thread, ownerID)
			if err != nil {
				return nil, 0, err
			}
			if err = port.CheckThreadOwnerReadDecision(requests[i], decisions[i], facts, after); errors.Is(err, port.ErrPolicyFactsChanged) {
				changed = true
				break
			} else if err != nil {
				return nil, 0, err
			}
		}
		if changed {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		return current, currentTotal, nil
	}
	return nil, 0, port.ErrPolicyFactsChanged
}

func resolveAuthorListPrincipal(ctx context.Context, resolve func(context.Context) (port.PrincipalContext, error)) (port.PrincipalContext, error) {
	principal, err := resolveThreadOwnerPrincipal(ctx, resolve)
	if errors.Is(err, port.ErrPolicyFactsChanged) {
		return port.PrincipalContext{}, port.ErrPolicyFactsChanged
	}
	if err != nil {
		return port.PrincipalContext{}, errors.Join(port.ErrUserSessionPrincipalUnavailable, err)
	}
	if principal.Actor.Kind != "user" || principal.Audience != "user" {
		return port.PrincipalContext{}, port.ErrUserSessionPrincipalUnavailable
	}
	return principal, nil
}

func authorListThreadFacts(thread *domain.Thread, ownerID string) (port.ThreadOwnerReadFacts, error) {
	if thread == nil {
		return port.ThreadOwnerReadFacts{}, port.ErrPolicyDecisionMismatch
	}
	facts, err := publicThreadFacts(thread, thread.ID)
	if err != nil {
		return facts, err
	}
	if facts.Owner.ID != ownerID || facts.DeletionStatus != "active" {
		return facts, port.ErrPolicyDecisionMismatch
	}
	return facts, nil
}
