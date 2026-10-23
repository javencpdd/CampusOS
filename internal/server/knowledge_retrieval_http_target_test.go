package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	platformroute "github.com/campusos/CampusOS/internal/platform/route"
	"github.com/campusos/CampusOS/internal/transport/httpapi"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This target uses the production module graph and production HTTP router over
// a real loopback socket. Indexing runs through the ordinary resident worker.
func TestKnowledgeRetrievalPublicHTTPPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_HTTP_DSN")
	if dsn == "" {
		t.Skip("requires owned10 HTTP PostgreSQL")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_10_http" {
		t.Fatal("refuse non-owned10 HTTP database")
	}
	root := os.Getenv("CAMPUSOS_KNOWLEDGE_HTTP_ROOT")
	if !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-10-http/") {
		t.Fatal("owned HTTP root required")
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
		var value string
		if e := pool.QueryRow(ctx, sql, args...).Scan(&value); e != nil {
			t.Fatal(e)
		}
		return value
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
		t.Fatal("HTTP target deadline: " + name)
	}
	exec(`INSERT INTO users(id,username,nickname,email) VALUES(440101,'owned10-http','HTTP owner','owned10-http@example.test');
 INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(440110,'HTTP group','owned10-http-group','group','active');
 INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(440111,'HTTP board','owned10-http-board',440110,'board','active');
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(440111,'discussion',true),(440111,'article',true);
 INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status)
 SELECT 440200+n,'校园检索资料 '||n,'HTTP校园公开中文资料 编号 '||n,'markdown',440101,'HTTP owner',440111,'published','discussion','published','clear','active' FROM generate_series(1,35) n;
 INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status) VALUES
 (440290,'图书馆文章','HTTP_EXCERPT_ONLY','safe_html',440101,'HTTP owner',440111,'published','article','published','clear','active'),
 (440291,'HTTP_PRIVATE_TITLE','HTTP_PRIVATE_BODY','markdown',440101,'HTTP owner',440111,'private','discussion','private','clear','active');
 INSERT INTO richtext_article_contents(id,thread_id,title,content_html,status,created_by) VALUES(440390,440290,'图书馆文章','<p>图书馆 开放 时间 <strong>周末</strong></p>','published',440101);`)
	cfg := config.Load()
	cfg.Database.DSN, cfg.Plugin.DataDir, cfg.Redis.Enabled = dsn, filepath.Join(root, "data/plugin_data"), false
	var server *Server
	var infra *infrastructureBootstrap
	var listener *httptest.Server
	checks := []string{}
	check := func(name string) { checks = append(checks, name) }
	start := func() (*Server, *infrastructureBootstrap, *httptest.Server) {
		t.Helper()
		s := New(cfg)
		i, e := s.startInfrastructure()
		if e != nil || i.database == nil || s.knowledge == nil || s.knowledge.Retrieval() == nil {
			t.Fatal("normal production composition required", e)
		}
		pluginHandler, e := s.pluginHTTPHandler()
		if e != nil {
			t.Fatal(e)
		}
		// The same Build call and real module handlers as runApplication; only
		// listener ownership differs so the test can choose an ephemeral port.
		router := httpapi.Build(httpapi.Dependencies{
			JWT: s.identity.JWTManager(), SessionVerifier: s.identity.Sessions(), Permissions: s.identity.Permissions(),
			AdminAccess: s.identity.AdminAccess(), AdminMFA: s.identity.MFA(), AdminJWT: s.identity.AdminDomainJWT(),
			AdminSessions: portadapt.NewAdminDomainSessionVerifier(s.identity.AdminDomainSessions()), AdminCodes: portadapt.NewAdminCodeChecker(s.identity.AdminDomainLifecycle()),
			Features: s.features, Feature: s.featureHandler, ModuleCatalog: s.moduleCatalog, PluginManager: s.manager,
			Identity: s.identity.Handlers(), Community: s.community.Handlers(), AcademicTerm: s.academicTerm.Handler(), UserStorage: s.storage.Handler(),
			Space: s.space.Handler(), Plugin: pluginHandler, PluginV5: s.pluginsV5Handler(), AI: s.ai.Handler(), Integration: s.integration.Handler(),
			Webhook: s.webhook.Handler(), MCP: s.mcp.Handler(), Message: s.message.Handler(), Homepage: s.appearance.HomepageHandler(), WebTheme: s.appearance.WebThemeHandler(),
			RichText: s.richtext.Handler(), Schedule: s.schedule.Handler(), PersonalDocuments: s.personalDocuments.Handler(), Knowledge: s.knowledge.Handler(),
			MutualAid: s.mutualAid.Handler(), Secondhand: s.secondhand.Handler(), PlatformLog: s.platformLog.Handler(), Moderation: s.moderation.Handler(),
			Reliability: s.reliability.Handler(), EmailDelivery: s.emailDelivery.Handler(), Metrics: i.metrics,
		})
		found := false
		for _, d := range router.RouteDescriptors() {
			if d.OperationCode == "http.knowledge.search.public" {
				if d.Method != http.MethodGet || d.Path != "/api/v1/knowledge/collections/:id/search" || d.Audience != platformroute.AudiencePublic || d.FeatureID != "knowledge" || d.PermissionCode != "" {
					t.Fatal("search route authority drift", d)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("production search route missing")
		}
		if e = s.identity.SyncRouteDescriptors(ctx, router.RouteDescriptors()); e != nil {
			t.Fatal("normal route catalog sync", e)
		}
		return s, i, httptest.NewServer(router)
	}
	server, infra, listener = start()
	defer func() {
		if listener != nil {
			listener.Close()
		}
		if infra != nil {
			infra.Stop()
		}
	}()
	type envelope struct {
		Code  int             `json:"code"`
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	httpRequests := 0
	timings := []map[string]any{}
	request := func(path, body string, want int, code string) (json.RawMessage, string) {
		t.Helper()
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, listener.URL+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		// Public routes ignore caller claimed roles; no credentials are supplied.
		req.Header.Set("X-Claimed-Role", "admin")
		client := &http.Client{Timeout: 8 * time.Second}
		begin := time.Now()
		res, e := client.Do(req)
		if e != nil {
			t.Fatal("actual HTTP request", e)
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if e != nil {
			t.Fatal(e)
		}
		httpRequests++
		timings = append(timings, map[string]any{"status": res.StatusCode, "milliseconds": float64(time.Since(begin).Microseconds()) / 1000})
		if res.StatusCode != want || !strings.Contains(res.Header.Get("Cache-Control"), "no-store") {
			t.Fatalf("HTTP status/cache path=%s got=%d cache=%q want=%d body=%s", path, res.StatusCode, res.Header.Get("Cache-Control"), want, raw)
		}
		var out envelope
		if json.Unmarshal(raw, &out) != nil {
			t.Fatal("invalid response envelope")
		}
		if want == http.StatusOK {
			if out.Code != 0 || out.Error != nil || len(out.Data) == 0 {
				t.Fatal("success envelope", string(raw))
			}
		} else if out.Error == nil || out.Error.Code != code || len(out.Data) > 0 {
			t.Fatal("error contract or data leak", string(raw))
		}
		for _, marker := range []string{"HTTP_PRIVATE_TITLE", "HTTP_PRIVATE_BODY", "HTTP_EXCERPT_ONLY"} {
			if strings.Contains(string(raw), marker) {
				t.Fatal("private or excerpt bytes leaked")
			}
		}
		return out.Data, string(raw)
	}
	path := func(id string) string { return "/api/v1/knowledge/collections/" + id }
	searchPath := func(id, query string, page, size int) string {
		return path(id) + "/search?query=" + url.QueryEscape(query) + "&page=" + strconv.Itoa(page) + "&page_size=" + strconv.Itoa(size)
	}
	missing := "kc_" + strings.Repeat("0", 32)
	request(searchPath(missing, "公开", 1, 5), "", 503, "knowledge.disabled")
	if _, err = server.features.Request(knowledge.FeatureID, true); err != nil {
		t.Fatal(err)
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account, sid := "adm.knowledge.http", "as.knowledge.http"
	if err = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "retrieval-http", Email: "retrieval-http@example.test", DisplayName: "HTTP native"}); err != nil {
		t.Fatal(err)
	}
	if err = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.knowledge.http", AccountID: account, RoleID: "security_admin", GrantedBy: account}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = identityrepo.NewPgAdminSessionRepository(pool).CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: account, RefreshTokenDigest: strings.Repeat("b", 64), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, server.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: account, SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	c, err := server.knowledge.Collections().SaveCollection(ctx, knowledge.Collection{Title: "HTTP公开检索集合", Status: "active"}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	state, err := server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "board", SourceID: "440111", Active: true}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	c = state.Collection
	state, err = server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "thread", SourceID: "440201", Active: true}, resolve)
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
	ready := func() bool {
		// An idle job whose schedule was advanced to zero is still due for a
		// normal resident cycle. Wait for that cycle to finish before probing
		// successful HTTP reads; the service correctly rejects a build in flight.
		return scalar(`SELECT count(*) FROM knowledge_index_jobs j JOIN knowledge_collections c ON c.id=j.id WHERE j.id=$1 AND j.state='idle' AND j.data->>'run_generation'='0' AND (j.data->>'ready_generation')::bigint>0 AND j.data->>'collection_version'=c.facts_version::text AND j.data->>'requested'=j.data->>'active_request' AND j.available_at_ms>$2`, c.ID, time.Now().Add(time.Second).UnixMilli()) == 1
	}
	wait("resident indexed all origins", func() bool {
		return ready() && scalar(`SELECT count(*) FROM knowledge_index_documents WHERE collection_id=$1`, c.ID) == 38
	})
	if scalar(`SELECT count(*) FROM plugin_installations`)+scalar(`SELECT count(*) FROM plugin_user_installations`) != 0 {
		t.Fatal("external plugin unexpectedly installed")
	}
	check("actual production router/catalog and Core worker; anonymous route, default-off/no-store, zero external installations")
	search := func(query string, page, size int) knowledge.PublicSearchPage {
		t.Helper()
		data, _ := request(searchPath(c.ID, query, page, size), "", 200, "")
		var out knowledge.PublicSearchPage
		if json.Unmarshal(data, &out) != nil || out.Provider != knowledge.BuiltinLexicalProvider || out.Generation < 1 || out.Page != page || out.PageSize != size {
			t.Fatal("invalid search response", string(data))
		}
		return out
	}
	citationPath := func(hit knowledge.PublicSearchHit) string {
		return path(hit.Citation.CollectionID) + "/sources/" + hit.Citation.MemberID + "?revision=" + url.QueryEscape(hit.Citation.VisibilityRevision)
	}
	verify := func(hit knowledge.PublicSearchHit) {
		t.Helper()
		if !utf8.ValidString(hit.Snippet) || utf8.RuneCountInString(hit.Snippet) > knowledge.SearchSnippetRunes ||
			hit.Score < 1 || hit.Score != 4*len(hit.Explanation.TitleTerms)+len(hit.Explanation.ChunkTerms) ||
			hit.Explanation.TitleWeight != 4 || hit.Explanation.ChunkWeight != 1 || len(hit.Explanation.Terms) == 0 ||
			len(hit.Explanation.Terms) > knowledge.MaxSearchTerms || hit.Citation.CollectionID != c.ID ||
			hit.Citation.Generation < 1 || hit.Citation.ChunkIndex < 0 || !strings.HasPrefix(hit.Citation.MemberID, "km_") ||
			len(hit.Citation.VisibilityRevision) != 64 {
			t.Fatal("HTTP hit/snippet/citation bounds or score explanation invalid", hit)
		}
		data, _ := request(citationPath(hit), "", 200, "")
		var source knowledge.VisibleSource
		if json.Unmarshal(data, &source) != nil || source.Source.Reference != hit.Citation.Source || source.Source.URL != hit.Citation.URL || source.Source.Title != hit.Title || source.Revision != hit.Citation.VisibilityRevision || !strings.Contains(source.Source.Content, hit.Snippet) {
			t.Fatal("HTTP citation does not open current source", string(data))
		}
	}
	seen := map[string]bool{}
	var oldPrivate knowledge.PublicSearchHit
	for page := 1; page <= 7; page++ {
		out := search("HTTP校园公开中文资料", page, 6)
		if out.Total != 35 {
			t.Fatal("dedup/total", out.Total)
		}
		for _, hit := range out.Items {
			if seen[hit.Citation.Source.ID] {
				t.Fatal("duplicate source")
			}
			seen[hit.Citation.Source.ID] = true
			verify(hit)
			if hit.Citation.Source.ID == "440201" {
				oldPrivate = hit
			}
		}
	}
	if len(seen) != 35 || oldPrivate.Citation.MemberID == "" {
		t.Fatal("missing source pages")
	}
	data, _ := request(path(c.ID)+"/search?query="+url.QueryEscape("HTTP校园公开中文资料"), "", 200, "")
	var defaults knowledge.PublicSearchPage
	if json.Unmarshal(data, &defaults) != nil || defaults.Page != 1 || defaults.PageSize != 20 || len(defaults.Items) != 20 || defaults.Total != 35 {
		t.Fatal("HTTP pagination defaults")
	}
	check("multi-page source dedup/count and canonical defaults; every returned citation reopens through actual source HTTP route")
	ranked := search("图书馆 开放", 1, 10)
	if ranked.Total != 2 || ranked.Items[0].Citation.Source.Kind != knowledge.OfficialKind || ranked.Items[0].Score != 10 || ranked.Items[0].Explanation.TitleWeight != 4 || ranked.Items[0].Explanation.ChunkWeight != 1 || ranked.Items[1].Citation.Source.Kind != knowledge.PublicArticleKind {
		t.Fatal("explainable HTTP ranking", ranked)
	}
	var oldArticle, oldOfficial knowledge.PublicSearchHit
	for _, hit := range ranked.Items {
		verify(hit)
		if hit.Citation.Source.Kind == knowledge.OfficialKind {
			oldOfficial = hit
		} else {
			oldArticle = hit
		}
	}
	if search("HTTP_PRIVATE_BODY", 1, 10).Total != 0 || search("HTTP_EXCERPT_ONLY", 1, 10).Total != 0 || search("不存在的词语", 1, 10).Total != 0 || search("HTTP校园公开中文资料 OR 1", 1, 10).Total != 0 {
		t.Fatal("nonliteral/private/empty search")
	}
	check("literal Chinese/Latin AND, bounded plain snippets, current RichText and official ranking explanations; private/excerpt bytes absent")
	invalidSearch := []string{"", "query=", "query=a&query=b", "query=a&unknown=x", "query=a&provider=http%3A%2F%2F127.0.0.1", "query=a&collection_id=" + c.ID, "query=a&page=01", "query=a&page=0", "query=a&page_size=51", "query=a&page_size=+1", "query=a&page=1&page=2", "query=%", "query=a;provider=b", "query=%FF", "query=%00", "query=" + strings.Repeat("a", 1025), "query=" + strings.Repeat("a", 3300)}
	for _, raw := range invalidSearch {
		request(path(c.ID)+"/search?"+raw, "", 400, "request.invalid")
	}
	request(searchPath(c.ID, "图书馆", 1, 5), `{"query":"override"}`, 400, "request.invalid")
	request(searchPath("bad-id", "图书馆", 1, 5), "", 400, "request.invalid")
	check("closed search query rejects unknown/duplicate/scope/provider overrides, malformed URL/UTF8/controls, noncanonical pagination, oversize and GET body")
	citationBase := path(c.ID) + "/sources/" + oldOfficial.Citation.MemberID
	for _, raw := range []string{"revision=", "revision=" + strings.Repeat("A", 64), "revision=" + oldOfficial.Citation.VisibilityRevision + "&revision=" + oldOfficial.Citation.VisibilityRevision, "revision=%", "revision=" + oldOfficial.Citation.VisibilityRevision + ";unknown=x", "revision=" + oldOfficial.Citation.VisibilityRevision + "&url=https%3A%2F%2Fexample.test"} {
		request(citationBase+"?"+raw, "", 400, "request.invalid")
	}
	request(citationPath(oldOfficial), "x", 400, "request.invalid")
	check("citation reopen rejects malformed/duplicate revision, URL override and GET body with no-store and no returned source data")
	docsBefore := text(`SELECT jsonb_agg(to_jsonb(d) ORDER BY id)::text FROM knowledge_index_documents d`)
	exec(`UPDATE threads SET publication_status='private',status='private' WHERE id=440201`)
	out := search("HTTP校园公开中文资料", 1, 50)
	if out.Total != 34 {
		t.Fatal("private stale count", out.Total)
	}
	for _, hit := range out.Items {
		if hit.Citation.Source.ID == "440201" {
			t.Fatal("private stale snippet")
		}
	}
	request(citationPath(oldPrivate), "", 404, "resource.not_found")
	if docsBefore != text(`SELECT jsonb_agg(to_jsonb(d) ORDER BY id)::text FROM knowledge_index_documents d`) {
		t.Fatal("query repaired physical stale projection")
	}
	check("private stale indexed source disappears immediately from totals/snippets and old citation while physical projection stays unchanged")
	exec(`UPDATE richtext_article_contents SET content_html='<p>HTTP_CURRENT_REVISED_ARTICLE</p>' WHERE id=440390`)
	if search("图书馆 开放", 1, 10).Total != 1 {
		t.Fatal("old RichText snippet")
	}
	request(citationPath(oldArticle), "", 409, "resource.conflict")
	exec(`UPDATE knowledge_official_sources SET data=jsonb_set(data,'{status}','"withdrawn"') WHERE id=$1`, official.ID)
	if search("图书馆 开放", 1, 10).Total != 0 {
		t.Fatal("withdrawn official snippet")
	}
	request(citationPath(oldOfficial), "", 404, "resource.not_found")
	check("RichText-only revision invalidates old HTTP snippet/citation and official withdrawal denies stale citation without asynchronous cleanup")
	request(searchPath(missing, "图书馆", 1, 5), "", 404, "resource.not_found")
	request(path(missing)+"/sources/"+oldPrivate.Citation.MemberID+"?revision="+oldPrivate.Citation.VisibilityRevision, "", 404, "resource.not_found")
	check("unknown collection and cross-collection source reference cannot expand anonymous search/citation scope")
	if _, err = server.features.Request(knowledge.FeatureID, false); err != nil {
		t.Fatal(err)
	}
	request(searchPath(c.ID, "公开", 1, 5), "", 503, "knowledge.disabled")
	request(citationPath(oldPrivate), "", 503, "knowledge.disabled")
	check("actual owning Feature hot disable blocks both search and source ingress with no-store")
	listener.Close()
	listener = nil
	infra.Stop()
	infra = nil
	server, infra, listener = start()
	request(searchPath(c.ID, "公开", 1, 5), "", 503, "knowledge.disabled")
	if _, err = server.features.Request(knowledge.FeatureID, true); err != nil {
		t.Fatal(err)
	}
	// Only advance owned idle schedules; do not inject pages/cursors/generations.
	exec(`UPDATE knowledge_index_jobs SET data=jsonb_set(data,'{available_at_ms}','"0"') WHERE state='idle'`)
	wait("new production graph ready", ready)
	if search("HTTP校园公开中文资料", 1, 50).Total != 34 || search("图书馆 开放", 1, 5).Total != 0 {
		t.Fatal("restart resurrected stale snippets")
	}
	check("full infrastructure/router restart preserves Feature gate and current-source visibility; normal resident indexer recovers")
	listener.Close()
	listener = nil
	infra.Stop()
	infra = nil
	proof := map[string]any{"status": "passed", "actual_server_composition": true, "actual_production_router": true, "actual_postgresql": true, "actual_loopback_http": true, "normal_resident_indexer": true, "normal_resident_outbox": true, "fake_provider_or_manual_page": false, "browser_or_ui": false, "external_installations": 0, "checks": checks, "check_count": len(checks), "http_requests": httpRequests, "full_infrastructure_restarts": 1, "initial_index_documents": 38, "initial_unique_keyword_sources": 35, "http_timings": timings, "quality_scope": "HTTP lexical/citation contract smoke, not full Chinese question evaluation or capacity acceptance"}
	wire, _ := json.MarshalIndent(proof, "", "  ")
	if err = os.WriteFile(filepath.Join(root, "target-proof.json"), append(wire, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
