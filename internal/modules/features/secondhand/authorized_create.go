package secondhand

import (
	"context"
	"errors"
	"reflect"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type structuredCreator func(context.Context, string, string, domain.CreateThreadRequest, communityport.ThreadCreateOptions, communityport.StructuredThreadParticipant) (*domain.Thread, error)

type checkedCreateParticipant struct {
	createParticipant
	expected *Detail
}

func (p *checkedCreateParticipant) PersistThreadDetail(ctx context.Context, thread *domain.Thread) error {
	if !transaction.Active(ctx) || p.detail == nil || thread == nil || p.detail.CreatedBy != thread.AuthorID || p.detail.Version != 1 {
		return identityport.ErrPolicyDecisionMismatch
	}
	if err := p.createParticipant.PersistThreadDetail(ctx, thread); err != nil {
		return err
	}
	if !validFeatureReadDetail(p.detail, thread) {
		return identityport.ErrPolicyDecisionMismatch
	}
	p.expected = cloneDetail(p.detail)
	return p.check(ctx)
}
func (p *checkedCreateParticipant) check(ctx context.Context) error {
	if p.expected == nil {
		return nil
	}
	actual, err := p.store.Get(ctx, p.expected.ThreadID)
	if err != nil {
		return err
	}
	actual = cloneDetail(actual)
	if actual != nil {
		actual.CreatedAt = actual.CreatedAt.UTC()
		actual.UpdatedAt = actual.UpdatedAt.UTC()
	}
	if !reflect.DeepEqual(actual, p.expected) {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}

// CreateForPrincipal is the authenticated Feature ingress. The current User
// owns the new detail; the actual typed participant and Core thread are checked
// before the shared required-audit transaction can commit.
func (s *Service) CreateForPrincipal(ctx context.Context, authorName string, req CreateRequest, resolve identityport.SessionPrincipalResolver) (*Result, error) {
	if s.reliable == nil || nilFeatureReadPort(s.store) {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	creator, ok := s.community.(communityport.AuthenticatedThreadCreator)
	if !ok || nilFeatureReadPort(creator) {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	if resolve == nil {
		return nil, identityport.ErrUserSessionPrincipalUnavailable
	}
	initial, err := resolve(ctx)
	if err != nil {
		return nil, err
	}
	if err := initial.Validate(); err != nil {
		return nil, identityport.ErrUserSessionPrincipalUnavailable
	}
	if initial.Actor.Kind != "user" || initial.Audience != "user" {
		return nil, communityport.ErrAuthorCommandDenied
	}
	participant := &checkedCreateParticipant{}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		if err := s.ensureEnabled(); err != nil {
			return identityport.PrincipalContext{}, errors.Join(identityport.ErrPolicyFactsChanged, err)
		}
		p, err := resolve(ctx)
		if err != nil {
			return identityport.PrincipalContext{}, err
		}
		if !reflect.DeepEqual(p, initial) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		if err := participant.check(ctx); err != nil {
			return identityport.PrincipalContext{}, errors.Join(identityport.ErrPolicyFactsChanged, err)
		}
		return p, nil
	}
	create := func(ctx context.Context, _ string, name string, request domain.CreateThreadRequest, opts communityport.ThreadCreateOptions, detail communityport.StructuredThreadParticipant) (*domain.Thread, error) {
		return creator.CreateStructuredThreadForPrincipal(ctx, name, request, opts, detail, bound)
	}
	result, err := s.create(ctx, initial.Actor.ID, authorName, req, create, participant)
	if err != nil {
		return nil, err
	}
	return result, nil
}
