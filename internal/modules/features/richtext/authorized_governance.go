package richtext

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// Executing governance does not authorize a private Article body response.
type ArticleGovernanceResult struct {
	ThreadID string `json:"thread_id"`
	Status   string `json:"status"`
}
type articleGovernancePart struct {
	s                *Service
	store            ArticleAuthorityStore
	operation        string
	initial, written *Article
	result           *ArticleGovernanceResult
}

func (p *articleGovernancePart) ThreadType() domain.ThreadType { return domain.ThreadTypeArticle }
func (p *articleGovernancePart) LoadGovernanceFacts(ctx context.Context, t *domain.Thread, lock bool) (any, error) {
	if err := p.s.ensureEnabled(); err != nil {
		if lock {
			return nil, errors.Join(identityport.ErrPolicyFactsChanged, err)
		}
		return nil, err
	}
	get := p.s.store.GetArticleByThreadID
	if lock {
		get = p.store.LockArticleAuthority
	}
	a, err := get(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	if a == nil || a.ID == "" || a.ThreadID != t.ID || a.CreatedBy != t.AuthorID || a.Title != t.Title || a.FactsVersion < 1 {
		return nil, identityport.ErrPolicyFactsChanged
	}
	// Core state authorizes the transition; an old display mirror never does.
	if lock {
		p.initial = cloneArticle(a)
	}
	return canonicalAuthorArticle(a), nil
}
func (p *articleGovernancePart) ApplyGovernanceCommand(ctx context.Context, t *domain.Thread, actor identityport.PrincipalRef) error {
	if err := p.s.ensureEnabled(); err != nil {
		return errors.Join(identityport.ErrPolicyFactsChanged, err)
	}
	if p.initial == nil || actor.Kind != "admin" || actor.ID == "" {
		return identityport.ErrPolicyFactsChanged
	}
	expected := cloneArticle(p.initial)
	applyArticleThreadState(expected, t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if p.operation == "direct_restore" {
		sanitized, err := Sanitize(expected.ContentHTML)
		if err != nil {
			return err
		}
		if strings.TrimSpace(expected.Title) == "" {
			return ErrInvalidArticle
		}
		expected.SanitizedHTML = sanitized.HTML
		expected.RenderHTML = RenderArticleHTML(sanitized.HTML)
		if expected.Status == StatusPublished {
			expected.PublishedAt = &now
		}
	}
	expected.UpdatedBy = ""
	expected.UpdatedActorKind, expected.UpdatedActorID = actor.Kind, actor.ID
	expected.UpdatedAt = now
	if err := p.store.UpdateArticleIfFacts(ctx, expected, p.initial.FactsVersion); err != nil {
		return err
	}
	if expected.FactsVersion <= p.initial.FactsVersion {
		return identityport.ErrPolicyFactsChanged
	}
	p.written = cloneArticle(expected)
	p.result = &ArticleGovernanceResult{ThreadID: t.ID, Status: expected.Status}
	return nil
}
func (p *articleGovernancePart) CheckGovernanceResult(ctx context.Context, t *domain.Thread, actor identityport.PrincipalRef) error {
	if err := p.s.ensureEnabled(); err != nil {
		return errors.Join(identityport.ErrPolicyFactsChanged, err)
	}
	actual, err := p.store.LockArticleAuthority(ctx, t.ID)
	if err != nil {
		return err
	}
	if p.written == nil || actual == nil || actual.UpdatedBy != "" || actual.UpdatedActorKind != actor.Kind || actual.UpdatedActorID != actor.ID || !reflect.DeepEqual(canonicalAuthorArticle(actual), canonicalAuthorArticle(p.written)) {
		return identityport.ErrPolicyFactsChanged
	}
	projected := cloneArticle(actual)
	applyArticleThreadState(projected, t)
	if projected.Status != actual.Status {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}
func (s *Service) AdminGovernanceForPrincipal(ctx context.Context, id, operation, reason string, resolve identityport.SessionPrincipalResolver) (*ArticleGovernanceResult, error) {
	if s == nil {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	commands, ok := s.community.(communityport.AuthenticatedThreadGovernanceCommands)
	if !ok || nilReadAdapter(commands) || s.reliable == nil {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	store, ok := s.store.(ArticleAuthorityStore)
	if !ok || nilReadAdapter(store) {
		return nil, communityport.ErrGovernanceCommandUnavailable
	}
	if operation != "take_down" && operation != "direct_restore" && operation != "trash" {
		return nil, identityport.ErrResourcePolicyUnknownAction
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		if resolve == nil {
			return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
		}
		p, err := resolve(ctx)
		if err != nil {
			return identityport.PrincipalContext{}, err
		}
		if p.Validate() != nil || p.Actor.Kind != "admin" || p.Audience != "admin" {
			return identityport.PrincipalContext{}, identityport.ErrAdminSessionPrincipalUnavailable
		}
		return p, nil
	}
	part := &articleGovernancePart{s: s, store: store, operation: operation}
	if err := commands.ExecuteThreadGovernanceCommand(ctx, id, operation, reason, bound, part); err != nil {
		if errors.Is(err, communityport.ErrThreadNotFound) || errors.Is(err, ErrArticleNotFound) {
			return nil, ErrArticleNotFound
		}
		return nil, err
	}
	return part.result, nil
}
