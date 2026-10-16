package richtext

import (
	"context"
	"errors"
	"math"
	"reflect"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var ErrArticleFactsUnavailable = errors.New("article authority facts unavailable")
var ErrArticleFactsConflict = errors.New("article authority facts changed")

type ArticleAuthorityStore interface {
	LockArticleAuthority(context.Context, string) (*Article, error)
	UpdateArticleIfFacts(context.Context, *Article, int64) error
}
type articleHiddenFacts struct {
	Version  int64
	Kind, ID string
}

func normalizeArticleActor(a *Article) {
	if a.UpdatedBy != "" {
		a.UpdatedActorKind, a.UpdatedActorID = "user", a.UpdatedBy
		return
	}
	if a.UpdatedActorKind == "" || a.UpdatedActorKind == "legacy_unknown" {
		a.UpdatedActorKind, a.UpdatedActorID = "legacy_unknown", ""
	}
}
func (s *MemoryStore) nextArticleVersion() (int64, error) {
	if s.articleVersionCounter == math.MaxInt64 {
		return 0, ErrArticleFactsUnavailable
	}
	s.articleVersionCounter++
	return s.articleVersionCounter, nil
}
func sameArticleAuthority(a, b *Article) bool {
	if a == nil || b == nil {
		return false
	}
	x, y := canonicalAuthorArticle(a), canonicalAuthorArticle(b)
	x.FactsVersion, y.FactsVersion = 0, 0
	x.UpdatedAt, y.UpdatedAt = x.CreatedAt, y.CreatedAt
	return reflect.DeepEqual(x, y)
}
func (s *PgStore) requireArticleFacts(ctx context.Context) error {
	var managed bool
	if err := s.db(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.richtext_article_contents'::regclass AND tgname='trg_article_facts_version' AND NOT tgisinternal AND tgenabled IN('O','A') AND tgtype=23 AND tgfoid=to_regprocedure('public.advance_article_facts_version()')) AND EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.article_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1)`).Scan(&managed); err != nil || !managed {
		return ErrArticleFactsUnavailable
	}
	return nil
}
func (s *PgStore) LockArticleAuthority(ctx context.Context, id string) (*Article, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrArticleFactsUnavailable
	}
	row, err := s.getArticle(ctx, `thread_id=$1 AND deleted_at IS NULL`, id, true)
	if err != nil {
		return nil, err
	}
	if err := s.requireArticleFacts(ctx); err != nil {
		return nil, err
	}
	return row, nil
}
func (s *PgStore) UpdateArticleIfFacts(ctx context.Context, a *Article, version int64) error {
	if _, ok := transaction.FromContext(ctx); !ok {
		return ErrArticleFactsUnavailable
	}
	if a == nil || version < 1 {
		return ErrArticleFactsConflict
	}
	row, err := s.LockArticleAuthority(ctx, a.ThreadID)
	if err != nil {
		return err
	}
	if row.FactsVersion != version {
		return ErrArticleFactsConflict
	}
	return s.updateArticle(ctx, a, &version)
}
func (s *MemoryStore) LockArticleAuthority(ctx context.Context, id string) (*Article, error) {
	if !transaction.Active(ctx) {
		return nil, ErrArticleFactsUnavailable
	}
	return s.GetArticleByThreadID(ctx, id)
}
func (s *MemoryStore) UpdateArticleIfFacts(ctx context.Context, a *Article, version int64) error {
	if !transaction.Active(ctx) {
		return ErrArticleFactsUnavailable
	}
	if a == nil || version < 1 {
		return ErrArticleFactsConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.articles[a.ThreadID]
	if !ok || current.FactsVersion != version {
		return ErrArticleFactsConflict
	}
	return s.updateArticleLocked(a, current)
}
func (s *MemoryStore) updateArticleLocked(a, current *Article) error {
	normalizeArticleActor(a)
	version := current.FactsVersion
	if !sameArticleAuthority(a, current) {
		var err error
		version, err = s.nextArticleVersion()
		if err != nil {
			return err
		}
	}
	a.FactsVersion = version
	s.articles[a.ThreadID] = cloneArticle(a)
	return nil
}
