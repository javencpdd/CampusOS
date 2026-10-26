package richtext

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func textSourceFixture(t *testing.T, html string) (*Service, string) {
	t.Helper()
	s, _ := newTestService(true)
	a, e := s.CreateDraft(t.Context(), "1001", "Alice", SaveArticleRequest{Title: "公开文章", CategoryID: "1", ContentHTML: html})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Publish(t.Context(), a.ThreadID, "1001"); e != nil {
		t.Fatal(e)
	}
	return s, a.ThreadID
}
func deniedText(t *testing.T, c PublicTextSource, e, want error) {
	t.Helper()
	if c != (PublicTextSource{}) || !errors.Is(e, want) {
		t.Fatalf("rejection released source or wrong error: %v want=%v", e, want)
	}
}
func TestPublicArticleTextOwnsFullBodyProjection(t *testing.T) {
	body := strings.Repeat("完整正文 ", 180)
	s, id := textSourceFixture(t, "<h1>Title &amp; label</h1><p>"+body+"正文末尾</p><a href='https://example.invalid/private-link'>链接文字</a><script>NEVER_SCRIPT</script><style>NEVER_STYLE</style><iframe>NEVER_FRAME</iframe><img src='https://example.invalid/image' alt='NEVER_IMAGE'>")
	stored, _ := s.store.GetArticleByThreadID(t.Context(), id)
	stored.Summary = "NEVER_SUMMARY"
	stored.CoverURL = "https://example.invalid/cover"
	stored.ContentJSON = json.RawMessage(`{"never":"NEVER_JSON"}`)
	stored.SanitizedHTML = "NEVER_CACHE"
	stored.RenderHTML = "NEVER_RENDER"
	if e := s.store.UpdateArticle(t.Context(), stored); e != nil {
		t.Fatal(e)
	}
	c, e := s.PublicTextSources().ReadPublicTextSource(t.Context(), id)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(c.Text, "正文末尾") || !strings.Contains(c.Text, "链接文字") || !strings.Contains(c.Text, "Title & label") || len(c.Text) < 500 || c.ThreadID != id || c.ArticleID != stored.ID || len(c.Revision) != 64 {
		t.Fatalf("wrong full-text source %+v", c)
	}
	for _, bad := range []string{"NEVER_", "https://", "<p>", "<script>", "created_by", "attachments"} {
		wire, _ := json.Marshal(c)
		if strings.Contains(string(wire), bad) {
			t.Fatal("source contains excluded material", bad)
		}
	}
	thread, e := s.community.GetThread(t.Context(), id)
	if e != nil || thread.ViewCount != 0 {
		t.Fatal("source read changed view count", e)
	}
}
func TestPublicArticleTextCanonicalAnonymousState(t *testing.T) {
	for _, pub := range []domain.PublicationStatus{"published", "private", "draft"} {
		for _, mod := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
			for _, del := range []domain.DeletionStatus{"active", "trashed", "purged"} {
				t.Run(string(pub)+"/"+string(mod)+"/"+string(del), func(t *testing.T) {
					s, repo := newTestService(true)
					a, e := s.CreateDraft(t.Context(), "1001", "Alice", SaveArticleRequest{Title: "State source", CategoryID: "1", ContentHTML: "<p>source</p>"})
					if e != nil {
						t.Fatal(e)
					}
					row, _ := repo.GetByID(t.Context(), a.ThreadID)
					row.PublicationStatus = pub
					row.ModerationStatus = mod
					row.DeletionStatus = del
					if e = repo.Update(t.Context(), row); e != nil {
						t.Fatal(e)
					}
					// A carried owner proof must never turn this public-only Port into
					// the private author reader used by the existing edit UI.
					ctx := identityport.WithSessionPrincipalResolver(t.Context(), richReadProof("1001"))
					c, e := s.PublicTextSources().ReadPublicTextSource(ctx, a.ThreadID)
					if pub == "published" && mod == "clear" && del == "active" {
						if e != nil || c.Text != "source" {
							t.Fatal(e)
						}
					} else {
						deniedText(t, c, e, ErrPublicTextUnavailable)
					}
				})
			}
		}
	}
}
func TestPublicArticleTextChangesDuringRead(t *testing.T) {
	for _, mode := range []string{"body", "body_aba", "private", "disable", "missing_epoch", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, id := textSourceFixture(t, "<p>Original source</p>")
			store := s.store
			calls := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.store = &richReadStore{Store: store, afterGet: func(_ int) {
				calls++
				if calls != 1 {
					return
				}
				switch mode {
				case "body", "body_aba":
					a, _ := store.GetArticleByThreadID(ctx, id)
					old := a.ContentHTML
					a.ContentHTML = "<p>different</p>"
					if e := store.UpdateArticle(ctx, a); e != nil {
						t.Fatal(e)
					}
					if mode == "body_aba" {
						a.ContentHTML = old
						if e := store.UpdateArticle(ctx, a); e != nil {
							t.Fatal(e)
						}
					}
				case "private":
					thread, _ := s.community.GetThread(ctx, id)
					thread.PublicationStatus = "private"
					if _, e := s.community.SaveFeatureThread(ctx, thread, "1001", "richtext_offline"); e != nil {
						t.Fatal(e)
					}
				case "disable":
					s.SetEnabledChecker(func() bool { return false })
				case "missing_epoch":
					s.store = &sourceMissingEpochStore{Store: store}
				case "cancel":
					cancel()
				}
			}}
			c, e := s.PublicTextSources().ReadPublicTextSource(ctx, id)
			want := ErrPublicTextChanged
			if mode == "private" || mode == "disable" || mode == "missing_epoch" {
				want = ErrPublicTextUnavailable
			}
			if mode == "cancel" {
				want = context.Canceled
			}
			deniedText(t, c, e, want)
		})
	}
}

