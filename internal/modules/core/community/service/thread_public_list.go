package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/idgen"
)

// ListPublicThreads authorizes an entire page before returning its final
// snapshot. Both repository reads apply the public predicate before pagination
// and return rows plus total from a single snapshot. A changed member, order,
// total or fact requires a new page and new decisions, never a sparse page.
func (s *ThreadService) ListPublicThreads(ctx context.Context, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	filter, err := s.prepareThreadListFilter(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	filter.Status = "" // Legacy status is neither a public fact nor a second predicate.
	filter.PublicationStatus = string(domain.PublicationStatusPublished)
	filter.ModerationStatus = string(domain.ModerationStatusClear)
	filter.DeletionStatus = string(domain.DeletionStatusActive)
	filter.IncludeTrashed = false
	if int64(filter.Page-1) > math.MaxInt64/int64(filter.PageSize) {
		return nil, 0, repository.ErrThreadPaginationOverflow
	}
	offset := int64(filter.Page-1) * int64(filter.PageSize)
	engine := s.publicPolicy
	if engine == nil {
		engine = policy.ThreadPublicReadPolicy{}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		initial, total, err := s.repo.List(ctx, filter)
		if err != nil {
			return nil, 0, fmt.Errorf("list public threads: %w", err)
		}
		if err = validatePublicThreadPage(initial, total, offset, filter.PageSize); err != nil {
			return nil, 0, err
		}
		requests := make([]port.ThreadPublicReadRequest, len(initial))
		decisions := make([]port.ThreadPublicReadDecision, len(initial))
		for i, thread := range initial {
			facts, err := publicThreadFacts(thread, thread.ID)
			if err != nil {
				return nil, 0, err
			}
			requests[i] = port.ThreadPublicReadRequest{
				Contract: port.ThreadPublicReadContract, RequestID: strconv.FormatInt(idgen.New(), 10),
				Policy: port.ThreadPublicReadPolicyVersion,
				Principal: port.PrincipalContext{Contract: port.PrincipalContract,
					Actor: port.PrincipalRef{Kind: "anonymous"}, Audience: "public", AuthenticationStrength: "none"},
				Facts: facts,
			}
			decisions[i], err = engine.DecideThreadPublicRead(requests[i])
			if err != nil {
				return nil, 0, fmt.Errorf("public list policy: %w", err)
			}
			if err = port.CheckThreadPublicReadDecision(requests[i], decisions[i], facts); err != nil {
				return nil, 0, err
			}
		}
		// This read is the linearization point, including when the page is
		// empty. Looking up only initial IDs would miss membership/total races.
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		current, currentTotal, err := s.repo.List(ctx, filter)
		if err != nil {
			return nil, 0, fmt.Errorf("recheck public thread page: %w", err)
		}
		if err = validatePublicThreadPage(current, currentTotal, offset, filter.PageSize); err != nil {
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
			facts, err := publicThreadFacts(thread, thread.ID)
			if err != nil {
				return nil, 0, err
			}
			if err = port.CheckThreadPublicReadDecision(requests[i], decisions[i], facts); errors.Is(err, port.ErrPolicyFactsChanged) {
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

func validatePublicThreadPage(threads []*domain.Thread, total, offset int64, pageSize int) error {
	if total < 0 {
		return port.ErrPolicyDecisionMismatch
	}
	expected := int64(0)
	if total > offset {
		expected = min(total-offset, int64(pageSize))
	}
	if int64(len(threads)) != expected {
		return port.ErrPolicyDecisionMismatch
	}
	seen := make(map[string]struct{}, len(threads))
	for _, thread := range threads {
		if thread == nil {
			return port.ErrPolicyDecisionMismatch
		}
		if _, duplicate := seen[thread.ID]; duplicate {
			return port.ErrPolicyDecisionMismatch
		}
		seen[thread.ID] = struct{}{}
	}
	return nil
}
