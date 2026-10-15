package richtext

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/pkg/eventbus"
)

type authorArticleLocker interface {
	LockArticleForAuthorCommand(context.Context, string) (*Article, error)
}
type articleAuthorPart struct {
	s         *Service
	operation string
	apply     func(context.Context, string) (*ArticleResult, error)
	initial   *Article
	result    *ArticleResult
}

func (p *articleAuthorPart) ThreadType() domain.ThreadType { return domain.ThreadTypeArticle }
func (p *articleAuthorPart) LoadAuthorFacts(ctx context.Context, t *domain.Thread, lock bool) (any, error) {
	if err := p.s.ensureEnabled(); err != nil {
		return nil, err
	}
	get := p.s.store.GetArticleByThreadID
	if lock {
		reader, ok := p.s.store.(authorArticleLocker)
		if !ok || nilReadAdapter(reader) {
			return nil, communityport.ErrAuthorCommandUnavailable
		}
		get = reader.LockArticleForAuthorCommand
	}
	a, err := get(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	if a == nil || a.ID == "" || a.ThreadID != t.ID || a.CreatedBy != t.AuthorID || a.Title != t.Title {
		return nil, identityport.ErrPolicyDecisionMismatch
	}
	snap := canonicalAuthorArticle(a)
	if lock && p.initial == nil {
		p.initial = cloneArticle(a)
	}
	return snap, nil
}
func (p *articleAuthorPart) ApplyAuthorCommand(ctx context.Context, t *domain.Thread) error {
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
func (p *articleAuthorPart) CheckAuthorResult(ctx context.Context, t *domain.Thread) error {
	if err := p.s.ensureEnabled(); err != nil {
		return errors.Join(identityport.ErrPolicyFactsChanged, err)
	}
	actual, err := p.s.store.(authorArticleLocker).LockArticleForAuthorCommand(ctx, t.ID)
	if err != nil {
		return err
	}
	if actual == nil || actual.ThreadID != t.ID || actual.CreatedBy != t.AuthorID || actual.Title != t.Title || actual.UpdatedBy != t.AuthorID {
		return identityport.ErrPolicyFactsChanged
	}
	if p.operation == "trash" {
		expected := cloneArticle(p.initial)
		applyArticleThreadState(expected, t)
		expected.UpdatedBy = t.AuthorID
		normalizeArticleActor(expected)
		if actual.FactsVersion <= p.initial.FactsVersion {
			return identityport.ErrPolicyFactsChanged
		}
		expected.FactsVersion = actual.FactsVersion
		if actual.UpdatedAt.Before(p.initial.UpdatedAt) {
			return identityport.ErrPolicyFactsChanged
		}
		expected.UpdatedAt = actual.UpdatedAt
		if !reflect.DeepEqual(canonicalAuthorArticle(actual), canonicalAuthorArticle(expected)) {
			return identityport.ErrPolicyFactsChanged
		}
		p.result = articleResult(actual)
		return nil
	}
	if p.result == nil || p.result.Article == nil || p.result.ThreadID != t.ID || !reflect.DeepEqual(canonicalAuthorArticle(actual), canonicalAuthorArticle(p.result.Article)) {
		return identityport.ErrPolicyFactsChanged
	}
	// Publication/moderation/deletion are Core facts; cached Feature status must
	// match the actual result rather than widening visibility.
	projected := cloneArticle(actual)
	applyArticleThreadState(projected, t)
	if projected.Status != actual.Status {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}
func (p *articleAuthorPart) AuthorEventPayload() any { return p.result }
func canonicalAuthorArticle(a *Article) *Article {
	result := canonicalCreatedArticle(a)
	if result == nil {
		return nil
	}
	result.CreatedAt = result.CreatedAt.Truncate(time.Microsecond)
	result.UpdatedAt = result.UpdatedAt.Truncate(time.Microsecond)
	if result.PublishedAt != nil {
		v := result.PublishedAt.UTC().Truncate(time.Microsecond)
		result.PublishedAt = &v
	}
	return result
}
func (s *Service) executeArticleForPrincipal(ctx context.Context, id, operation, code, event string, resolve identityport.SessionPrincipalResolver, apply func(context.Context, string) (*ArticleResult, error)) (*ArticleResult, error) {
	commands, ok := s.community.(communityport.AuthenticatedThreadAuthorCommands)
	if !ok || nilReadAdapter(commands) || s.reliable == nil {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	if reader, ok := s.store.(authorArticleLocker); !ok || nilReadAdapter(reader) {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	part := &articleAuthorPart{s: s, operation: operation, apply: apply}
	if err := commands.ExecuteThreadAuthorCommand(ctx, id, operation, code, event, resolve, part); err != nil {
		if errors.Is(err, communityport.ErrThreadNotFound) {
			return nil, ErrArticleNotFound
		}
		return nil, err
	}
	return part.result, nil
}
func (s *Service) UpdateDraftForPrincipal(ctx context.Context, id string, req SaveArticleRequest, resolve identityport.SessionPrincipalResolver) (*ArticleResult, error) {
	return s.executeArticleForPrincipal(ctx, id, "edit", "feature.richtext.article.update", eventbus.EventThreadUpdated, resolve, func(ctx context.Context, owner string) (*ArticleResult, error) {
		return s.UpdateDraft(ctx, id, owner, req)
	})
}
func (s *Service) PublishForPrincipal(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver) (*ArticleResult, error) {
	return s.executeArticleForPrincipal(ctx, id, "edit", "feature.richtext.article.publish", eventbus.EventThreadUpdated, resolve, func(ctx context.Context, owner string) (*ArticleResult, error) { return s.Publish(ctx, id, owner) })
}
func (s *Service) OfflineForPrincipal(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver) (*ArticleResult, error) {
	return s.executeArticleForPrincipal(ctx, id, "edit", "feature.richtext.article.offline", eventbus.EventThreadUpdated, resolve, func(ctx context.Context, owner string) (*ArticleResult, error) { return s.Offline(ctx, id, owner) })
}
func (s *Service) DeleteForPrincipal(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver) error {
	_, err := s.executeArticleForPrincipal(ctx, id, "trash", "feature.richtext.article.author_trash", eventbus.EventThreadDeleted, resolve, func(ctx context.Context, owner string) (*ArticleResult, error) { return nil, s.Delete(ctx, id, owner) })
	return err
}