type sourceMissingEpochStore struct{ Store }

func (s *sourceMissingEpochStore) GetArticleByThreadID(ctx context.Context, id string) (*Article, error) {
	a, e := s.Store.GetArticleByThreadID(ctx, id)
	if a != nil {
		a.FactsVersion = 0
	}
	return a, e
}
func TestPublicArticleTextBudgetsAndInvalidSource(t *testing.T) {
	for _, tc := range []struct {
		name, html string
		want       error
	}{
		{"text_at_limit", "<p>" + strings.Repeat("x", MaxPublicTextBytes) + "</p>", nil},
		{"text_over_limit", "<p>" + strings.Repeat("x", MaxPublicTextBytes+1) + "</p>", ErrPublicTextTooLarge},
		{"raw_html_limit", strings.Repeat("x", MaxHTMLBytes+1), ErrPublicTextTooLarge},
		{"no_visible_text", "<script>not a source</script>", ErrPublicTextUnavailable},
		{"deep_html", strings.Repeat("<div>", MaxDepth+1) + "text" + strings.Repeat("</div>", MaxDepth+1), ErrPublicTextUnavailable},
		{"node_budget", strings.Repeat("<p>x</p>", MaxNodes+1), ErrPublicTextUnavailable},
		{"invalid_utf8", string([]byte{0xff}), ErrPublicTextUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, id := textSourceFixture(t, "<p>valid</p>")
			a, _ := s.store.GetArticleByThreadID(t.Context(), id)
			a.ContentHTML = tc.html
			if e := s.store.UpdateArticle(t.Context(), a); e != nil {
				t.Fatal(e)
			}
			c, e := s.PublicTextSources().ReadPublicTextSource(t.Context(), id)
			if tc.want == nil {
				if e != nil || len(c.Text) != MaxPublicTextBytes {
					t.Fatal(e)
				}
			} else {
				deniedText(t, c, e, tc.want)
			}
		})
	}
	s, id := textSourceFixture(t, "<p>valid</p>")
	s.store = &sourceMissingEpochStore{Store: s.store}
	c, e := s.PublicTextSources().ReadPublicTextSource(t.Context(), id)
	deniedText(t, c, e, ErrPublicTextUnavailable)
	for _, id := range []string{"", "01", "-1", "+1", "1/../2", "9223372036854775808"} {
		c, e := s.PublicTextSources().ReadPublicTextSource(t.Context(), id)
		deniedText(t, c, e, ErrPublicTextUnavailable)
	}
	var absent *Service
	c, e = absent.PublicTextSources().ReadPublicTextSource(t.Context(), "1")
	deniedText(t, c, e, ErrPublicTextUnavailable)
}
func TestPublicArticleTextConcurrentReadOnly(t *testing.T) {
	s, id := textSourceFixture(t, "<p>source</p>")
	a, _ := s.store.GetArticleByThreadID(t.Context(), id)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := s.PublicTextSources().ReadPublicTextSource(t.Context(), id)
			if e != nil || c.Text != "source" {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	after, _ := s.store.GetArticleByThreadID(t.Context(), id)
	if a.FactsVersion != after.FactsVersion {
		t.Fatal("reader changed article epoch")
	}
	thread, _ := s.community.GetThread(t.Context(), id)
	if thread.ViewCount != 0 {
		t.Fatal("reader changed views")
	}
}
