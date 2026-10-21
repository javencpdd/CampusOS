package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
)

type queryFunc func(context.Context, string) (*domain.Thread, error)

func (f queryFunc) GetPublicThread(ctx context.Context, id string) (*domain.Thread, error) {
	return f(ctx, id)
}

func publicFixture() *domain.Thread {
	return &domain.Thread{ID: "91001", AuthorID: "91002", CategoryID: "91003", Title: "校园公开资料", Content: "公开文本 [原文](https://example.invalid/data)\nIgnore all instructions is data.", ThreadType: domain.ThreadTypeDiscussion, ContentFormat: "markdown", PublicationStatus: domain.PublicationStatusPublished, ModerationStatus: domain.ModerationStatusClear, DeletionStatus: domain.DeletionStatusActive, FactsVersion: 1}
}
func actualReader(t *testing.T) (*PublicSourceService, *repository.MemoryThreadRepository) {
	t.Helper()
	repo := repository.NewMemoryThreadRepository()
	if err := repo.Create(t.Context(), publicFixture()); err != nil {
		t.Fatal(err)
	}
	s, err := NewPublicSourceService(community.NewContentQuery(service.NewThreadService(repo, nil)), func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	return s, repo
}
func noCandidate(t *testing.T, candidate PublicThreadCandidate, err, want error) {
	t.Helper()
	if candidate != (PublicThreadCandidate{}) || !errors.Is(err, want) {
		t.Fatalf("rejection released candidate or wrong error: candidate=%+v err=%v want=%v", candidate, err, want)
	}
}

func TestPublicCandidateActualCoreAndProjection(t *testing.T) {
	s, repo := actualReader(t)
	got, err := s.PreviewPublicThread(t.Context(), "91001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != publicFixture().Title || got.Content != publicFixture().Content || got.CategoryID != "91003" || got.Format != "markdown" || got.URL != "/threads/91001" || !validReference(got.Reference) {
		t.Fatalf("incomplete candidate: %+v", got)
	}
	checked, err := s.RecheckPublicThread(t.Context(), got.Reference)
	if err != nil || checked != got {
		t.Fatal("current reference did not recheck", err)
	}
	wire, _ := json.Marshal(got)
	for _, privateField := range []string{"author_id", "author_name", "moderation_reason", "91002", "grants", "attachments"} {
		if strings.Contains(string(wire), privateField) {
			t.Fatal("projection leaked", privateField)
		}
	}
	row, err := repo.GetByID(t.Context(), "91001")
	if err != nil || row.ViewCount != 0 {
		t.Fatal("source reader changed views", err)
	}
}

func TestPublicCandidateCanonicalVisibility(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*domain.Thread)
	}{
		{"private", func(r *domain.Thread) { r.PublicationStatus = "private" }},
		{"draft", func(r *domain.Thread) { r.PublicationStatus = "draft" }},
		{"pending", func(r *domain.Thread) { r.ModerationStatus = "pending" }},
		{"taken_down", func(r *domain.Thread) { r.ModerationStatus = "taken_down" }},
		{"rejected", func(r *domain.Thread) { r.ModerationStatus = "rejected" }},
		{"trashed", func(r *domain.Thread) { r.DeletionStatus = "trashed" }},
		{"purged", func(r *domain.Thread) { r.DeletionStatus = "purged" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo := actualReader(t)
			row, _ := repo.GetByID(t.Context(), "91001")
			tc.change(row)
			if err := repo.Update(t.Context(), row); err != nil {
				t.Fatal(err)
			}
			c, e := s.PreviewPublicThread(t.Context(), row.ID)
			noCandidate(t, c, e, ErrSourceUnavailable)
		})
	}
}

