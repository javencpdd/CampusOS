package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/idgen"
)

// publicThreadFacts uses only the row that supplied the returned content. A
// missing explicit state or repository version is an internal invalid fact,
// never an invitation to infer public visibility from the legacy status.
func publicThreadFacts(thread *domain.Thread, id string) (port.ThreadPublicReadFacts, error) {
	if thread == nil || thread.ID != id || thread.FactsVersion <= 0 {
		return port.ThreadPublicReadFacts{}, port.ErrPolicyDecisionMismatch
	}
	facts := port.ThreadPublicReadFacts{
		Resource:          port.PolicyResourceRef{Kind: "community.thread", ID: thread.ID},
		Owner:             port.PrincipalRef{Kind: "user", ID: thread.AuthorID},
		Board:             port.PolicyResourceRef{Kind: "community.board", ID: thread.CategoryID},
		FactsVersion:      strconv.FormatInt(thread.FactsVersion, 10),
		PublicationStatus: string(thread.PublicationStatus),
		ModerationStatus:  string(thread.ModerationStatus),
		DeletionStatus:    string(thread.DeletionStatus),
	}
	if facts.Validate() != nil {
		return port.ThreadPublicReadFacts{}, port.ErrPolicyDecisionMismatch
	}
	return facts, nil
}

// readPublicThread is the canonical public predicate for anonymous HTTP and
// internal public reads. Integration/plugin ingress still owns its Scope,
// Grant/Consent and lifecycle checks; this predicate cannot satisfy them.
func (s *ThreadService) readPublicThread(ctx context.Context, id string, countView bool) (*domain.Thread, error) {
	snapshots, ok := s.repo.(repository.PublicThreadSnapshotRepository)
	if !ok {
		return nil, port.ErrPolicyDecisionMismatch
	}
	engine := s.publicPolicy
	if engine == nil {
		engine = policy.ThreadPublicReadPolicy{}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		thread, err := s.repo.GetByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("read public thread: %w", err)
		}
		facts, err := publicThreadFacts(thread, id)
		if err != nil {
			return nil, err
		}
		request := port.ThreadPublicReadRequest{
			Contract:  port.ThreadPublicReadContract,
			RequestID: strconv.FormatInt(idgen.New(), 10),
			Policy:    port.ThreadPublicReadPolicyVersion,
			Principal: port.PrincipalContext{Contract: port.PrincipalContract,
				Actor: port.PrincipalRef{Kind: "anonymous"}, Audience: "public", AuthenticationStrength: "none"},
			Facts: facts,
		}
		decision, err := engine.DecideThreadPublicRead(request)
		if err != nil {
			return nil, fmt.Errorf("public policy: %w", err)
		}
		if err = port.CheckThreadPublicReadDecision(request, decision, facts); err != nil {
			if errors.Is(err, port.ErrPolicyResourceNotPublic) {
				return nil, repository.ErrThreadNotFound
			}
			return nil, err
		}
		// The version-conditional SELECT/UPDATE (or Memory lock) is the
		// linearization point. It returns content from the same checked row;
		// a view increment cannot fetch a newer, now-private body.
		current, err := snapshots.RecheckPublicThread(ctx, id, thread.FactsVersion, countView)
		if errors.Is(err, repository.ErrThreadNotFound) {
			continue // Reload, make new facts and a new decision, never reuse allow.
		}
		if err != nil {
			return nil, fmt.Errorf("recheck public thread: %w", err)
		}
		currentFacts, err := publicThreadFacts(current, id)
		if err != nil {
			return nil, err
		}
		if err = port.CheckThreadPublicReadDecision(request, decision, currentFacts); err != nil {
			return nil, err
		}
		if countView {
			s.invalidateListCache(ctx)
		}
		return current, nil
	}
	return nil, port.ErrPolicyFactsChanged
}
