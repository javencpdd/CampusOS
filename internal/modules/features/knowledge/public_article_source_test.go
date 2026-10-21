package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"
)

type articleTextFunc func(context.Context, string) (richtext.PublicTextSource, error)

func (f articleTextFunc) ReadPublicTextSource(ctx context.Context, id string) (richtext.PublicTextSource, error) {
	return f(ctx, id)
}
func articleTextFixture() richtext.PublicTextSource {
	return richtext.PublicTextSource{ThreadID: "92001", ArticleID: "92002", CategoryID: "92003", Title: "Public article", Text: "owned public text", Revision: strings.Repeat("a", 64)}
}
func actualRichTextCandidate(t *testing.T) (*PublicArticleSourceService, *richtext.Service, string) {
	t.Helper()
	repo := communityrepo.NewMemoryThreadRepository()
	core := communityservice.NewThreadService(repo, nil)
	rich := richtext.NewService(richtext.NewMemoryStore(), community.NewContentGateway(repo, core))
	rich.SetEnabledChecker(func() bool { return true })
	a, e := rich.CreateDraft(t.Context(), "1001", "Alice", richtext.SaveArticleRequest{Title: "Real article", CategoryID: "92003", ContentHTML: "<p>Actual <b>public</b> body</p><script>NO_SCRIPT</script>"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = rich.Publish(t.Context(), a.ThreadID, "1001"); e != nil {
		t.Fatal(e)
	}
	s, e := NewPublicArticleSourceService(rich.PublicTextSources(), func() bool { return true })
	if e != nil {
		t.Fatal(e)
	}
	return s, rich, a.ThreadID
}
func TestArticleCandidateActualRichText(t *testing.T) {
	s, rich, id := actualRichTextCandidate(t)
	c, e := s.PreviewPublicArticle(t.Context(), id)
	if e != nil {
		t.Fatal(e)
	}
	if c.Content != "Actual public body" || c.Format != "text" || c.Reference.Kind != PublicArticleKind || c.Reference.ID != id || c.URL != "/threads/"+id || c.CategoryID != "92003" {
		t.Fatalf("invalid article candidate %+v", c)
	}
	if _, e = s.RecheckPublicArticle(t.Context(), c.Reference); e != nil {
		t.Fatal(e)
	}
	wire, _ := json.Marshal(c)
	for _, bad := range []string{"NO_SCRIPT", "created_by", "ContentHTML", "sanitized_html", "article_id", "attachments"} {
		if strings.Contains(string(wire), bad) {
			t.Fatal("candidate leaked", bad)
		}
	}
	if _, e = rich.Offline(t.Context(), id, "1001"); e != nil {
		t.Fatal(e)
	}
	x, e := s.RecheckPublicArticle(t.Context(), c.Reference)
	noCandidate(t, x, e, ErrSourceUnavailable)
	if _, e = rich.Publish(t.Context(), id, "1001"); e != nil {
		t.Fatal(e)
	}
	x, e = s.RecheckPublicArticle(t.Context(), c.Reference)
	noCandidate(t, x, e, ErrSourceChanged)
}
func TestArticleCandidateSourceRaceAndKinds(t *testing.T) {
	for _, mode := range []string{"body", "epoch", "category", "source_kind", "private", "disable", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			row := articleTextFixture()
			calls := 0
			enabled := true
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reader := articleTextFunc(func(context.Context, string) (richtext.PublicTextSource, error) {
				calls++
				if calls == 2 {
					switch mode {
					case "body":
						row.Text = "new text"
					case "epoch":
						row.Revision = strings.Repeat("b", 64)
					case "category":
						row.CategoryID = "92004"
					case "source_kind":
						row.ThreadID = "92004"
					case "private":
						return richtext.PublicTextSource{}, richtext.ErrPublicTextUnavailable
					case "disable":
						enabled = false
					case "cancel":
						cancel()
					}
				}
				return row, nil
			})
			s, _ := NewPublicArticleSourceService(reader, func() bool { return enabled })
			x, e := s.PreviewPublicArticle(ctx, "92001")
			want := ErrSourceChanged
			if mode == "source_kind" || mode == "private" {
				want = ErrSourceUnavailable
			}
			if mode == "disable" {
				want = ErrDisabled
			}
			if mode == "cancel" {
				want = context.Canceled
			}
			noCandidate(t, x, e, want)
		})
	}
	s, _, id := actualRichTextCandidate(t)
	c, e := s.PreviewPublicArticle(t.Context(), id)
	if e != nil {
		t.Fatal(e)
	}
	ref := c.Reference
	ref.Kind = PublicThreadKind
	x, e := s.RecheckPublicArticle(t.Context(), ref)
	noCandidate(t, x, e, ErrInvalidReference)
	ref = c.Reference
	ref.Revision = strings.Repeat("0", 64)
	x, e = s.RecheckPublicArticle(t.Context(), ref)
	noCandidate(t, x, e, ErrSourceChanged)
}
func TestArticleCandidateBudgetsAndUnavailableAdapters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*richtext.PublicTextSource)
		want   error
	}{
		{"body_limit", func(r *richtext.PublicTextSource) { r.Text = strings.Repeat("中", MaxSourceBodyBytes/3+1) }, ErrSourceTooLarge},
		{"title_limit", func(r *richtext.PublicTextSource) { r.Title = strings.Repeat("x", MaxSourceTitleBytes+1) }, ErrSourceTooLarge},
		{"invalid_text", func(r *richtext.PublicTextSource) { r.Text = string([]byte{0xff}) }, ErrSourceUnavailable},
		{"missing_article", func(r *richtext.PublicTextSource) { r.ArticleID = "" }, ErrSourceUnavailable},
		{"wrong_thread", func(r *richtext.PublicTextSource) { r.ThreadID = "92005" }, ErrSourceUnavailable},
		{"missing_revision", func(r *richtext.PublicTextSource) { r.Revision = "" }, ErrSourceUnavailable},
		{"upper_revision", func(r *richtext.PublicTextSource) { r.Revision = strings.Repeat("A", 64) }, ErrSourceUnavailable},
		{"empty_title", func(r *richtext.PublicTextSource) { r.Title = "" }, ErrSourceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := articleTextFixture()
			tc.change(&row)
			s, _ := NewPublicArticleSourceService(articleTextFunc(func(context.Context, string) (richtext.PublicTextSource, error) { return row, nil }), func() bool { return true })
			x, e := s.PreviewPublicArticle(t.Context(), "92001")
			noCandidate(t, x, e, tc.want)
		})
	}
	for _, tc := range []struct{ upstream, want error }{{richtext.ErrPublicTextTooLarge, ErrSourceTooLarge}, {richtext.ErrPublicTextChanged, ErrSourceChanged}, {errors.New("DB password private original title"), ErrSourceUnavailable}} {
		s, _ := NewPublicArticleSourceService(articleTextFunc(func(context.Context, string) (richtext.PublicTextSource, error) {
			return richtext.PublicTextSource{}, tc.upstream
		}), func() bool { return true })
		x, e := s.PreviewPublicArticle(t.Context(), "92001")
		noCandidate(t, x, e, tc.want)
	}
	var absent articleTextFunc
	for _, reader := range []richtext.PublicTextSourceReader{nil, absent} {
		if _, e := NewPublicArticleSourceService(reader, func() bool { return true }); !errors.Is(e, ErrSourceUnavailable) {
			t.Fatal(e)
		}
	}
	s, _ := NewPublicArticleSourceService(articleTextFunc(func(context.Context, string) (richtext.PublicTextSource, error) { return articleTextFixture(), nil }), func() bool { return false })
	x, e := s.PreviewPublicArticle(t.Context(), "92001")
	noCandidate(t, x, e, ErrDisabled)
	for _, id := range []string{"", "01", "+1", "1/../2"} {
		x, e := s.PreviewPublicArticle(t.Context(), id)
		noCandidate(t, x, e, ErrInvalidReference)
	}
}
