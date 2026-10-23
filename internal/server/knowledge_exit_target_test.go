package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	platformroute "github.com/campusos/CampusOS/internal/platform/route"
	"github.com/campusos/CampusOS/internal/transport/httpapi"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This target uses the ordinary module graph and production router. Neither
// machine authority nor the current public-source check is replaced by a fake.
func TestKnowledgeExitPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_EXIT_DSN")
	if dsn == "" {
		t.Skip("requires owned11 Integration PostgreSQL")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_11_exit" {
		t.Fatal("refuse non-owned11 Integration database")
	}
	root := os.Getenv("CAMPUSOS_KNOWLEDGE_EXIT_ROOT")
	if !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-11-exit/") {
		t.Fatal("owned Integration root required")
	}
	t.Chdir(root)
	ctx := t.Context()
	pool, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		t.Fatal("connect owned PostgreSQL")
	}
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal("owned SQL command failed", e)
		}
	}
	scalar := func(sql string, args ...any) int64 {
		t.Helper()
		var n int64
		if e := pool.QueryRow(ctx, sql, args...).Scan(&n); e != nil {
			t.Fatal("owned SQL scalar failed", e)
		}
		return n
	}
	wait := func(label string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if predicate() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("target deadline", label)
	}
	exec(`INSERT INTO users(id,username,nickname,email) VALUES(451101,'owned11-source','Integration owner','owned11-source@example.test');
 INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(451110,'Integration group','owned11-source-group','group','active');
 INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(451111,'Integration board','owned11-source-board',451110,'board','active');
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(451111,'discussion',true),(451111,'article',true);
 INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status) VALUES
 (451201,'公开机器引用','INTEGRATION_PUBLIC_THREAD','markdown',451101,'Integration owner',451111,'published','discussion','published','clear','active'),
 (451202,'PRIVATE_TARGET_TITLE','PRIVATE_TARGET_BODY','markdown',451101,'Integration owner',451111,'private','discussion','private','clear','active'),
 (451203,'公开机器文章','PRIVATE_TARGET_ARTICLE_EXCERPT','safe_html',451101,'Integration owner',451111,'published','article','published','clear','active');
 INSERT INTO richtext_article_contents(id,thread_id,title,content_html,status,created_by) VALUES(451303,451203,'公开机器文章','<p>INTEGRATION_PUBLIC_ARTICLE</p>','published',451101);
 INSERT INTO personal_documents(id,owner_user_id,name,document_type) VALUES(451401,451101,'PRIVATE_TARGET_DOCUMENT_NAME','text');`)
	private := prepareKnowledgeExitTrust(t, root)
	cfg := config.Load()
	cfg.Plugin.ContainerEnabled = true
	cfg.Database.DSN, cfg.Plugin.DataDir, cfg.Redis.Enabled = dsn, filepath.Join(root, "data/plugin_data"), false
	s := New(cfg)
	infra, err := s.startInfrastructure()
	if err != nil || infra.database == nil || s.knowledge == nil {
		t.Fatal("normal production composition required")
	}
	defer infra.Stop()
	pluginHandler, err := s.pluginHTTPHandler()
	if err != nil {
		t.Fatal(err)
	}
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
		Reliability: s.reliability.Handler(), EmailDelivery: s.emailDelivery.Handler(), Metrics: infra.metrics,
	})
	found := false
	for _, d := range router.RouteDescriptors() {
		if d.OperationCode == "http.integration.knowledge.search" {
			if d.Method != http.MethodPost || d.Path != "/api/v1/integration/knowledge/collections/:id/search" || d.Audience != platformroute.AudienceIntegration || d.FeatureID != "knowledge" || d.PermissionCode != "" {
				t.Fatal("Integration route authority drift")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("production Integration source route missing")
	}
	if err = s.identity.SyncRouteDescriptors(ctx, router.RouteDescriptors()); err != nil {
		t.Fatal("normal route catalog sync failed")
	}
	listener := httptest.NewServer(router)
	defer listener.Close()
	value, ok := s.appContext.Lookup("identity.integration-read-gateway")
	machine, okService := value.(*identityservice.IntegrationService)
	if !ok || !okService {
		t.Fatal("actual Identity IntegrationService required")
	}
	if _, err = s.features.Request(knowledge.FeatureID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.features.Request("controlled-richtext-article", true); err != nil {
		t.Fatal(err)
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account, sid := "adm.knowledge.integration", "as.knowledge.integration"
	if err = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "integration-source", Email: "integration-source@example.test", DisplayName: "Integration source native"}); err != nil {
		t.Fatal(err)
	}
	if err = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.knowledge.integration", AccountID: account, RoleID: "security_admin", GrantedBy: account}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = identityrepo.NewPgAdminSessionRepository(pool).CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: account, RefreshTokenDigest: strings.Repeat("b", 64), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, s.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: account, SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	collections := s.knowledge.Collections()
	createCollection := func(title string, sources ...knowledge.Selection) knowledge.Collection {
		t.Helper()
		c, e := collections.SaveCollection(ctx, knowledge.Collection{Title: title, Status: "active"}, resolve)
		if e != nil {
			t.Fatal(e)
		}
		for _, source := range sources {
			state, e := collections.SaveSelection(ctx, c.ID, c.Version, source, resolve)
			if e != nil {
				t.Fatal(e)
			}
			c = state.Collection
		}
		return c
	}
	official, err := collections.SaveOfficial(ctx, knowledge.Official{Title: "公开官方机器引用", Content: "INTEGRATION_PUBLIC_OFFICIAL", Format: "text", Status: "published"}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	c := createCollection("机器公开集合", knowledge.Selection{Kind: "thread", SourceID: "451201", Active: true}, knowledge.Selection{Kind: "official", SourceID: official.ID, Active: true}, knowledge.Selection{Kind: "thread", SourceID: "451203", Active: true})
	foreign := createCollection("其他公开集合", knowledge.Selection{Kind: "official", SourceID: official.ID, Active: true})
	list, err := collections.Sources(ctx, c.ID, 1, 50)
	if err != nil || len(list.Items) != 3 {
		t.Fatal("normal current source inventory required", err)
	}
	var threadSource knowledge.VisibleSource
	for _, source := range list.Items {
		if source.Source.Reference.Kind == knowledge.PublicThreadKind {
			threadSource = source
		}
	}
	foreignList, err := collections.Sources(ctx, foreign.ID, 1, 50)
	if err != nil || len(foreignList.Items) != 1 {
		t.Fatal("foreign collection inventory required", err)
	}
	principal, err := machine.CreatePrincipal(ctx, "native knowledge reader", resolve)
	if err != nil {
		t.Fatal(err)
	}
	issueRequest := func(collectionID string, expires time.Time) identityservice.IntegrationIssueRequest {
		nowMS := time.Now().Add(-time.Second).UnixMilli()
		return identityservice.IntegrationIssueRequest{PrincipalVersion: principal.Version, NotBeforeMS: nowMS, ExpiresAtMS: expires.UnixMilli(), Rules: []identityport.IntegrationAccessRule{{ID: "knowledge-source", Channel: "integration_http", Endpoint: "knowledge.search", Action: "knowledge.source.read", ResourceKind: "public_collection", ResourceIDs: []string{collectionID}, Status: "active", NotBeforeMS: nowMS, ExpiresAtMS: expires.UnixMilli()}}}
	}
	waitIndex := time.Now().Add(45 * time.Second)
	for {
		ready := scalar(`SELECT count(*) FROM knowledge_index_jobs j JOIN knowledge_collections c ON c.id=j.id WHERE j.id=$1 AND j.state='idle' AND j.data->>'run_generation'='0' AND (j.data->>'ready_generation')::bigint>0 AND j.data->>'collection_version'=c.facts_version::text AND j.data->>'requested'=j.data->>'active_request' AND j.available_at_ms>$2`, c.ID, time.Now().Add(time.Second).UnixMilli()) == 1
		if ready && scalar(`SELECT count(*) FROM knowledge_index_documents WHERE collection_id=$1`, c.ID) == 3 {
			break
		}
		if time.Now().After(waitIndex) {
			t.Fatal("normal resident indexer deadline")
		}
		time.Sleep(25 * time.Millisecond)
	}
	issued, err := machine.IssueCredential(ctx, principal.ID, issueRequest(c.ID, time.Now().Add(time.Hour)), resolve)
	if err != nil {
		t.Fatal(err)
	}
	tokens := []string{issued.Token}
	searchPath := func(id string) string { return "/api/v1/integration/knowledge/collections/" + id + "/search" }
	sourcePath := func(source knowledge.VisibleSource) string {
		return "/api/v1/integration/knowledge/collections/" + source.CollectionID + "/sources/" + source.MemberID + "/revisions/" + source.Revision
	}
	const queryBody = `{"query":"INTEGRATION_PUBLIC","page":1,"page_size":2}`
	type result struct {
		status int
		cache  string
		raw    []byte
		err    error
		path   string
	}
	client := &http.Client{Timeout: 8 * time.Second}
	do := func(method, path, token, body string, headers http.Header) result {
		req, e := http.NewRequestWithContext(ctx, method, listener.URL+path, strings.NewReader(body))
		if e != nil {
			return result{err: e}
		}
		if token != "" {
			req.Header.Set("Authorization", "Integration "+token)
		}
		for k, values := range headers {
			for _, v := range values {
				req.Header.Add(k, v)
			}
		}
		req.Header.Set("Content-Type", "application/json")
		res, e := client.Do(req)
		if e != nil {
			return result{err: e}
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return result{status: res.StatusCode, cache: res.Header.Get("Cache-Control"), raw: raw, err: e, path: path}
	}
	count := 0
	assert := func(r result, want int, code string) knowledge.PublicSearchPage {
		t.Helper()
		count++
		if r.err != nil || r.status != want || strings.HasPrefix(r.path, "/api/v1/integration/knowledge/") && !strings.Contains(r.cache, "no-store") {
			t.Fatalf("actual HTTP status/cache got=%d want=%d transport_error=%t", r.status, want, r.err != nil)
		}
		for _, marker := range append([]string{"PRIVATE_TARGET_TITLE", "PRIVATE_TARGET_BODY", "PRIVATE_TARGET_DOCUMENT_NAME", "PRIVATE_TARGET_ARTICLE_EXCERPT"}, tokens...) {
			if strings.Contains(string(r.raw), marker) {
				t.Fatal("private data or raw credential leaked")
			}
		}
		var env struct {
			Code  int             `json:"code"`
			Data  json.RawMessage `json:"data"`
			Error *struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(r.raw, &env) != nil {
			t.Fatal("malformed response")
		}
		if want != 200 {
			if env.Error == nil || code != "" && env.Error.Code != code || len(env.Data) > 0 {
				t.Fatal("denial error contract or buffered page leaked")
			}
			return knowledge.PublicSearchPage{}
		}
		var out knowledge.PublicSearchPage
		if env.Code != 0 || env.Error != nil || json.Unmarshal(env.Data, &out) != nil || out.Provider != knowledge.BuiltinLexicalProvider || out.Generation < 1 {
			t.Fatal("search result contract")
		}
		for _, hit := range out.Items {
			if hit.Citation.CollectionID != c.ID || len(hit.Citation.VisibilityRevision) != 64 || hit.Citation.Generation != out.Generation {
				t.Fatal("search citation authority drift")
			}
			if hit.Citation.Source.Kind != "community.thread" && hit.Citation.Source.Kind != "richtext.article" && hit.Citation.Source.Kind != knowledge.OfficialKind {
				t.Fatal("unsupported source entered search")
			}
		}
		return out
	}
	request := func(id, token, body string, want int, code string) knowledge.PublicSearchPage {
		t.Helper()
		return assert(do("POST", searchPath(id), token, body, nil), want, code)
	}
	checks := []string{"production Server module graph/router/catalog, actual loopback HTTP, current Identity gateway and normal resident indexer with zero manual readiness"}
	check := func(s string) { checks = append(checks, s) }
	runKnowledgeMCPExit(t, s, pool, listener.URL, machine, principal, resolve, c, foreign, threadSource, issued.Token, root, &checks)
	first := request(c.ID, issued.Token, queryBody, 200, "")
	if first.Total != 3 || len(first.Items) != 2 || first.Page != 1 || first.PageSize != 2 {
		t.Fatal("normal corpus/paging")
	}
	second := request(c.ID, issued.Token, `{"query":"INTEGRATION_PUBLIC","page":2,"page_size":2}`, 200, "")
	if second.Total != 3 || len(second.Items) != 1 {
		t.Fatal("second page")
	}
	seen := map[string]bool{}
	for _, h := range append(first.Items, second.Items...) {
		if seen[h.Citation.MemberID] {
			t.Fatal("duplicated citation")
		}
		seen[h.Citation.MemberID] = true
	}
	check("current public thread, official and RichText sources share exact totals, disjoint pages, explanations and current bound citations")
	// Citation opening is a different endpoint even when both use public-read action.
	assert(do("GET", sourcePath(threadSource), issued.Token, "", nil), 403, "permission.denied")
	sourceReq := issueRequest(c.ID, time.Now().Add(time.Hour))
	sourceReq.Rules[0].Endpoint = "knowledge.source.read"
	sourceKey, err := machine.IssueCredential(ctx, principal.ID, sourceReq, resolve)
	if err != nil {
		t.Fatal(err)
	}
	tokens = append(tokens, sourceKey.Token)
	request(c.ID, sourceKey.Token, queryBody, 403, "permission.denied")
	request(foreign.ID, issued.Token, queryBody, 403, "permission.denied")
	check("search and source-read endpoint rules cannot borrow each other; another collection is rejected before search")
	for _, headers := range []http.Header{{"Authorization": {"Bearer user-or-admin"}}, {"Authorization": {"Integration duplicate"}}, {"Cookie": {"session=browser"}}, {"Origin": {"https://example.test"}}} {
		assert(do("POST", searchPath(c.ID), issued.Token, queryBody, headers), 401, "auth.invalid_token")
	}
	request(c.ID, "", queryBody, 401, "auth.invalid_token")
	assert(do("POST", searchPath(c.ID)+"?query=override", issued.Token, queryBody, nil), 401, "auth.invalid_token")
	for _, body := range []string{`{"query":"ok","page":1,"page_size":2,"provider":"remote"}`, `{"query":"ok","query":"override","page":1,"page_size":2}`, `{"query":"ok","page":1,"page_size":2,"collection_id":"foreign"}`, `{"query":"ok","page":0,"page_size":2}`, `{"query":"ok","page":1,"page_size":51}`, `{"query":"\ud800","page":1,"page_size":2}`} {
		request(c.ID, issued.Token, body, 400, "request.invalid")
	}
	check("strict closed JSON and independent machine authentication reject browser/JWT/duplicate headers, URL overrides, Provider/scope injection and invalid Unicode/paging")
	assert(do("GET", "/api/v1/mcp/tools", issued.Token, "", nil), 401, "")
	check("a search Integration key cannot read or control the independent Admin MCP tool directory")
	shrunk := issued.Credential.Rules[0]
	shrunk.ResourceIDs = []string{foreign.ID}
	changed, err := machine.UpdateCredential(ctx, principal.ID, issued.Credential.ID, issued.Credential.Version, []identityport.IntegrationAccessRule{shrunk}, false, resolve)
	if err != nil {
		t.Fatal(err)
	}
	request(c.ID, issued.Token, queryBody, 403, "permission.denied")
	issued.Credential, err = machine.UpdateCredential(ctx, principal.ID, changed.ID, changed.Version, issued.Credential.Rules, false, resolve)
	if err != nil {
		t.Fatal(err)
	}
	request(c.ID, issued.Token, queryBody, 200, "")
	check("real current credential scope shrink immediately removes search, restored exact scope succeeds without an allow cache")
	for _, table := range []string{"platform_command_audits", "platform_outbox"} {
		condition := "NEW.command_code='identity.integration.read'"
		if table == "platform_outbox" {
			condition = "NEW.event_type='integration.accessed'"
		}
		beforeA := scalar(`SELECT count(*) FROM platform_command_audits WHERE command_code='identity.integration.read'`)
		beforeO := scalar(`SELECT count(*) FROM platform_outbox WHERE event_type='integration.accessed'`)
		exec(`CREATE FUNCTION owned11_search_deny() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned search receipt failure'; END $$`)
		exec(`CREATE TRIGGER owned11_search_deny BEFORE INSERT ON ` + table + ` FOR EACH ROW WHEN (` + condition + `) EXECUTE FUNCTION owned11_search_deny()`)
		request(c.ID, issued.Token, queryBody, 503, "knowledge.unavailable")
		exec(`DROP TRIGGER owned11_search_deny ON ` + table + `; DROP FUNCTION owned11_search_deny()`)
		if scalar(`SELECT count(*) FROM platform_command_audits WHERE command_code='identity.integration.read'`) != beforeA || scalar(`SELECT count(*) FROM platform_outbox WHERE event_type='integration.accessed'`) != beforeO {
			t.Fatal("failed search receipts survived rollback")
		}
	}
	check("real PostgreSQL audit and outbox insertion failures discard snippets and totals and roll back both read receipts")
	rotated, err := machine.RotateCredential(ctx, principal.ID, issued.Credential.ID, issued.Credential.Version, issueRequest(c.ID, time.Now().Add(time.Hour)), resolve)
	if err != nil {
		t.Fatal(err)
	}
	tokens = append(tokens, rotated.Token)
	request(c.ID, issued.Token, queryBody, 401, "auth.invalid_token")
	request(c.ID, rotated.Token, queryBody, 200, "")
	principal, err = machine.UpdatePrincipal(ctx, principal.ID, principal.Name, "suspended", principal.Version, resolve)
	if err != nil {
		t.Fatal(err)
	}
	request(c.ID, rotated.Token, queryBody, 401, "auth.invalid_token")
	principal, err = machine.UpdatePrincipal(ctx, principal.ID, principal.Name, "active", principal.Version, resolve)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(1200 * time.Millisecond)
	short, err := machine.IssueCredential(ctx, principal.ID, issueRequest(c.ID, expires), resolve)
	if err != nil {
		t.Fatal(err)
	}
	tokens = append(tokens, short.Token)
	request(c.ID, short.Token, queryBody, 200, "")
	wait("natural key expiry", func() bool { return time.Now().UnixMilli() >= expires.UnixMilli() })
	request(c.ID, short.Token, queryBody, 401, "auth.invalid_token")
	check("actual rotation, principal suspension and natural credential expiry reject the same previously successful search")
	holder, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	if _, err = holder.Exec(ctx, `SELECT pg_advisory_lock(451012)`); err != nil {
		t.Fatal(err)
	}
	defer holder.Exec(context.Background(), `SELECT pg_advisory_unlock(451012)`)
	exec(`CREATE FUNCTION owned11_search_block() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(451012); RETURN NEW; END $$; CREATE TRIGGER owned11_search_block BEFORE INSERT ON platform_command_audits FOR EACH ROW WHEN (NEW.command_code='identity.integration.read') EXECUTE FUNCTION owned11_search_block()`)
	readDone := make(chan result, 1)
	go func() { readDone <- do("POST", searchPath(c.ID), rotated.Token, queryBody, nil) }()
	wait("search audit barrier", func() bool {
		return scalar(`SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND objid=451012 AND NOT granted`) == 1
	})
	for _, q := range []string{`SELECT id FROM knowledge_collections WHERE id=$1 FOR UPDATE NOWAIT`, `SELECT id FROM knowledge_index_jobs WHERE id=$1 FOR UPDATE NOWAIT`, `SELECT id FROM knowledge_collection_sources WHERE collection_id=$1 FOR UPDATE NOWAIT`} {
		_, e := pool.Exec(ctx, q, c.ID)
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "55P03" {
			t.Fatal("search collection/index/member lock expired before audit")
		}
	}
	_, e := pool.Exec(ctx, `SELECT id FROM threads WHERE id=451201 FOR UPDATE NOWAIT`)
	var pe *pgconn.PgError
	if !errors.As(e, &pe) || pe.Code != "55P03" {
		t.Fatal("searched origin lock expired before audit")
	}
	check("real audit barrier plus independent NOWAIT proves collection/index/member/current origin locks survive Retrieval callback to machine commit")
	revokeDone := make(chan error, 1)
	go func() {
		_, e := machine.UpdateCredential(ctx, principal.ID, rotated.Credential.ID, rotated.Credential.Version, nil, true, resolve)
		revokeDone <- e
	}()
	wait("revoke waits for searched machine proof", func() bool {
		return scalar(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%integration_principals%'`) > 0
	})
	select {
	case <-revokeDone:
		t.Fatal("revoke passed an active read lock")
	default:
	}
	if _, err = holder.Exec(ctx, `SELECT pg_advisory_unlock(451012)`); err != nil {
		t.Fatal(err)
	}
	assert(<-readDone, 200, "")
	if err = <-revokeDone; err != nil {
		t.Fatal(err)
	}
	exec(`DROP TRIGGER owned11_search_block ON platform_command_audits; DROP FUNCTION owned11_search_block()`)
	request(c.ID, rotated.Token, queryBody, 401, "auth.invalid_token")
	check("concurrent real revocation waits for prior search commit, then all later requests deny without snippets or totals")
	fresh, err := machine.IssueCredential(ctx, principal.ID, issueRequest(c.ID, time.Now().Add(time.Hour)), resolve)
	if err != nil {
		t.Fatal(err)
	}
	tokens = append(tokens, fresh.Token)
	runKnowledgeHostExit(t, s, pool, private, resolve, c, foreign, threadSource, root, &checks)
	exec(`UPDATE threads SET publication_status='private',status='private' WHERE id=451201`)
	current := request(c.ID, fresh.Token, queryBody, 200, "")
	if current.Total != 2 {
		t.Fatal("private origin retained in total")
	}
	for _, hit := range current.Items {
		if hit.Citation.Source.ID == "451201" {
			t.Fatal("private origin retained in results")
		}
	}
	if scalar(`SELECT count(*) FROM knowledge_index_documents WHERE collection_id=$1`, c.ID) != 3 {
		t.Fatal("search unexpectedly relied on asynchronous index purge")
	}
	check("private current origin disappears before total/paging while stale index row still exists; no projection or result authority cache")
	if _, err = collections.SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "document", SourceID: "451401", Active: true}, resolve); !errors.Is(err, knowledge.ErrInvalid) {
		t.Fatal("private document accepted into public collection")
	}
	if scalar(`SELECT count(*) FROM personal_documents WHERE id=451401`) != 1 {
		t.Fatal("actual private fixture absent")
	}
	check("actual owner-only private document cannot become a public selection or machine search result")
	if _, err = s.features.Request(knowledge.FeatureID, false); err != nil {
		t.Fatal(err)
	}
	request(c.ID, fresh.Token, queryBody, 503, "knowledge.disabled")
	check("owning Feature hot disable closes machine search without an index-derived fallback")
	if scalar(`SELECT count(*) FROM platform_command_audits a JOIN platform_outbox o ON o.id=a.event_id WHERE a.command_code='identity.integration.read' AND a.actor_type='integration' AND a.actor_id=$1 AND a.resource_type='public_collection' AND a.resource_id=$2 AND o.event_type='integration.accessed' AND o.payload->>'endpoint'='knowledge.search' AND o.payload->>'action'='knowledge.source.read'`, principal.ID, c.ID) < 1 {
		t.Fatal("typed search audit/outbox pair absent")
	}
	var receipts string
	if err = pool.QueryRow(ctx, `SELECT (SELECT coalesce(jsonb_agg(to_jsonb(a))::text,'[]') FROM platform_command_audits a)||(SELECT coalesce(jsonb_agg(to_jsonb(o))::text,'[]') FROM platform_outbox o)`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	for _, token := range tokens {
		if strings.Contains(receipts, token) {
			t.Fatal("raw key in receipts")
		}
	}
	if strings.Contains(receipts, "INTEGRATION_PUBLIC") || strings.Contains(receipts, "PRIVATE_TARGET_") {
		t.Fatal("query/source body in receipts")
	}
	if scalar(`SELECT count(*) FROM plugin_installations`)+scalar(`SELECT count(*) FROM plugin_user_installations`) != 2 {
		t.Fatal("expected global and user owned SDK example installations")
	}
	check("typed receipts contain endpoint/action/collection and integration actor, never query, source text or raw key; one owned SDK example, global and user installation")
	proof := map[string]any{"status": "passed", "checks": checks, "check_count": len(checks), "http_requests": count, "actual_server_composition": true, "actual_production_router": true, "actual_loopback_http": true, "actual_postgresql": true, "normal_resident_indexer": true, "fake_provider_or_manual_page": false, "private_source_leak_count": 0, "projected_documents": 3, "unindexed_private_documents": 1, "external_installations": 2, "actual_container_sdk": true, "actual_standard_mcp": true, "browser_or_ui": false, "windows": false}
	wire, err := json.MarshalIndent(proof, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(root, "target-proof.json"), append(wire, '\n'), 0600) != nil {
		t.Fatal("write proof")
	}
}
