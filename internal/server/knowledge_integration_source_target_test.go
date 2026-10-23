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
func TestKnowledgeIntegrationSourcePostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("requires owned11 Integration PostgreSQL")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_11_source" {
		t.Fatal("refuse non-owned11 Integration database")
	}
	root := os.Getenv("CAMPUSOS_KNOWLEDGE_INTEGRATION_ROOT")
	if !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-11-source-read/") {
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
	cfg := config.Load()
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
		if d.OperationCode == "http.integration.knowledge.source.read" {
			if d.Method != http.MethodGet || d.Path != "/api/v1/integration/knowledge/collections/:id/sources/:member_id/revisions/:revision" || d.Audience != platformroute.AudienceIntegration || d.FeatureID != "knowledge" || d.PermissionCode != "" {
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
	var threadSource, officialSource, articleSource knowledge.VisibleSource
	for _, source := range list.Items {
		if source.Source.Reference.Kind == knowledge.OfficialKind {
			officialSource = source
		} else if source.Source.Reference.Kind == knowledge.PublicArticleKind {
			articleSource = source
		} else {
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
		return identityservice.IntegrationIssueRequest{PrincipalVersion: principal.Version, NotBeforeMS: nowMS, ExpiresAtMS: expires.UnixMilli(), Rules: []identityport.IntegrationAccessRule{{ID: "knowledge-source", Channel: "integration_http", Endpoint: "knowledge.source.read", Action: "knowledge.source.read", ResourceKind: "public_collection", ResourceIDs: []string{collectionID}, Status: "active", NotBeforeMS: nowMS, ExpiresAtMS: expires.UnixMilli()}}}
	}
	issued, err := machine.IssueCredential(ctx, principal.ID, issueRequest(c.ID, time.Now().Add(time.Hour)), resolve)
	if err != nil {
		t.Fatal("real credential issue failed", err)
	}
	tokens := []string{issued.Token}
	path := func(source knowledge.VisibleSource) string {
		return "/api/v1/integration/knowledge/collections/" + source.CollectionID + "/sources/" + source.MemberID + "/revisions/" + source.Revision
	}
	type result struct {
		status int
		cache  string
		raw    []byte
		err    error
	}
	client := &http.Client{Timeout: 8 * time.Second}
	do := func(target, token, body string, headers http.Header) result {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, listener.URL+target, strings.NewReader(body))
		if e != nil {
			return result{err: e}
		}
		if token != "" {
			req.Header.Set("Authorization", "Integration "+token)
		}
		for key, values := range headers {
			for _, v := range values {
				req.Header.Add(key, v)
			}
		}
		res, e := client.Do(req)
		if e != nil {
			return result{err: e}
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return result{status: res.StatusCode, cache: res.Header.Get("Cache-Control"), raw: raw, err: e}
	}
	httpRequests := 0
	assert := func(r result, want int, code string, expected *knowledge.VisibleSource) {
		t.Helper()
		httpRequests++
		if r.err != nil || r.status != want || !strings.Contains(r.cache, "no-store") {
			t.Fatalf("HTTP status/cache got=%d want=%d transport_error=%t", r.status, want, r.err != nil)
		}
		for _, marker := range append([]string{"PRIVATE_TARGET_TITLE", "PRIVATE_TARGET_BODY", "PRIVATE_TARGET_DOCUMENT_NAME", "PRIVATE_TARGET_ARTICLE_EXCERPT"}, tokens...) {
			if strings.Contains(string(r.raw), marker) {
				t.Fatal("private source or raw credential leaked")
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
			t.Fatal("invalid HTTP envelope")
		}
		if want != http.StatusOK {
			if env.Error == nil || env.Error.Code != code || len(env.Data) > 0 || strings.Contains(string(r.raw), "INTEGRATION_PUBLIC_") {
				t.Fatal("denial error contract or source-body leak")
			}
			return
		}
		var source knowledge.VisibleSource
		if env.Code != 0 || env.Error != nil || json.Unmarshal(env.Data, &source) != nil || expected == nil || source != *expected {
			t.Fatal("current source citation response mismatch")
		}
	}
	request := func(target, token, body string, headers http.Header, want int, code string, source *knowledge.VisibleSource) {
		t.Helper()
		assert(do(target, token, body, headers), want, code, source)
	}
	checks := []string{"ordinary Server module graph, Identity-owned machine gateway, production route catalog/router and actual loopback HTTP"}
	check := func(label string) { checks = append(checks, label) }
	request(path(threadSource), issued.Token, "", nil, 200, "", &threadSource)
	request(path(officialSource), issued.Token, "", nil, 200, "", &officialSource)
	request(path(articleSource), issued.Token, "", nil, 200, "", &articleSource)
	check("one exact endpoint/collection rule opens current public thread, official and RichText members within that collection")
	for _, headers := range []http.Header{
		{"Authorization": {"Bearer independent-admin-domain"}},
		{"Authorization": {"Integration duplicate"}},
		{"Cookie": {"campusos_admin_refresh=browser-session"}},
		{"Origin": {"https://example.test"}},
	} {
		request(path(threadSource), issued.Token, "", headers, 401, "auth.invalid_token", nil)
	}
	request(path(threadSource), "", "", nil, 401, "auth.invalid_token", nil)
	request(path(threadSource), "bad-token", "", nil, 401, "auth.invalid_token", nil)
	request(path(threadSource)+"?collection_id="+foreign.ID, issued.Token, "", nil, 401, "auth.invalid_token", nil)
	request(path(threadSource), issued.Token, `{"scope":"all"}`, nil, 400, "request.invalid", nil)
	request(strings.TrimSuffix(path(threadSource), threadSource.Revision)+strings.Repeat("A", 64), issued.Token, "", nil, 400, "request.invalid", nil)
	check("missing/malformed/wrong-domain/duplicate credentials, cookies, Origin, query overrides, GET body and noncanonical revisions fail without data")
	request(path(foreignList.Items[0]), issued.Token, "", nil, 403, "permission.denied", nil)
	foreignMember := foreignList.Items[0]
	foreignMember.CollectionID = c.ID
	request(path(foreignMember), issued.Token, "", nil, 404, "resource.not_found", nil)
	wrongEndpoint := issueRequest(c.ID, time.Now().Add(time.Hour))
	wrongEndpoint.Rules[0].Endpoint, wrongEndpoint.Rules[0].Action, wrongEndpoint.Rules[0].ResourceKind, wrongEndpoint.Rules[0].ResourceIDs = "public_thread.read", "community.thread.read", "community.thread", []string{"451201"}
	other, err := machine.IssueCredential(ctx, principal.ID, wrongEndpoint, resolve)
	if err != nil {
		t.Fatal("issue other endpoint credential", err)
	}
	tokens = append(tokens, other.Token)
	request(path(threadSource), other.Token, "", nil, 403, "permission.denied", nil)
	check("compiled endpoint whitelist and exact collection scope reject Community credentials and another collection")
	shrunkRule := issued.Credential.Rules[0]
	shrunkRule.ResourceIDs = []string{foreign.ID}
	changed, err := machine.UpdateCredential(ctx, principal.ID, issued.Credential.ID, issued.Credential.Version, []identityport.IntegrationAccessRule{shrunkRule}, false, resolve)
	if err != nil {
		t.Fatal("real scope shrink", err)
	}
	request(path(threadSource), issued.Token, "", nil, 403, "permission.denied", nil)
	issued.Credential, err = machine.UpdateCredential(ctx, principal.ID, changed.ID, changed.Version, issued.Credential.Rules, false, resolve)
	if err != nil {
		t.Fatal("real exact scope restore", err)
	}
	request(path(threadSource), issued.Token, "", nil, 200, "", &threadSource)
	check("real credential scope update removes then restores exact collection access; foreign member cannot cross collection binding")
	for _, table := range []string{"platform_command_audits", "platform_outbox"} {
		condition := "NEW.command_code='identity.integration.read'"
		if table == "platform_outbox" {
			condition = "NEW.event_type='integration.accessed'"
		}
		beforeAudits := scalar(`SELECT count(*) FROM platform_command_audits WHERE command_code='identity.integration.read'`)
		beforeEvents := scalar(`SELECT count(*) FROM platform_outbox WHERE event_type='integration.accessed'`)
		exec(`CREATE FUNCTION owned11_deny_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned receipt failure'; END $$`)
		exec(`CREATE TRIGGER owned11_deny_receipt BEFORE INSERT ON ` + table + ` FOR EACH ROW WHEN (` + condition + `) EXECUTE FUNCTION owned11_deny_receipt()`)
		request(path(threadSource), issued.Token, "", nil, 503, "knowledge.unavailable", nil)
		exec(`DROP TRIGGER owned11_deny_receipt ON ` + table + `; DROP FUNCTION owned11_deny_receipt()`)
		if scalar(`SELECT count(*) FROM platform_command_audits WHERE command_code='identity.integration.read'`) != beforeAudits || scalar(`SELECT count(*) FROM platform_outbox WHERE event_type='integration.accessed'`) != beforeEvents {
			t.Fatal("failed read receipt survived rollback")
		}
		request(path(threadSource), issued.Token, "", nil, 200, "", &threadSource)
	}
	check("real PostgreSQL typed audit and outbox insertion failures discard buffered source body and roll back both receipts")
	rotated, err := machine.RotateCredential(ctx, principal.ID, issued.Credential.ID, issued.Credential.Version, issueRequest(c.ID, time.Now().Add(time.Hour)), resolve)
	if err != nil {
		t.Fatal("real rotate failed", err)
	}
	tokens = append(tokens, rotated.Token)
	request(path(threadSource), issued.Token, "", nil, 401, "auth.invalid_token", nil)
	request(path(threadSource), rotated.Token, "", nil, 200, "", &threadSource)
	principal, err = machine.UpdatePrincipal(ctx, principal.ID, principal.Name, "suspended", principal.Version, resolve)
	if err != nil {
		t.Fatal(err)
	}
	request(path(threadSource), rotated.Token, "", nil, 401, "auth.invalid_token", nil)
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
	request(path(threadSource), short.Token, "", nil, 200, "", &threadSource)
	wait("natural credential expiry", func() bool { return time.Now().UnixMilli() >= expires.UnixMilli() })
	request(path(threadSource), short.Token, "", nil, 401, "auth.invalid_token", nil)
	check("real rotation invalidates old key; live principal suspension and natural credential expiry deny the same current citation")
	// Block audit insertion after the source callback. The concurrent revocation
	// must wait for the read transaction's Identity row locks and then take effect.
	holder, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	if _, err = holder.Exec(ctx, `SELECT pg_advisory_lock(451011)`); err != nil {
		t.Fatal(err)
	}
	defer holder.Exec(context.Background(), `SELECT pg_advisory_unlock(451011)`)
	exec(`CREATE FUNCTION owned11_block_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(451011); RETURN NEW; END $$;
 CREATE TRIGGER owned11_block_receipt BEFORE INSERT ON platform_command_audits FOR EACH ROW WHEN (NEW.command_code='identity.integration.read') EXECUTE FUNCTION owned11_block_receipt()`)
	readDone := make(chan result, 1)
	go func() { readDone <- do(path(threadSource), rotated.Token, "", nil) }()
	wait("read reached real audit lock", func() bool {
		return scalar(`SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND objid=451011 AND NOT granted`) == 1
	})
	for _, sql := range []string{
		`SELECT id FROM knowledge_collections WHERE id=$1 FOR UPDATE NOWAIT`,
		`SELECT id FROM knowledge_source_selections WHERE collection_id=$1 FOR UPDATE NOWAIT`,
		`SELECT id FROM knowledge_collection_sources WHERE collection_id=$1 FOR UPDATE NOWAIT`,
	} {
		_, lockErr := pool.Exec(ctx, sql, c.ID)
		var pe *pgconn.PgError
		if !errors.As(lockErr, &pe) || pe.Code != "55P03" {
			t.Fatal("current collection/membership lock did not survive source callback")
		}
	}
	_, lockErr := pool.Exec(ctx, `SELECT id FROM threads WHERE id=451201 FOR UPDATE NOWAIT`)
	var pe *pgconn.PgError
	if !errors.As(lockErr, &pe) || pe.Code != "55P03" {
		t.Fatal("original Community source lock did not survive source callback")
	}
	check("independent PostgreSQL NOWAIT proves collection, selection, member and original thread locks remain held at the actual audit barrier")
	revokeDone := make(chan error, 1)
	go func() {
		_, e := machine.UpdateCredential(ctx, principal.ID, rotated.Credential.ID, rotated.Credential.Version, nil, true, resolve)
		revokeDone <- e
	}()
	wait("revocation waits for read row locks", func() bool {
		return scalar(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%integration_principals%'`) > 0
	})
	select {
	case <-revokeDone:
		t.Fatal("revocation committed before buffered read transaction")
	default:
	}
	if _, err = holder.Exec(ctx, `SELECT pg_advisory_unlock(451011)`); err != nil {
		t.Fatal(err)
	}
	assert(<-readDone, 200, "", &threadSource)
	if err = <-revokeDone; err != nil {
		t.Fatal("ordered revocation failed", err)
	}
	exec(`DROP TRIGGER owned11_block_receipt ON platform_command_audits; DROP FUNCTION owned11_block_receipt()`)
	request(path(threadSource), rotated.Token, "", nil, 401, "auth.invalid_token", nil)
	check("actual audit barrier proves Identity locks survive the source callback through commit; competing key revocation commits afterwards and denies every later read")
	// Use a fresh current key to distinguish original-source authority failures
	// from machine identity failures.
	fresh, err := machine.IssueCredential(ctx, principal.ID, issueRequest(c.ID, time.Now().Add(time.Hour)), resolve)
	if err != nil {
		t.Fatal(err)
	}
	tokens = append(tokens, fresh.Token)
	exec(`UPDATE threads SET content='INTEGRATION_CURRENT_REVISED_THREAD' WHERE id=451201`)
	request(path(threadSource), fresh.Token, "", nil, 409, "resource.conflict", nil)
	exec(`UPDATE threads SET publication_status='private',status='private' WHERE id=451201`)
	request(path(threadSource), fresh.Token, "", nil, 404, "resource.not_found", nil)
	privateMember := threadSource
	privateMember.MemberID = "km_" + strings.Repeat("0", 32)
	request(path(privateMember), fresh.Token, "", nil, 404, "resource.not_found", nil)
	check("changed original body rejects old revision, private original and unknown member return no source")
	privateMember.MemberID = "451401"
	request(path(privateMember), fresh.Token, "", nil, 400, "request.invalid", nil)
	if scalar(`SELECT count(*) FROM personal_documents WHERE id=451401 AND owner_user_id=451101`) != 1 {
		t.Fatal("real owner-only private document fixture absent")
	}
	check("actual owner-only private document ID cannot enter the member namespace or disclose its name")
	if _, err = s.features.Request(knowledge.FeatureID, false); err != nil {
		t.Fatal(err)
	}
	request(path(officialSource), fresh.Token, "", nil, 503, "knowledge.disabled", nil)
	check("owning Feature hot disable closes the machine ingress")
	if scalar(`SELECT count(*) FROM platform_command_audits a JOIN platform_outbox o ON o.id=a.event_id WHERE a.command_code='identity.integration.read' AND a.actor_type='integration' AND a.actor_id=$1 AND a.resource_type='public_collection' AND a.resource_id=$2 AND o.event_type='integration.accessed' AND o.payload->>'endpoint'='knowledge.source.read' AND o.payload->>'action'='knowledge.source.read'`, principal.ID, c.ID) < 1 {
		t.Fatal("typed machine read audit/outbox pair absent")
	}
	var receipts string
	if err = pool.QueryRow(ctx, `SELECT (SELECT coalesce(jsonb_agg(to_jsonb(a))::text,'[]') FROM platform_command_audits a) || (SELECT coalesce(jsonb_agg(to_jsonb(o))::text,'[]') FROM platform_outbox o)`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	for _, token := range tokens {
		if strings.Contains(receipts, token) {
			t.Fatal("raw credential persisted in audit/outbox")
		}
	}
	if strings.Contains(receipts, "INTEGRATION_PUBLIC_") || strings.Contains(receipts, "PRIVATE_TARGET_") {
		t.Fatal("source contents persisted in machine read receipts")
	}
	if scalar(`SELECT count(*) FROM plugin_installations`)+scalar(`SELECT count(*) FROM plugin_user_installations`) != 0 {
		t.Fatal("unexpected external plugin installation")
	}
	check("successful machine receipts carry typed integration actor, exact endpoint/action/collection and outbox link without raw credentials or source body")
	proof := map[string]any{"status": "passed", "actual_server_composition": true, "actual_production_router": true, "actual_postgresql": true, "actual_loopback_http": true, "normal_identity_gateway": true, "mock_authority_or_manual_readiness": false, "fake_provider_or_manual_page": false, "source_read_requires_search_index": false, "private_source_leak_count": 0, "external_installations": 0, "browser_or_ui": false, "windows": false, "checks": checks, "check_count": len(checks), "http_requests": httpRequests, "actual_postgresql_server_version_num": scalar(`SELECT current_setting('server_version_num')::bigint`), "schema_table_count": scalar(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'`)}
	wire, err := json.MarshalIndent(proof, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(root, "target-proof.json"), append(wire, '\n'), 0600) != nil {
		t.Fatal("write target proof failed")
	}
}
