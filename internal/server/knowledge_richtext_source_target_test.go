package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Native production-module Port target, not a test HTTP route or browser test.
func TestKnowledgeRichTextSourceProductionPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_RICHTEXT_DSN")
	if dsn == "" {
		t.Skip("requires owned09a RichText PostgreSQL target")
	}
	parsed, e := pgxpool.ParseConfig(dsn)
	if e != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_09a_richtext_source" {
		t.Fatal("refuse non-owned RichText target database")
	}
	root := os.Getenv("CAMPUSOS_KNOWLEDGE_RICHTEXT_ROOT")
	if !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-09a-richtext-source/") {
		t.Fatal("requires owned09a RichText target root")
	}
	t.Chdir(root)
	cfg := config.Load()
	cfg.Database.DSN = dsn
	cfg.Plugin.DataDir = filepath.Join(root, "data", "plugin_data")
	cfg.Redis.Enabled = false
	var infra *infrastructureBootstrap
	var s *Server
	var pool *pgxpool.Pool
	var articles knowledge.PublicArticleCandidates
	var plain knowledge.PublicSourceCandidates
	var original richtext.PublicTextSourceReader
	start := func() {
		t.Helper()
		s = New(cfg)
		var err error
		infra, err = s.startInfrastructure()
		if err != nil {
			t.Fatal(err)
		}
		pool = infra.database
		if pool == nil {
			t.Fatal("requires actual PostgreSQL")
		}
		v, ok := s.appContext.Lookup(knowledge.PortPublicArticleCandidates)
		var typed bool
		articles, typed = v.(knowledge.PublicArticleCandidates)
		if !ok || !typed {
			t.Fatal("production article candidates missing")
		}
		v, ok = s.appContext.Lookup(knowledge.PortPublicSourceCandidates)
		plain, typed = v.(knowledge.PublicSourceCandidates)
		if !ok || !typed {
			t.Fatal("production Community candidates missing")
		}
		v, ok = s.appContext.Lookup(richtext.PortPublicTextSource)
		original, typed = v.(richtext.PublicTextSourceReader)
		if !ok || !typed {
			t.Fatal("owning RichText text source missing")
		}
	}
	start()
	defer func() {
		if infra != nil {
			infra.Stop()
		}
	}()
	restart := func() { infra.Stop(); infra = nil; start() }
	ctx := t.Context()
	checks := []string{}
	check := func(name string) { checks = append(checks, name) }
	deny := func(c knowledge.PublicArticleCandidate, err, want error) {
		t.Helper()
		if c != (knowledge.PublicArticleCandidate{}) || !errors.Is(err, want) {
			t.Fatalf("denial released article or wrong error: %v want %v", err, want)
		}
	}
	c, e := articles.PreviewPublicArticle(ctx, "400201")
	deny(c, e, knowledge.ErrDisabled)
	check("Knowledge defaults off for article candidates")
	if _, e = s.features.Request(knowledge.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO users(id,username,nickname,email) VALUES(400101,'owned09a-rich-alice','Alice','owned09a-rich-alice@example.test'),(400102,'owned09a-rich-bob','Bob','owned09a-rich-bob@example.test');
INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(400110,'Owned Rich group','owned09a-rich-group','group','active');
INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(400111,'Owned Rich board','owned09a-rich-board',400110,'board','active'),(400112,'Owned Rich moved','owned09a-rich-moved',400110,'board','active');
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(400111,'article',TRUE),(400112,'article',TRUE),(400111,'discussion',TRUE);
INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status) VALUES
 (400201,'Rich Alice','COMMUNITY_EXCERPT_ONLY','safe_html',400101,'Alice',400111,'published','article','published','clear','active'),
 (400202,'Rich Bob','COMMUNITY_EXCERPT_ONLY','safe_html',400102,'Bob',400111,'published','article','published','clear','active'),
 (400203,'PRIVATE_RICH_ALICE','PRIVATE_RICH_EXCERPT','safe_html',400101,'Alice',400111,'private','article','private','clear','active'),
 (400204,'PRIVATE_RICH_BOB','PRIVATE_RICH_EXCERPT','safe_html',400102,'Bob',400111,'private','article','private','clear','active'),
 (400205,'Plain regression','PLAIN_CURRENT_BODY','markdown',400101,'Alice',400111,'published','discussion','published','clear','active');`)
	html := "<h1>公开 &amp; 正文</h1><p>" + strings.Repeat("完整正文 ", 160) + "FULL_BODY_TAIL</p><a href='https://example.invalid/no-fetch'>LINK_LABEL</a><script>NO_SCRIPT</script><style>NO_STYLE</style><iframe>NO_FRAME</iframe><img src='https://example.invalid/image' alt='NO_IMAGE'>"
	exec(`INSERT INTO richtext_article_contents(id,thread_id,title,summary,cover_url,content_html,content_json,sanitized_html,status,created_by)
