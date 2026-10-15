package richtext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type structuredArticleCreator func(context.Context, string, string, domain.CreateThreadRequest, communityport.ThreadCreateOptions, communityport.StructuredThreadParticipant) (*domain.Thread, error)

type checkedArticleCreateParticipant struct {
	articleCreateParticipant
	expected *Article
}

func (p *checkedArticleCreateParticipant) PersistThreadDetail(ctx context.Context, thread *domain.Thread) error {
	if !transaction.Active(ctx) || p.article == nil || thread == nil || p.article.CreatedBy != thread.AuthorID || p.article.Status != StatusDraft {
		return identityport.ErrPolicyDecisionMismatch
	}
	if err := p.articleCreateParticipant.PersistThreadDetail(ctx, thread); err != nil {
		return err
	}
	if p.article.ThreadID != thread.ID || p.article.CreatedBy != thread.AuthorID || p.article.Title != thread.Title {
		return identityport.ErrPolicyDecisionMismatch
	}
	p.expected = canonicalCreatedArticle(p.article)
	return p.check(ctx)
}
func (p *checkedArticleCreateParticipant) check(ctx context.Context) error {
	if p.expected == nil {
		return nil
	}
	actual, err := p.store.GetArticleByThreadID(ctx, p.expected.ThreadID)
	if err != nil {
		return err
	}
	actual = canonicalCreatedArticle(actual)
	if actual != nil {
		actual.CreatedAt = actual.CreatedAt.UTC()
		actual.UpdatedAt = actual.UpdatedAt.UTC()
	}
	if !reflect.DeepEqual(actual, p.expected) {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}

// CreateDraftForPrincipal is the authenticated Feature ingress. The current User
// owns the new detail; the actual typed participant and Core thread are checked
// before the shared required-audit transaction can commit.
func (s *Service) CreateDraftForPrincipal(ctx context.Context, authorName string, req SaveArticleRequest, resolve identityport.SessionPrincipalResolver) (*ArticleResult, error) {
	if s.reliable == nil || nilReadAdapter(s.store) {
		return nil, communityport.ErrAuthorCommandUnavailable
	}
	creator, ok := s.community.(communityport.AuthenticatedThreadCreator)
	if !ok || nilReadAdapter(creator) {
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
	participant := &checkedArticleCreateParticipant{}
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
	result, err := s.createDraft(ctx, initial.Actor.ID, authorName, req, create, participant)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// PostgreSQL jsonb preserves JSON values rather than input whitespace/key order.
// UseNumber retains exact number tokens; no float rounding may hide a change.
func canonicalCreatedArticle(a *Article) *Article {
	if a == nil {
		return nil
	}
	result := cloneArticle(a)
	result.CreatedAt = result.CreatedAt.UTC()
	result.UpdatedAt = result.UpdatedAt.UTC()
	var value any
	decoder := json.NewDecoder(bytes.NewReader(normalizeJSON(result.ContentJSON)))
	decoder.UseNumber()
	if decoder.Decode(&value) == nil {
		if normalized, err := json.Marshal(canonicalArticleJSON(value)); err == nil {
			result.ContentJSON = normalized
		}
	}
	return result
}

func canonicalArticleJSON(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = canonicalArticleJSON(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = canonicalArticleJSON(item)
		}
		return v
	case json.Number:
		raw := string(v)
		negative := strings.HasPrefix(raw, "-")
		raw = strings.TrimPrefix(raw, "-")
		exponent := int64(0)
		if index := strings.IndexAny(raw, "eE"); index >= 0 {
			var err error
			exponent, err = strconv.ParseInt(raw[index+1:], 10, 32)
			if err != nil {
				return v
			}
			raw = raw[:index]
		}
		if index := strings.IndexByte(raw, '.'); index >= 0 {
			exponent -= int64(len(raw) - index - 1)
			raw = raw[:index] + raw[index+1:]
		}
		digits := strings.TrimLeft(raw, "0")
		if digits == "" {
			return json.Number("0")
		}
		trimmed := strings.TrimRight(digits, "0")
		exponent += int64(len(digits) - len(trimmed))
		if negative {
			trimmed = "-" + trimmed
		}
		return json.Number(trimmed + "e" + strconv.FormatInt(exponent, 10))
	}
	return value
}