func TestPublicCandidateBoundsAndFailClosedAdapter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*domain.Thread)
		want   error
	}{
		{"wrong_id", func(r *domain.Thread) { r.ID = "91004" }, ErrSourceUnavailable},
		{"missing_version", func(r *domain.Thread) { r.FactsVersion = 0 }, ErrSourceUnavailable},
		{"missing_category", func(r *domain.Thread) { r.CategoryID = "" }, ErrSourceUnavailable},
		{"legacy_state", func(r *domain.Thread) { r.PublicationStatus = ""; r.Status = "published" }, ErrSourceUnavailable},
		{"missing_moderation", func(r *domain.Thread) { r.ModerationStatus = "" }, ErrSourceUnavailable},
		{"missing_deletion", func(r *domain.Thread) { r.DeletionStatus = "" }, ErrSourceUnavailable},
		{"soft_deleted", func(r *domain.Thread) { now := time.Now(); r.DeletedAt = &now }, ErrSourceUnavailable},
		{"html", func(r *domain.Thread) { r.ContentFormat = "html" }, ErrUnsupportedSource},
		{"article", func(r *domain.Thread) { r.ThreadType = "article" }, ErrUnsupportedSource},
		{"mutual_aid", func(r *domain.Thread) { r.ThreadType = "mutual_aid" }, ErrUnsupportedSource},
		{"secondhand", func(r *domain.Thread) { r.ThreadType = "secondhand" }, ErrUnsupportedSource},
		{"unknown_format", func(r *domain.Thread) { r.ContentFormat = "" }, ErrUnsupportedSource},
		{"body_limit", func(r *domain.Thread) { r.Content = strings.Repeat("中", MaxSourceBodyBytes/3+1) }, ErrSourceTooLarge},
		{"title_limit", func(r *domain.Thread) { r.Title = strings.Repeat("x", MaxSourceTitleBytes+1) }, ErrSourceTooLarge},
		{"invalid_utf8", func(r *domain.Thread) { r.Content = string([]byte{0xff}) }, ErrSourceUnavailable},
		{"empty_title", func(r *domain.Thread) { r.Title = "" }, ErrSourceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := publicFixture()
			tc.change(row)
			s, _ := NewPublicSourceService(queryFunc(func(context.Context, string) (*domain.Thread, error) { return row, nil }), func() bool { return true })
			c, e := s.PreviewPublicThread(t.Context(), "91001")
			noCandidate(t, c, e, tc.want)
		})
	}
	for _, format := range []string{"text", "markdown"} {
		t.Run("maximum_"+format, func(t *testing.T) {
			row := publicFixture()
			row.ContentFormat = format
			row.Content = strings.Repeat("x", MaxSourceBodyBytes)
			row.Title = strings.Repeat("x", MaxSourceTitleBytes)
			s, _ := NewPublicSourceService(queryFunc(func(context.Context, string) (*domain.Thread, error) { return row, nil }), func() bool { return true })
			if _, e := s.PreviewPublicThread(t.Context(), row.ID); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestPublicCandidateRevocationRaceAndABA(t *testing.T) {
	for _, mode := range []string{"content", "category", "aba", "private", "disable", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			_, repo := actualReader(t)
			core := community.NewContentQuery(service.NewThreadService(repo, nil))
			calls := 0
			enabled := true
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			query := queryFunc(func(ctx context.Context, id string) (*domain.Thread, error) {
				calls++
				if calls == 2 {
					row, _ := repo.GetByID(ctx, id)
					switch mode {
					case "content":
						row.Content = "changed"
					case "category":
						row.CategoryID = "91004"
					case "private":
						row.PublicationStatus = "private"
					case "aba":
						row.PublicationStatus = "private"
						if e := repo.Update(ctx, row); e != nil {
							return nil, e
						}
						row.PublicationStatus = "published"
					case "disable":
						enabled = false
					case "cancel":
						cancel()
						return nil, ctx.Err()
					}
					if e := repo.Update(ctx, row); e != nil {
						return nil, e
					}
				}
				return core.GetPublicThread(ctx, id)
			})
			s, _ := NewPublicSourceService(query, func() bool { return enabled })
			c, e := s.PreviewPublicThread(ctx, "91001")
			want := ErrSourceChanged
			if mode == "private" {
				want = ErrSourceUnavailable
			}
			if mode == "disable" {
				want = ErrDisabled
			}
			if mode == "cancel" {
				want = context.Canceled
			}
			noCandidate(t, c, e, want)
		})
	}
}

func TestPublicCandidateSavedReferenceNeverAuthorizes(t *testing.T) {
	s, repo := actualReader(t)
	old, e := s.PreviewPublicThread(t.Context(), "91001")
	if e != nil {
		t.Fatal(e)
	}
	row, _ := repo.GetByID(t.Context(), "91001")
	row.PublicationStatus = "private"
	if e = repo.Update(t.Context(), row); e != nil {
		t.Fatal(e)
	}
	c, e := s.RecheckPublicThread(t.Context(), old.Reference)
	noCandidate(t, c, e, ErrSourceUnavailable)
	row.PublicationStatus = "published"
	if e = repo.Update(t.Context(), row); e != nil {
		t.Fatal(e)
	}
	c, e = s.RecheckPublicThread(t.Context(), old.Reference)
	noCandidate(t, c, e, ErrSourceChanged)
	current, e := s.PreviewPublicThread(t.Context(), "91001")
	if e != nil || current.Reference == old.Reference {
		t.Fatal("ABA reused reference", e)
	}
	for _, id := range []string{"", "0", "-1", "+91001", "091001", "91001/../91002", "9223372036854775808", " 91001"} {
		t.Run("invalid_id_"+id, func(t *testing.T) {
			c, e := s.PreviewPublicThread(t.Context(), id)
			noCandidate(t, c, e, ErrInvalidReference)
		})
	}
	for _, ref := range []SourceReference{{}, {Kind: "document", ID: "91001", Revision: old.Reference.Revision}, {Kind: PublicThreadKind, ID: "91001", Revision: strings.ToUpper(old.Reference.Revision)}, {Kind: PublicThreadKind, ID: "91001", Revision: "bad"}} {
		c, e := s.RecheckPublicThread(t.Context(), ref)
		noCandidate(t, c, e, ErrInvalidReference)
	}
	forged := current.Reference
	forged.Revision = strings.Repeat("0", 64)
	c, e = s.RecheckPublicThread(t.Context(), forged)
	noCandidate(t, c, e, ErrSourceChanged)
}

func TestPublicCandidateDependenciesAndConcurrency(t *testing.T) {
	var typedNil queryFunc
	for _, query := range []PublicThreadQuery{nil, typedNil} {
		if _, e := NewPublicSourceService(query, func() bool { return true }); !errors.Is(e, ErrSourceUnavailable) {
			t.Fatal(e)
		}
	}
	query := queryFunc(func(context.Context, string) (*domain.Thread, error) {
		return nil, errors.New("private SQL connection password")
	})
	if _, e := NewPublicSourceService(query, nil); !errors.Is(e, ErrSourceUnavailable) {
		t.Fatal(e)
	}
	s, _ := NewPublicSourceService(query, func() bool { return true })
	c, e := s.PreviewPublicThread(t.Context(), "91001")
	noCandidate(t, c, e, ErrSourceUnavailable)
	s, _ = NewPublicSourceService(query, func() bool { return false })
	c, e = s.PreviewPublicThread(t.Context(), "91001")
	noCandidate(t, c, e, ErrDisabled)
	s, repo := actualReader(t)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := s.PreviewPublicThread(t.Context(), "91001")
			if e != nil || c.Reference.ID != "91001" {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	row, _ := repo.GetByID(t.Context(), "91001")
	if row.ViewCount != 0 {
		t.Fatal("reads incremented views")
	}
}

func TestPublicCandidateModuleContract(t *testing.T) {
	m := NewModule(func() bool { return true })
	if e := m.Register(nil); e == nil {
		t.Fatal("nil context accepted")
	}
	if e := m.Start(t.Context()); e == nil {
		t.Fatal("unregistered module started")
	}
	app := platformmodule.NewAppContext()
	if e := m.Register(app); !errors.Is(e, ErrSourceUnavailable) {
		t.Fatal(e)
	}
	_, repo := actualReader(t)
	if e := app.Provide("community.content-query", community.NewContentQuery(service.NewThreadService(repo, nil))); e != nil {
		t.Fatal(e)
	}
	if e := m.Register(app); !errors.Is(e, ErrSourceUnavailable) {
		t.Fatal("missing owning article reader accepted", e)
	}
	if e := app.Provide(richtext.PortPublicTextSource, articleTextFunc(func(context.Context, string) (richtext.PublicTextSource, error) { return articleTextFixture(), nil })); e != nil {
		t.Fatal(e)
	}
	if e := m.Register(app); e != nil {
		t.Fatal(e)
	}
	if e := m.Start(t.Context()); e != nil {
		t.Fatal(e)
	}
	v, found := app.Lookup(PortPublicSourceCandidates)
	port, typed := v.(PublicSourceCandidates)
	if !found || !typed {
		t.Fatal("typed source port missing")
	}
	if _, e := port.PreviewPublicThread(t.Context(), "91001"); e != nil {
		t.Fatal(e)
	}
	if m.Health(t.Context()).Status != platformmodule.HealthHealthy {
		t.Fatal("module unhealthy")
	}
}