SELECT id+100,id,title,'NO_SUMMARY','https://example.invalid/cover',CASE WHEN publication_status='private' THEN '<p>PRIVATE_RICH_BODY</p>' ELSE $1 END,'{"no":"NO_JSON"}'::jsonb,'NO_STORED_RENDER','published',author_id FROM threads WHERE thread_type='article'`, html)
	preview := func() knowledge.PublicArticleCandidate {
		t.Helper()
		c, e := articles.PreviewPublicArticle(ctx, "400201")
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	for _, id := range []string{"400201", "400202"} {
		c, e := articles.PreviewPublicArticle(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		if c.Reference.Kind != knowledge.PublicArticleKind || c.Reference.ID != id || c.Format != "text" || c.URL != "/threads/"+id || c.CategoryID != "400111" || len(c.Content) < 500 || !strings.Contains(c.Content, "FULL_BODY_TAIL") || !strings.Contains(c.Content, "LINK_LABEL") {
			t.Fatal("source is not complete owning-module text")
		}
		wire, _ := json.Marshal(c)
		for _, bad := range []string{"COMMUNITY_EXCERPT", "NO_", "https://", "created_by", "content_json", "attachments", "<p>"} {
			if strings.Contains(string(wire), bad) {
				t.Fatal("source projection leaked excluded material", bad)
			}
		}
		if current, e := articles.RecheckPublicArticle(ctx, c.Reference); e != nil || current != c {
			t.Fatal("current article reference failed", e)
		}
	}
	check("two owners full RichText bodies rather than Community excerpts")
	check("plain text strips script style frame HTML URLs caches JSON cover and asset metadata")
	for _, id := range []string{"400203", "400204", "400299"} {
		c, e := articles.PreviewPublicArticle(ctx, id)
		deny(c, e, knowledge.ErrSourceUnavailable)
	}
	check("two owners private and missing article deny without metadata")
	carried := identityport.WithSessionPrincipalResolver(ctx, func(context.Context) (identityport.PrincipalContext, error) {
		t.Error("public source tried a carried owner proof")
		return identityport.PrincipalContext{}, errors.New("owner proof forbidden")
	})
	if c, e := original.ReadPublicTextSource(carried, "400203"); e == nil || c != (richtext.PublicTextSource{}) {
		t.Fatal("ambient owner proof opened private article")
	}
	check("owning source stays anonymous under carried session context")
	if _, e = plain.PreviewPublicThread(ctx, "400205"); e != nil {
		t.Fatal(e)
	}
	c, e = plain.PreviewPublicThread(ctx, "400201")
	deny(c, e, knowledge.ErrUnsupportedSource)
	c, e = articles.PreviewPublicArticle(ctx, "400205")
	deny(c, e, knowledge.ErrSourceUnavailable)
	ref := preview().Reference
	ref.Kind = knowledge.PublicThreadKind
	c, e = articles.RecheckPublicArticle(ctx, ref)
	deny(c, e, knowledge.ErrInvalidReference)
	check("Community and RichText source kinds cannot substitute or fall back to excerpt")
	snapshot := func() string {
		t.Helper()
		var out string
		if e := pool.QueryRow(ctx, `SELECT jsonb_build_object('threads',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM threads t),'articles',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM richtext_article_contents a))::text`).Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	before := snapshot()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := articles.PreviewPublicArticle(ctx, "400201"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if snapshot() != before {
		t.Fatal("source reads changed original article/thread bytes or counters")
	}
	check("32 concurrent production reads preserve every original row byte and counter")
	var coreEpochBefore, coreEpochAfter int64
	if e = pool.QueryRow(ctx, "SELECT facts_version FROM threads WHERE id=400201").Scan(&coreEpochBefore); e != nil {
		t.Fatal(e)
	}
	old := preview()
	exec("UPDATE richtext_article_contents SET content_html='<p>EDITED_FULL_BODY</p>' WHERE thread_id=400201")
	c, e = articles.RecheckPublicArticle(ctx, old.Reference)
	deny(c, e, knowledge.ErrSourceChanged)
	if e = pool.QueryRow(ctx, "SELECT facts_version FROM threads WHERE id=400201").Scan(&coreEpochAfter); e != nil || coreEpochAfter != coreEpochBefore || preview().Content != "EDITED_FULL_BODY" {
		t.Fatal("article-only edit did not own its source epoch", e)
	}
	check("article body-only edit invalidates reference while Community epoch and excerpt stay unchanged")
	old = preview()
	exec("BEGIN; UPDATE richtext_article_contents SET content_html='<p>temporary body</p>' WHERE thread_id=400201; UPDATE richtext_article_contents SET content_html='<p>EDITED_FULL_BODY</p>' WHERE thread_id=400201; COMMIT")
	c, e = articles.RecheckPublicArticle(ctx, old.Reference)
	deny(c, e, knowledge.ErrSourceChanged)
	if preview().Content != old.Content {
		t.Fatal("body ABA fixture changed final text")
	}
	check("same transaction article body ABA never revives a reference")
	old = preview()
	exec("BEGIN; UPDATE threads SET publication_status='private' WHERE id=400201; UPDATE threads SET publication_status='published' WHERE id=400201; COMMIT")
	c, e = articles.RecheckPublicArticle(ctx, old.Reference)
	deny(c, e, knowledge.ErrSourceChanged)
	check("Community public private public ABA invalidates article reference")
	old = preview()
	exec("BEGIN; UPDATE threads SET title='Rich renamed' WHERE id=400201; UPDATE richtext_article_contents SET title='Rich renamed' WHERE thread_id=400201; COMMIT")
	c, e = articles.RecheckPublicArticle(ctx, old.Reference)
	deny(c, e, knowledge.ErrSourceChanged)
	check("coherent article and Community title edit invalidates reference")
	old = preview()
	exec("UPDATE threads SET category_id=400112 WHERE id=400201")
	c, e = articles.RecheckPublicArticle(ctx, old.Reference)
	deny(c, e, knowledge.ErrSourceChanged)
	if preview().CategoryID != "400112" {
		t.Fatal("moved source retained old board")
	}
	check("current Community board move invalidates article source")
	for _, tc := range []struct{ name, table, set, restore string }{
		{"private", "threads", "publication_status='private'", "publication_status='published'"},
		{"draft", "threads", "publication_status='draft'", "publication_status='published'"},
		{"pending", "threads", "moderation_status='pending'", "moderation_status='clear'"},
		{"taken_down", "threads", "moderation_status='taken_down'", "moderation_status='clear'"},
		{"rejected", "threads", "moderation_status='rejected'", "moderation_status='clear'"},
		{"trashed", "threads", "deletion_status='trashed'", "deletion_status='active'"},
		{"deleted_thread", "threads", "deleted_at=NOW()", "deleted_at=NULL"},
		{"deleted_article", "richtext_article_contents", "deleted_at=NOW()", "deleted_at=NULL"},
	} {
		old := preview()
		where := "id=400201"
		if tc.table != "threads" {
			where = "thread_id=400201"
		}
		exec("UPDATE " + tc.table + " SET " + tc.set + " WHERE " + where)
		c, e := articles.RecheckPublicArticle(ctx, old.Reference)
		deny(c, e, knowledge.ErrSourceUnavailable)
		exec("UPDATE " + tc.table + " SET " + tc.restore + " WHERE " + where)
		check(tc.name + " rejects old source without content")
	}
	for _, set := range []string{"created_by=400102", "title='MISMATCH_PRIVATE_TITLE'"} {
		exec("UPDATE richtext_article_contents SET " + set + " WHERE thread_id=400201")
		c, e := articles.PreviewPublicArticle(ctx, "400201")
		deny(c, e, knowledge.ErrSourceUnavailable)
		exec("UPDATE richtext_article_contents SET created_by=400101,title='Rich renamed' WHERE thread_id=400201")
	}
	check("article owner and title must match authoritative Community facts")
	for _, html := range []string{"<p>" + strings.Repeat("x", 65537) + "</p>", strings.Repeat("x", 100001)} {
		exec("UPDATE richtext_article_contents SET content_html=$1 WHERE thread_id=400201", html)
		c, e := articles.PreviewPublicArticle(ctx, "400201")
		deny(c, e, knowledge.ErrSourceTooLarge)
	}
	exec("UPDATE richtext_article_contents SET content_html='<p>RESTORED_FULL_BODY</p>' WHERE thread_id=400201")
	check("raw HTML and extracted text byte budgets fail explicitly")
	old = preview()
	exec("UPDATE threads SET view_count=view_count+1,updated_at=NOW() WHERE id=400201")
	exec("UPDATE richtext_article_contents SET updated_at=NOW() WHERE thread_id=400201")
	if current, e := articles.RecheckPublicArticle(ctx, old.Reference); e != nil || current != old {
		t.Fatal("counter or timestamp invalidated semantic source", e)
	}
	check("unrelated counters and wall clock do not change source reference")
	if _, e = s.features.Request(knowledge.FeatureID, false); e != nil {
		t.Fatal(e)
	}
	c, e = articles.RecheckPublicArticle(ctx, old.Reference)
	deny(c, e, knowledge.ErrDisabled)
	if _, e = s.features.Request(knowledge.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	check("Knowledge hot disable blocks held article reference")
	state, e := s.features.Request("controlled-richtext-article", false)
	if e != nil || !state.Enabled || !state.PendingRestart {
		t.Fatal("source Feature restart contract changed", e)
	}
	if _, e = articles.RecheckPublicArticle(ctx, old.Reference); e != nil {
		t.Fatal("staged source disable took effect before restart", e)
	}
	before = snapshot()
	restart()
	c, e = articles.RecheckPublicArticle(ctx, old.Reference)
	deny(c, e, knowledge.ErrSourceUnavailable)
	if _, e = plain.PreviewPublicThread(ctx, "400205"); e != nil {
		t.Fatal("RichText off disabled plain Community source", e)
	}
	check("actual restart applies RichText disable while Community source remains available")
	if _, e = s.features.Request("controlled-richtext-article", true); e != nil {
		t.Fatal(e)
	}
	c, e = articles.RecheckPublicArticle(ctx, old.Reference)
	deny(c, e, knowledge.ErrSourceUnavailable)
	restart()
	if _, e = articles.RecheckPublicArticle(ctx, old.Reference); e != nil {
		t.Fatal("current source did not recheck after explicit reenable restart", e)
	}
	if snapshot() != before {
		t.Fatal("source Feature restarts changed original rows")
	}
	check("explicit reenable restart rechecks current source and preserves original rows")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	c, e = articles.PreviewPublicArticle(cancelled, "400201")
	deny(c, e, context.Canceled)
	check("cancelled source read releases no candidate")
	proof := map[string]any{"status": "passed", "actual_server_composition": true, "actual_cmd_server_http": false, "postgres": true, "source_port_only": true, "source_feature_real_restarts": 2, "checks": checks, "check_count": len(checks)}
	wire, _ := json.MarshalIndent(proof, "", "  ")
	if e = os.WriteFile(filepath.Join(root, "target-proof.json"), append(wire, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	t.Logf("V12_KNOWLEDGE_RICHTEXT_SOURCE_PROOF %d actual production component checks passed", len(checks))
}
