package mutualaid

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

type authorDetailLocker interface {
	LockDetailForAuthorCommand(context.Context, string) (*Detail, error)
}
type authorWriteParticipant struct {
	s      *Service
	apply  func(context.Context, string) (*Result, error)
	result *Result
}

func (p *authorWriteParticipant) ThreadType() domain.ThreadType { return domain.ThreadTypeMutualAid }
func (p *authorWriteParticipant) LoadAuthorFacts(ctx context.Context, t *domain.Thread, lock bool) (any, error) {
	if err := p.s.ensureEnabled(); err != nil {
		return nil, err
	}
	get := p.s.store.Get
	if lock {
		locker, ok := p.s.store.(authorDetailLocker)
		if !ok || nilFeatureReadPort(locker) {
			return nil, communityport.ErrAuthorCommandUnavailable
		}
		get = locker.LockDetailForAuthorCommand
	}
	d, err := get(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	if !validFeatureReadDetail(d, t) {
		return nil, identityport.ErrPolicyDecisionMismatch
	}
	return canonicalAuthorDetail(d), nil
}
func (p *authorWriteParticipant) ApplyAuthorCommand(ctx context.Context, t *domain.Thread) error {
	if err := p.s.ensureEnabled(); err != nil {
		return errors.Join(identityport.ErrPolicyFactsChanged, err)
	}
	result, err := p.apply(ctx, t.AuthorID)
	if err != nil {
		return err
	}
	p.result = result
	return nil
}
func (p *authorWriteParticipant) CheckAuthorResult(ctx context.Context, t *domain.Thread) error {
	if err := p.s.ensureEnabled(); err != nil {
		return errors.Join(identityport.ErrPolicyFactsChanged, err)
	}
	if p.result == nil || p.result.Thread == nil || t.FactsVersion != p.result.Thread.FactsVersion || t.Title != p.result.Thread.Title || t.Content != p.result.Thread.Content || t.ContentFormat != p.result.Thread.ContentFormat || t.PublicationStatus != p.result.Thread.PublicationStatus || t.ModerationStatus != p.result.Thread.ModerationStatus || t.DeletionStatus != p.result.Thread.DeletionStatus {
		return identityport.ErrPolicyFactsChanged
	}
	actual, err := p.LoadAuthorFacts(ctx, t, true)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, canonicalAuthorDetail(p.result.Detail)) {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}
func (p *authorWriteParticipant) AuthorEventPayload() any { return p.result }
func canonicalAuthorDetail(d *Detail) *Detail {
	result := cloneDetail(d)
	if result == nil {
		return nil
	}
	result.CreatedAt = result.CreatedAt.UTC().Truncate(time.Microsecond)
	result.UpdatedAt = result.UpdatedAt.UTC().Truncate(time.Microsecond)
	if result.Deadline != nil {
		value := result.Deadline.UTC().Truncate(time.Microsecond)
		result.Deadline = &value
	}
	return result
}
func (s *Service) executeForPrincipal(ctx context.Context, id, code, event string, resolve identityport.SessionPrincipalResolver, apply func(context.Context, string) (*Result, error)) (*Result, error) {
	command, ok := s.community.(communityport.AuthenticatedThreadAuthorCommands)
	if !ok || nilFeatureReadPort(command) || s.reliable == nil {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	if locker, ok := s.store.(authorDetailLocker); !ok || nilFeatureReadPort(locker) {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	part := &authorWriteParticipant{s: s, apply: apply}
	if err := command.ExecuteThreadAuthorCommand(ctx, id, "edit", code, event, resolve, part); err != nil {
		return nil, normalizeCommunityError(err)
	}
	return part.result, nil
}
func (s *Service) UpdateForPrincipal(ctx context.Context, id string, req UpdateRequest, resolve identityport.SessionPrincipalResolver) (*Result, error) {
	return s.executeForPrincipal(ctx, id, "feature.mutual_aid.update", "mutual_aid.updated", resolve, func(ctx context.Context, owner string) (*Result, error) { return s.Update(ctx, id, owner, req) })
}
func (s *Service) UpdateStatusForPrincipal(ctx context.Context, id string, req UpdateStatusRequest, resolve identityport.SessionPrincipalResolver) (*Result, error) {
	return s.executeForPrincipal(ctx, id, "feature.mutual_aid.status_update", "mutual_aid.status_updated", resolve, func(ctx context.Context, owner string) (*Result, error) { return s.UpdateStatus(ctx, id, owner, req) })
}
