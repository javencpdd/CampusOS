package service

import (
	"context"
	"reflect"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type featureGovernanceKey struct{}
type featureGovernanceBinding struct {
	part   communityport.ThreadGovernanceParticipant
	thread *domain.Thread
	facts  any
}

func detachedGovernanceThread(t *domain.Thread) *domain.Thread {
	if t == nil {
		return nil
	}
	v := *t
	v.Tags = append([]string(nil), t.Tags...)
	if t.ModerationAt != nil {
		at := *t.ModerationAt
		v.ModerationAt = &at
	}
	if t.DeletedAt != nil {
		at := *t.DeletedAt
		v.DeletedAt = &at
	}
	return &v
}

// The Feature owns its schema. Core keeps the existing named command, current
// authority and resource policy, and runs its participant in that transaction.
func (s *ThreadService) ExecuteThreadGovernanceCommand(ctx context.Context, id, operation, reason string, resolve identityport.SessionPrincipalResolver, part communityport.ThreadGovernanceParticipant) error {
	if s == nil || nilGovernancePort(part) {
		return ErrThreadGovernanceUnavailable
	}
	if !domain.IsKnownThreadType(part.ThreadType()) || part.ThreadType() == domain.ThreadTypeDiscussion {
		return ErrThreadGovernanceDenied
	}
	if operation != "take_down" && operation != "trash" && operation != "direct_restore" {
		return identityport.ErrResourcePolicyUnknownAction
	}
	initial, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return threadGovernanceError(err)
	}
	if initial == nil || initial.ThreadType != part.ThreadType() {
		return ErrThreadGovernanceDenied
	}
	facts, err := part.LoadGovernanceFacts(ctx, detachedGovernanceThread(initial), false)
	if err != nil {
		return threadGovernanceError(err)
	}
	if nilGovernancePort(facts) {
		return ErrThreadGovernanceUnavailable
	}
	ctx = context.WithValue(ctx, featureGovernanceKey{}, featureGovernanceBinding{part: part, thread: initial, facts: facts})
	if operation == "take_down" {
		_, err = s.TakeDownForPrincipal(ctx, id, reason, resolve)
	} else {
		_, err = s.AdminLifecycleThread(ctx, id, operation, reason, resolve)
	}
	return err
}

func checkFeatureGovernanceInitial(ctx context.Context, thread *domain.Thread) error {
	bound, ok := ctx.Value(featureGovernanceKey{}).(featureGovernanceBinding)
	if !ok {
		return nil
	}
	if thread == nil || thread.ThreadType != bound.part.ThreadType() || !sameGovernanceThread(bound.thread, thread) {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}
func lockFeatureGovernance(ctx context.Context, thread *domain.Thread) error {
	bound, ok := ctx.Value(featureGovernanceKey{}).(featureGovernanceBinding)
	if !ok {
		return nil
	}
	facts, err := bound.part.LoadGovernanceFacts(ctx, detachedGovernanceThread(thread), true)
	if err != nil {
		return err
	}
	if nilGovernancePort(facts) || !reflect.DeepEqual(bound.facts, facts) {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}
func applyFeatureGovernance(ctx context.Context, thread *domain.Thread, actor identityport.PrincipalRef) error {
	bound, ok := ctx.Value(featureGovernanceKey{}).(featureGovernanceBinding)
	if !ok {
		return nil
	}
	return bound.part.ApplyGovernanceCommand(ctx, detachedGovernanceThread(thread), actor)
}
func checkFeatureGovernanceFinal(ctx context.Context, thread *domain.Thread, actor identityport.PrincipalRef) error {
	bound, ok := ctx.Value(featureGovernanceKey{}).(featureGovernanceBinding)
	if !ok {
		return nil
	}
	return bound.part.CheckGovernanceResult(ctx, detachedGovernanceThread(thread), actor)
}
