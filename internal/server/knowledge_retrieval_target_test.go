package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A normal production graph supplies both resident indexing and the typed
// retrieval Port. No fake Provider, manual page or ProcessOnce is used here.
func TestKnowledgeRetrievalPublicLexicalPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_RETRIEVAL_DSN")
	if dsn == "" {
		t.Skip("requires owned10 lexical PostgreSQL")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_10_lexical" {
		t.Fatal("refuse non-owned10 lexical database")
	}
	root := os.Getenv("CAMPUSOS_KNOWLEDGE_RETRIEVAL_ROOT")
	if !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-10-lexical/") {
		t.Fatal("owned lexical root required")
	}
	t.Chdir(root)
	ctx := t.Context()
	pool, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	scalar := func(sql string, args ...any) int64 {
		t.Helper()
		var n int64
		if e := pool.QueryRow(ctx, sql, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	text := func(sql string, args ...any) string {
		t.Helper()
		var n string
		if e := pool.QueryRow(ctx, sql, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	wait := func(name string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(40 * time.Second)
		for time.Now().Before(deadline) {
			if predicate() {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(25 * time.Millisecond):
			}
		}
		t.Fatal("retrieval target deadline: " + name)
	}
	checks := []string{}
	check := func(name string) { checks = append(checks, name) }
	exec(`INSERT INTO users(id,username,nickname,email) VALUES(430101,'owned10-user','Lexical owner','owned10-user@example.test');
 INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(430110,'10 group','owned10-group','group','active');
 INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(430111,'10 board','owned10-board',430110,'board','active'),(430112,'10 moved','owned10-moved',430110,'board','active');
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(430111,'discussion',true),(430111,'article',true),(430112,'discussion',true);
 INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status)
 SELECT 430200+n,'校园资料 '||n,'校园公开检索 中文资料 编号 '||n,'markdown',430101,'Lexical owner',430111,'published','discussion','published','clear','active' FROM generate_series(1,65) n;
 UPDATE threads SET content=repeat('中',1021)||'分块边界关键词'||repeat('文',400) WHERE id=430265;
 INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status) VALUES
 (430290,'图书馆文章','EXCERPT_NOT_SEARCHABLE','safe_html',430101,'Lexical owner',430111,'published','article','published','clear','active'),
 (430291,'PRIVATE_RETRIEVAL_TITLE','PRIVATE_RETRIEVAL_BODY','markdown',430101,'Lexical owner',430111,'private','discussion','private','clear','active');
 INSERT INTO richtext_article_contents(id,thread_id,title,content_html,status,created_by) VALUES(430390,430290,'图书馆文章','<p>图书馆 开放 时间 <strong>周末</strong></p>','published',430101);`)
	cfg := config.Load()
	cfg.Database.DSN, cfg.Plugin.DataDir, cfg.Redis.Enabled = dsn, filepath.Join(root, "data/plugin_data"), false
	var server *Server
	var infra *infrastructureBootstrap
	start := func() (*Server, *infrastructureBootstrap) {
		t.Helper()
		s := New(cfg)
		i, e := s.startInfrastructure()
		if e != nil || i.database == nil || s.knowledge == nil || s.knowledge.Retrieval() == nil {
			t.Fatal("normal retrieval module composition required", e)
		}
		return s, i
	}
	server, infra = start()
	defer func() {
		if infra != nil {
			infra.Stop()
		}
	}()
	if _, err = server.knowledge.Retrieval().SearchPublic(ctx, knowledge.PublicSearchRequest{CollectionID: "kc_" + strings.Repeat("0", 32), Query: "公开", Page: 1, PageSize: 10}); !errors.Is(err, knowledge.ErrDisabled) {
		t.Fatal("default-off", err)
	}
	if _, err = server.features.Request(knowledge.FeatureID, true); err != nil {
		t.Fatal(err)
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account := "adm.knowledge.retrieval"
	if err = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "retrieval-native", Email: "retrieval-native@example.test", DisplayName: "10 native"}); err != nil {
		t.Fatal(err)
	}
	if err = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.knowledge.retrieval", AccountID: account, RoleID: "security_admin", GrantedBy: account}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sid := "as.knowledge.retrieval"
	if err = identityrepo.NewPgAdminSessionRepository(pool).CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: account, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, server.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: account, SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	c, err := server.knowledge.Collections().SaveCollection(ctx, knowledge.Collection{Title: "公开检索集合", Status: "active"}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	state, err := server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "board", SourceID: "430111", Active: true}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	c = state.Collection
	state, err = server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "thread", SourceID: "430201", Active: true}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	c = state.Collection
	official, err := server.knowledge.Collections().SaveOfficial(ctx, knowledge.Official{Title: "图书馆 开放", Content: "图书馆开放时间：周末九点", Format: "text", Status: "published"}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	state, err = server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "official", SourceID: official.ID, Active: true}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	c = state.Collection
	ready := func(id string) bool {
		return scalar(`SELECT count(*) FROM knowledge_index_jobs j JOIN knowledge_collections c ON c.id=j.id WHERE j.id=$1 AND j.state='idle' AND j.data->>'run_generation'='0' AND (j.data->>'ready_generation')::bigint>0 AND j.data->>'collection_version'=c.facts_version::text AND j.data->>'requested'=j.data->>'active_request' AND j.available_at_ms>$2`, id, time.Now().Add(time.Second).UnixMilli()) == 1
	}
	wait("normal resident indexing", func() bool {
		return ready(c.ID) && scalar(`SELECT count(*) FROM knowledge_index_documents WHERE collection_id=$1`, c.ID) == 68
	})
	if scalar(`SELECT count(*) FROM plugin_installations`)+scalar(`SELECT count(*) FROM plugin_user_installations`) != 0 {
		t.Fatal("external plugin unexpectedly required")
	}
	check("default-off and zero external installations; normal Core worker/indexer prepares public discussion RichText official documents with overlapping rules")
	timings := []map[string]any{}
	search := func(query string, page, size int) knowledge.PublicSearchPage {
		t.Helper()
		begin := time.Now()
		out, e := server.knowledge.Retrieval().SearchPublic(ctx, knowledge.PublicSearchRequest{CollectionID: c.ID, Query: query, Page: page, PageSize: size})
		if e != nil {
			t.Fatal(query, e)
		}
		timings = append(timings, map[string]any{"query": query, "page": page, "page_size": size, "total": out.Total, "milliseconds": float64(time.Since(begin).Microseconds()) / 1000})
		return out
	}
	verify := func(hit knowledge.PublicSearchHit) {
		t.Helper()
		source, e := server.knowledge.Collections().Source(ctx, c.ID, hit.Citation.MemberID, hit.Citation.VisibilityRevision)
		if e != nil || source.Source.Reference != hit.Citation.Source || source.Source.URL != hit.Citation.URL || source.Source.Title != hit.Title || !strings.Contains(source.Source.Content, hit.Snippet) {
			t.Fatal("citation does not open current origin", e)
		}
	}
	seen := map[string]bool{}
	for page := 1; page <= 10; page++ {
		out := search("校园公开检索", page, 7)
		if out.Total != 64 {
			t.Fatal("source dedup/count", out.Total)
		}
		for _, hit := range out.Items {
			if seen[hit.Citation.Source.ID] {
				t.Fatal("duplicate source")
			}
			seen[hit.Citation.Source.ID] = true
			verify(hit)
		}
	}
	if len(seen) != 64 {
		t.Fatal("missing pages")
	}
	check("paged source dedup and totals across more than two pages; every citation reopens current Community origin with its exact visibility revision")
	result := search("图书馆 开放", 1, 10)
	if result.Total != 2 || result.Items[0].Citation.Source.Kind != knowledge.OfficialKind || result.Items[0].Score != 10 || result.Items[1].Citation.Source.Kind != knowledge.PublicArticleKind {
		t.Fatal("explainable ranking", result)
	}
	for _, hit := range result.Items {
		verify(hit)
		if strings.Contains(hit.Snippet, "EXCERPT_NOT_SEARCHABLE") {
			t.Fatal("indexed excerpt")
		}
	}
	if out := search("分块边界关键词", 1, 10); out.Total != 1 || !strings.Contains(out.Items[0].Snippet, "分块边界关键词") {
		t.Fatal("cross-chunk Unicode keyword", out)
	}
	if search("PRIVATE_RETRIEVAL_BODY", 1, 10).Total != 0 || search("EXCERPT_NOT_SEARCHABLE", 1, 10).Total != 0 || search("不存在的词语", 1, 10).Total != 0 {
		t.Fatal("private/excerpt/empty literal results")
	}
	if search("校园公开检索 OR 1", 1, 10).Total != 0 {
		t.Fatal("query was treated as SQL/wildcard")
	}
	check("literal Chinese/Latin AND, title4/body1 score, current full RichText and official sources, Unicode boundary snippet and negative smoke queries")
	// Keep stale bytes physically present. No event or cleanup is required for
	// immediate rejection; searches must not mutate or repair their own index.
	docsBefore := text(`SELECT jsonb_agg(to_jsonb(d) ORDER BY id)::text FROM knowledge_index_documents d`)
	exec(`UPDATE threads SET publication_status='private',status='private' WHERE id=430201`)
	out := search("校园公开检索", 1, 50)
	if out.Total != 63 {
		t.Fatal("stale private count", out.Total)
	}
	for _, hit := range out.Items {
		if hit.Citation.Source.ID == "430201" {
			t.Fatal("private stale snippet")
		}
	}
	if docsBefore != text(`SELECT jsonb_agg(to_jsonb(d) ORDER BY id)::text FROM knowledge_index_documents d`) {
		t.Fatal("query mutated stale index")
	}
	exec(`UPDATE threads SET category_id=430112 WHERE id=430202`)
	if search("校园公开检索", 1, 50).Total != 62 {
		t.Fatal("moved source count")
	}
	check("stale indexed private and moved sources are filtered before ranking/count/pagination while physical projections remain unchanged")
	exec(`UPDATE richtext_article_contents SET content_html='<p>已修订的新文章</p>' WHERE id=430390`)
	if search("图书馆 开放", 1, 10).Total != 1 {
		t.Fatal("RichText-only changed revision returned old snippet")
	}
	exec(`UPDATE knowledge_official_sources SET data=jsonb_set(data,'{status}','"withdrawn"') WHERE id=$1`, official.ID)
	if search("图书馆 开放", 1, 10).Total != 0 {
		t.Fatal("withdrawn official snippet")
	}
	check("RichText-only current epoch and official withdrawal invalidate old snippets without waiting for asynchronous reindex")
	other, err := server.knowledge.Collections().SaveCollection(ctx, knowledge.Collection{Title: "另一公开集合", Status: "active"}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	otherOfficial, err := server.knowledge.Collections().SaveOfficial(ctx, knowledge.Official{Title: "Only other", Content: "OTHER_COLLECTION_MARKER", Format: "text", Status: "published"}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.knowledge.Collections().SaveSelection(ctx, other.ID, other.Version, knowledge.Selection{Kind: "official", SourceID: otherOfficial.ID, Active: true}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	wait("second collection and first rebuild", func() bool { return ready(other.ID) && ready(c.ID) })
	if search("OTHER_COLLECTION_MARKER", 1, 10).Total != 0 {
		t.Fatal("collection boundary expanded")
	}
	check("requested collection scope does not inherit another public collection or any Admin authority")
	// Source/rule/Feature lifecycle checks use actual owning service commands.
	var board knowledge.Selection
	for _, r := range state.Selections {
		if r.Kind == "board" {
			board = r
		}
	}
	board.Active = false
	state, err = server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, board, resolve)
	if err != nil {
		t.Fatal(err)
	}
	c = state.Collection
	if page, e := server.knowledge.Retrieval().SearchPublic(ctx, knowledge.PublicSearchRequest{CollectionID: c.ID, Query: "校园公开检索", Page: 1, PageSize: 10}); e == nil && page.Total != 0 {
		t.Fatal("old rule generation returned")
	}
	wait("disabled rule rebuilt", func() bool { return ready(c.ID) })
	if search("校园公开检索", 1, 10).Total != 0 {
		t.Fatal("inactive board rule search")
	}
	if _, err = server.features.Request(knowledge.FeatureID, false); err != nil {
		t.Fatal(err)
	}
	if page, e := server.knowledge.Retrieval().SearchPublic(ctx, knowledge.PublicSearchRequest{CollectionID: c.ID, Query: "公开", Page: 1, PageSize: 10}); !errors.Is(e, knowledge.ErrDisabled) || page.Total != 0 {
		t.Fatal("Feature off", e)
	}
	oldPort := server.knowledge.Retrieval()
	infra.Stop()
	infra = nil
	if page, e := oldPort.SearchPublic(ctx, knowledge.PublicSearchRequest{CollectionID: c.ID, Query: "公开", Page: 1, PageSize: 10}); !errors.Is(e, knowledge.ErrIndexConsumerStopped) || page.Total != 0 {
		t.Fatal("stopped port", e)
	}
	server, infra = start()
	if _, err = server.features.Request(knowledge.FeatureID, true); err != nil {
		t.Fatal(err)
	}
	// Advance only the owned idle deadline; preserve every cursor/generation.
	exec(`UPDATE knowledge_index_jobs SET data=jsonb_set(data,'{available_at_ms}','"0"') WHERE state='idle'`)
	wait("new production graph ready", func() bool { return ready(c.ID) })
	if search("校园公开检索", 1, 10).Total != 0 {
		t.Fatal("restart resurrected old rule")
	}
	check("owning rule/Feature disable, stopped Port rejection and full infrastructure restart preserve current visibility")
	infra.Stop()
	infra = nil
	proof := map[string]any{"status": "passed", "actual_server_composition": true, "actual_postgresql": true, "normal_resident_indexer": true, "normal_resident_outbox": true, "fake_provider_or_manual_page": false, "http_or_browser": false, "external_installations": 0, "checks": checks, "check_count": len(checks), "full_infrastructure_restarts": 1, "initial_index_documents": 68, "initial_unique_keyword_sources": 64, "query_timings": timings, "quality_scope": "literal keyword smoke only, not full Chinese question evaluation or capacity acceptance"}
	wire, _ := json.MarshalIndent(proof, "", "  ")
	if err = os.WriteFile(filepath.Join(root, "target-proof.json"), append(wire, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
