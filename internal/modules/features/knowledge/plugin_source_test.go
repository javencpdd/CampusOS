package knowledge

import (
	"context"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

type pluginSourceFunc func(context.Context, identity.SessionPrincipalResolver, PluginSourceRequest, func(context.Context) (string, error)) (PluginSourceResult, error)

func (f pluginSourceFunc) ListPublicPlugin(c context.Context, r identity.SessionPrincipalResolver, q PluginSourceRequest, g func(context.Context) (string, error)) (PluginSourceResult, error) {
	return f(c, r, q, g)
}
func TestPluginSourceValidatesPrimaryBytes(t *testing.T) {
	for _, mode := range []string{"ok", "text", "title", "url", "revision", "duplicate", "cursor", "feature"} {
		t.Run(mode, func(t *testing.T) {
			f, _, _, c := retrievalFixture(t, 1)
			page, e := f.s.Sources(t.Context(), c.ID, 1, 20)
			if e != nil || len(page.Items) == 0 {
				t.Fatal(e)
			}
			src := page.Items[0]
			adapter := pluginSourceFunc(func(ctx context.Context, _ identity.SessionPrincipalResolver, _ PluginSourceRequest, g func(context.Context) (string, error)) (PluginSourceResult, error) {
				if _, e := g(ctx); e != nil {
					return PluginSourceResult{}, e
				}
				item := PluginSourceItem{ExternalID: src.MemberID, Revision: src.Revision, Title: src.Source.Title, Text: src.Source.Content}
				out := PluginSourceResult{}
				switch mode {
				case "text":
					item.Text = "PRIVATE_FAKE"
				case "title":
					item.Title = "forged"
				case "url":
					item.SourceURL = "https://attacker.test/"
				case "revision":
					item.Revision = "old"
				case "cursor":
					out.NextCursor = "arbitrary"
				case "feature":
					f.enabled = false
				}
				out.Items = []PluginSourceItem{item}
				if mode == "duplicate" {
					out.Items = append(out.Items, item)
				}
				return out, nil
			})
			svc := newPluginSourceService(f.s, adapter)
			defer svc.stop()
			q := PluginSourceRequest{Publisher: "campusos", Key: "source", Version: "1.0.0", ContributionID: "source-main", RequestID: "q", CollectionID: c.ID, Query: "公开文本", Page: 1, Limit: 20, TimeoutMS: 1000}
			out, e := svc.List(t.Context(), privateTestResolver, q)
			if mode == "ok" {
				if e != nil || len(out.Items) != 1 || out.Items[0] != src {
					t.Fatal(out, e)
				}
			} else if e == nil || len(out.Items) != 0 {
				t.Fatal("untrusted source released", mode, out, e)
			}
		})
	}
}
func TestPublicRAGExternalGrantJoinsModelGuard(t *testing.T) {
	_, s, m, q := ragFixture(t)
	calls := 0
	guard := func(context.Context) error {
		calls++
		if calls >= 3 {
			return ErrRAGDenied
		}
		return nil
	}
	if out, e := s.AnswerChecked(t.Context(), privateTestResolver, q, guard); e == nil || out.Answer != "" {
		t.Fatal(out, e)
	}
	if m.calls > 1 {
		t.Fatal("external denial replayed model")
	}
}
