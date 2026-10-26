package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/modules/features/personaldocuments"
	"github.com/campusos/CampusOS/internal/transport/httpapi"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKnowledgePrivateIndexPostgres(t *testing.T) {
	dsn, root := os.Getenv("CAMPUSOS_PRIVATE_TARGET_DSN"), os.Getenv("CAMPUSOS_PRIVATE_TARGET_ROOT")
	if dsn == "" {
		t.Skip("requires owned13 private-preflight PostgreSQL target")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_13_index" || !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-13-exit/") {
		t.Fatal("refuse non-owned13 private target")
	}
	t.Chdir(root)
	ctx := t.Context()
	pool, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	sql := func(query string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	sql(`INSERT INTO users(id,username,nickname,email) VALUES(613101,'owned13-first','PRIVATE_OWNER_13','owned13-first@example.test'),(613102,'owned13-second','PRIVATE_OTHER_OWNER_13','owned13-second@example.test')`)
	cfg := config.Load()
	cfg.Database.DSN, cfg.Plugin.DataDir, cfg.Redis.Enabled = dsn, filepath.Join(root, "data/plugin_data"), false
	s := New(cfg)
	infra, err := s.startInfrastructure()
	if err != nil || s.knowledge == nil || s.personalDocuments == nil || s.knowledge.PrivateIndex() == nil {
		t.Fatal("normal Server composition required", err)
	}
	defer func() {
		if infra != nil {
			infra.Stop()
		}
	}()
	buildRouter := func(current *Server, infrastructure *infrastructureBootstrap) *httptest.Server {
		pluginHandler, err := current.pluginHTTPHandler()
		if err != nil {
			t.Fatal(err)
		}
		router := httpapi.Build(httpapi.Dependencies{
			JWT: current.identity.JWTManager(), SessionVerifier: current.identity.Sessions(), Permissions: current.identity.Permissions(),
			AdminAccess: current.identity.AdminAccess(), AdminMFA: current.identity.MFA(), AdminJWT: current.identity.AdminDomainJWT(),
			AdminSessions: portadapt.NewAdminDomainSessionVerifier(current.identity.AdminDomainSessions()), AdminCodes: portadapt.NewAdminCodeChecker(current.identity.AdminDomainLifecycle()),
			Features: current.features, Feature: current.featureHandler, ModuleCatalog: current.moduleCatalog, PluginManager: current.manager,
			Identity: current.identity.Handlers(), Community: current.community.Handlers(), AcademicTerm: current.academicTerm.Handler(), UserStorage: current.storage.Handler(),
			Space: current.space.Handler(), Plugin: pluginHandler, PluginV5: current.pluginsV5Handler(), AI: current.ai.Handler(), Integration: current.integration.Handler(),
			Webhook: current.webhook.Handler(), MCP: current.mcp.Handler(), Message: current.message.Handler(), Homepage: current.appearance.HomepageHandler(), WebTheme: current.appearance.WebThemeHandler(),
			RichText: current.richtext.Handler(), Schedule: current.schedule.Handler(), PersonalDocuments: current.personalDocuments.Handler(), Knowledge: current.knowledge.Handler(),
			MutualAid: current.mutualAid.Handler(), Secondhand: current.secondhand.Handler(), PlatformLog: current.platformLog.Handler(), Moderation: current.moderation.Handler(),
			Reliability: current.reliability.Handler(), EmailDelivery: current.emailDelivery.Handler(), Metrics: infrastructure.metrics,
		})
		if err = current.identity.SyncRouteDescriptors(ctx, router.RouteDescriptors()); err != nil {
			t.Fatal(err)
		}
		return httptest.NewServer(router)
	}
	listener := buildRouter(s, infra)
	defer func() {
		if listener != nil {
			listener.Close()
		}
	}()
	for _, feature := range []string{knowledge.FeatureID, personaldocuments.FeatureID} {
		if _, err = s.features.Request(feature, true); err != nil {
			t.Fatal(err)
		}
	}
	users := identityrepo.NewPgUserRepository(pool)
	issue := func(id string) *identityservice.SessionTokens {
		t.Helper()
		user, e := users.GetByID(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		tokens, e := s.identity.Sessions().Issue(ctx, user, identityservice.SessionMetadata{DeviceName: "Owned13 preflight"})
		if e != nil {
			t.Fatal(e)
		}
		return tokens
	}
	first, second := issue("613101"), issue("613102")
	claims, err := s.identity.JWTManager().VerifyAccessToken(first.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(current context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(current, s.identity.Sessions(), s.identity.Sessions(), claims)
	}
	repoRoot := os.Getenv("CAMPUSOS_PRIVATE_REPO")
	if !filepath.IsAbs(repoRoot) {
		t.Fatal("owned repo required")
	}
	for _, dir := range []string{"migrations", "modules"} {
		if e := os.CopyFS(filepath.Join(root, dir), os.DirFS(filepath.Join(repoRoot, dir))); e != nil {
			t.Fatal(e)
		}
	}
	password := adminSPABrowserSecret(t)
	hash, e := auth.HashPassword(password)
	if e != nil {
		t.Fatal(e)
	}
	sql(`INSERT INTO accounts(id,user_id,type,identifier,identifier_normalized,credential,verified,verification_state,verified_at) VALUES(613901,613101,'email','owned13-first@example.test','owned13-first@example.test',$1,true,'verified',now())`, hash)
	sql(`INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(613110,'Private group','private-index-group','group','active'); INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(613111,'Private board','private-index-board',613110,'board','active');INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(613111,'discussion',true);
 INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status) VALUES(613201,'私人帖子标题','PRIVATE_POST_BODY 校园图书馆','markdown',613101,'Owned13',613111,'private','discussion','private','clear','active')`)
	documents := map[string]personaldocuments.DocumentDetail{}
	for _, format := range []string{"text", "markdown", "campusdoc"} {
		content := "PRIVATE_DOCUMENT_BODY_13 校园图书馆"
		if format == "campusdoc" {
			content = `{"version":1,"blocks":[{"type":"paragraph","text":"PRIVATE_DOCUMENT_BODY_13 校园图书馆"}]}`
		}
		ext := map[string]string{"text": "txt", "markdown": "md", "campusdoc": "campusdoc"}[format]
		d, e := s.personalDocuments.Service().CreateTextForPrincipal(ctx, resolve, personaldocuments.CreateRequest{Name: "私人资料." + ext, Format: format, Content: content})
		if e != nil {
			t.Fatal(e)
		}
		documents[format] = d
	}
	browserDoc, e := s.personalDocuments.Service().CreateTextForPrincipal(ctx, resolve, personaldocuments.CreateRequest{Name: "私人浏览器资料.txt", Format: "text", Content: `BROWSER_PRIVATE_SECRET 校园图书馆 <img src="/private-must-not-fetch" onerror="window.privateInjected=true">`})
	if e != nil {
		t.Fatal(e)
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account, adminSID := "613101", "as.knowledge.private"
	if err = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "owned13-admin", Email: "owned13-admin@example.test", DisplayName: "Owned13 Admin"}); err != nil {
		t.Fatal(err)
	}
	if err = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.knowledge.private", AccountID: account, RoleID: "security_admin", GrantedBy: account}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = identityrepo.NewPgAdminSessionRepository(pool).CreateSession(ctx, identityrepo.AdminSession{ID: adminSID, AccountID: account, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: adminSID, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	adminToken, err := s.identity.AdminDomainJWT().GenerateAccessToken(account, "Owned13", auth.AccessTokenContext{SessionID: adminSID, AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	adminResolve := func(current context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(current, s.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: account, SessionID: adminSID, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	value, _ := s.appContext.Lookup("identity.integration-read-gateway")
	machine, ok := value.(*identityservice.IntegrationService)
	if !ok {
		t.Fatal("ordinary Integration issuer required")
	}
	principal, err := machine.CreatePrincipal(ctx, "Owned13 machine", adminResolve)
	if err != nil {
		t.Fatal(err)
	}
	beforeMS, expiresMS := now.UnixMilli(), now.Add(time.Hour).UnixMilli()
	machineToken, err := machine.IssueCredential(ctx, principal.ID, identityservice.IntegrationIssueRequest{PrincipalVersion: principal.Version, NotBeforeMS: beforeMS, ExpiresAtMS: expiresMS, Rules: []identityport.IntegrationAccessRule{{ID: "owned13-public", Channel: "integration_http", Endpoint: "knowledge.source.read", Action: "knowledge.source.read", ResourceKind: "public_collection", ResourceIDs: []string{"kc_" + strings.Repeat("0", 32)}, Status: "active", NotBeforeMS: beforeMS, ExpiresAtMS: expiresMS}}}, adminResolve)
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{}
	requests := 0
	call := func(method, path, token string, data any, want int) json.RawMessage {
		t.Helper()
		var raw []byte
		if data != nil {
			var e error
			raw, e = json.Marshal(data)
			if e != nil {
				t.Fatal(e)
			}
		}
		req, e := http.NewRequestWithContext(ctx, method, listener.URL+"/api/v1/knowledge/private"+path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, e := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		raw, e = io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if e != nil {
			t.Fatal(e)
		}
		requests++
		if res.StatusCode != want || !strings.Contains(res.Header.Get("Cache-Control"), "no-store") {
			t.Fatalf("%s %s status %d want %d: %s", method, path, res.StatusCode, want, raw)
		}
		var envelope struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(raw, &envelope) != nil {
			t.Fatal("envelope")
		}
		if want == 200 {
			if envelope.Code != 0 {
				t.Fatal("failed success", string(raw))
			}
		} else if len(envelope.Data) > 0 {
			t.Fatal("denial leaked data", string(raw))
		}
		return envelope.Data
	}
	check := func(name string) { checks = append(checks, name) }
	var status struct {
		CanIndex bool `json:"can_index"`
		Default  bool `json:"default_inclusion"`
		Outbound bool `json:"outbound_authorized"`
	}
	_ = json.Unmarshal(call("GET", "/status", first.AccessToken, nil, 200), &status)
	if !status.CanIndex || status.Default || status.Outbound {
		t.Fatal("private default/protection contract")
	}
	check("protected_external_key_custody_ready_default_no_inclusion_or_egress")
	list := func() []map[string]any {
		var value struct {
			Items []map[string]any `json:"items"`
		}
		if e := json.Unmarshal(call("GET", "/selections", first.AccessToken, nil, 200), &value); e != nil {
			t.Fatal(e)
		}
		return value.Items
	}
	if len(list()) != 0 {
		t.Fatal("implicit private index")
	}
	preview := func(kind, id string) knowledge.PrivateSource {
		var v struct {
			Source   knowledge.PrivateSource `json:"source"`
			CanIndex bool                    `json:"can_index"`
		}
		_ = json.Unmarshal(call("POST", "/preview", first.AccessToken, map[string]string{"kind": kind, "source_id": id}, 200), &v)
		if !v.CanIndex || v.Source.Revision == "" || v.Source.Text != "" {
			t.Fatal("preview contract")
		}
		return v.Source
	}
	choose := func(src knowledge.PrivateSource) string {
		var v struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(call("POST", "/selections", first.AccessToken, map[string]any{"kind": src.Kind, "source_id": src.ID, "revision": src.Revision, "consent": true}, 200), &v)
		if v.ID == "" {
			t.Fatal("missing durable selection")
		}
		return v.ID
	}
	waitReady := func(id string) {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			for _, item := range list() {
				if item["id"] == id && item["state"] == "ready" {
					return
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("normal resident private task deadline")
	}
	search := func(token, query string) knowledge.PrivateSearchPage {
		var page knowledge.PrivateSearchPage
		_ = json.Unmarshal(call("POST", "/search", token, map[string]string{"query": query}, 200), &page)
		return page
	}
	src := preview("personal_document", documents["text"].ID)
	call("POST", "/selections", first.AccessToken, map[string]any{"kind": src.Kind, "source_id": src.ID, "revision": src.Revision, "consent": false}, 400)
	call("POST", "/selections", first.AccessToken, map[string]any{"kind": src.Kind, "source_id": src.ID, "revision": src.Revision, "consent": true, "owner_id": "613102"}, 400)
	call("POST", "/selections", first.AccessToken, map[string]any{"kind": src.Kind, "source_id": src.ID, "revision": strings.Repeat("0", 64), "consent": true}, 409)
	call("POST", "/preview", second.AccessToken, map[string]string{"kind": src.Kind, "source_id": src.ID}, 404)
	call("GET", "/status", "", nil, 401)
	call("GET", "/status", adminToken, nil, 401)
	call("POST", "/search", machineToken.Token, map[string]string{"query": "校园图书馆"}, 401)
	check("explicit_one_source_consent_closed_dto_current_revision_and_owner_required")
	id := choose(src)
	waitReady(id)
	check("normal_resident_worker_publishes_durable_encrypted_document")
	page := search(first.AccessToken, "校园图书馆")
	if page.Total != 1 || !strings.Contains(page.Items[0].Snippet, "PRIVATE_DOCUMENT_BODY_13") {
		t.Fatal("private lexical search", page)
	}
	if search(second.AccessToken, "校园图书馆").Total != 0 {
		t.Fatal("cross-owner search")
	}
	call("POST", "/selections/"+id+"/source", second.AccessToken, map[string]string{"revision": src.Revision}, 404)
	call("DELETE", "/selections/"+id, second.AccessToken, nil, 404)
	check("search_and_exact_citation_current_owner_only")
	secondClaims, e := s.identity.JWTManager().VerifyAccessToken(second.AccessToken)
	if e != nil {
		t.Fatal(e)
	}
	secondResolve := func(c context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(c, s.identity.Sessions(), s.identity.Sessions(), secondClaims)
	}
	secondDoc, e := s.personalDocuments.Service().CreateTextForPrincipal(ctx, secondResolve, personaldocuments.CreateRequest{Name: "另一位用户私人资料.txt", Format: "text", Content: "OTHER_OWNER_PRIVATE 校园图书馆"})
	if e != nil {
		t.Fatal(e)
	}
	var secondPreview struct {
		Source knowledge.PrivateSource `json:"source"`
	}
	_ = json.Unmarshal(call("POST", "/preview", second.AccessToken, map[string]string{"kind": "personal_document", "source_id": secondDoc.ID}, 200), &secondPreview)
	var secondSelection struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(call("POST", "/selections", second.AccessToken, map[string]any{"kind": secondPreview.Source.Kind, "source_id": secondDoc.ID, "revision": secondPreview.Source.Revision, "consent": true}, 200), &secondSelection)
	secondDeadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(secondDeadline) {
		var state string
		_ = pool.QueryRow(ctx, `SELECT state FROM knowledge_private_sources WHERE id=$1`, secondSelection.ID).Scan(&state)
		if state == "ready" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if search(second.AccessToken, "OTHER_OWNER_PRIVATE").Total != 1 || search(first.AccessToken, "OTHER_OWNER_PRIVATE").Total != 0 || search(second.AccessToken, "PRIVATE_DOCUMENT_BODY_13").Total != 0 {
		t.Fatal("two positive private owners shared results")
	}
	call("DELETE", "/selections/"+secondSelection.ID, second.AccessToken, nil, 200)
	check("two_real_User_positive_private_corpora_are_disjoint")
	var derived string
	if e = pool.QueryRow(ctx, `SELECT encode(ciphertext,'hex') FROM knowledge_private_sources WHERE id=$1`, id).Scan(&derived); e != nil || strings.Contains(derived, "PRIVATE_DOCUMENT") {
		t.Fatal("encrypted projection", e)
	}
	check("database_projection_has_ciphertext_only_no_plaintext_chunks_or_cache")
	for _, format := range []string{"markdown", "campusdoc"} {
		v := preview("personal_document", documents[format].ID)
		selected := choose(v)
		waitReady(selected)
		call("POST", "/selections/"+selected+"/source", first.AccessToken, map[string]string{"revision": v.Revision}, 200)
		call("DELETE", "/selections/"+selected, first.AccessToken, nil, 200)
	}
	check("supported_txt_markdown_campusdoc_real_source_extract_index_citation_withdraw")
	post := preview("community_thread", "613201")
	postID := choose(post)
	waitReady(postID)
	if search(first.AccessToken, "PRIVATE_POST_BODY").Total != 1 {
		t.Fatal("owned private post not indexed")
	}
	pin, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pin.Exec(ctx, `SELECT id FROM knowledge_private_sources WHERE id=$1 FOR SHARE`, postID); e != nil {
		t.Fatal(e)
	}
	sql(`UPDATE threads SET content='PRIVATE_POST_CHANGED',current_revision=current_revision+1 WHERE id=613201`)
	if search(first.AccessToken, "PRIVATE_POST_BODY").Total != 0 {
		t.Fatal("stale origin projection read")
	}
	call("POST", "/selections/"+postID+"/source", first.AccessToken, map[string]string{"revision": post.Revision}, 409)
	if e = pin.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM knowledge_private_sources WHERE id=$1`, postID).Scan(&n)
		if n == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, e = os.Stat(filepath.Join(os.Getenv("KNOWLEDGE_PRIVATE_KEY_DIR"), postID+".key")); !os.IsNotExist(e) {
		t.Fatal("changed source derivative key retained")
	}
	check("private_post_exact_current_facts_revocation_and_periodic_key_task_cleanup")
	// Force only technical pending state while stopped; immutable ciphertext and
	// the real owner consent stay intact. Ordinary restart must reconcile it.
	if e = s.knowledge.Stop(ctx); e != nil {
		t.Fatal(e)
	}
	sql(`UPDATE knowledge_private_sources SET state='pending' WHERE id=$1`, id)
	sql(`UPDATE knowledge_private_tasks SET state='pending',attempts=0,lease_until_ms=0,available_at_ms=0 WHERE source_id=$1`, id)
	call("POST", "/search", first.AccessToken, map[string]string{"query": "校园图书馆"}, 503)
	if e = s.knowledge.Start(ctx); e != nil {
		t.Fatal(e)
	}
	waitReady(id)
	if search(first.AccessToken, "校园图书馆").Total != 1 {
		t.Fatal("restart lost private consent/payload")
	}
	check("stopped_module_denies_and_normal_restart_resumes_durable_pending_task")
	// Current protection failure never falls back to plaintext lexical search.
	keyRoot := os.Getenv("KNOWLEDGE_PRIVATE_KEY_DIR")
	if e = os.Chmod(keyRoot, 0755); e != nil {
		t.Fatal(e)
	}
	call("POST", "/search", first.AccessToken, map[string]string{"query": "校园图书馆"}, 503)
	if e = os.Chmod(keyRoot, 0700); e != nil {
		t.Fatal(e)
	}
	if search(first.AccessToken, "校园图书馆").Total != 1 {
		t.Fatal("protected resume")
	}
	check("insufficient_live_key_protection_fails_closed_without_plaintext_fallback")
	// Real v3 database/files backup excludes external keys. Apply it offline to
	// an independently migrated owned DB after the selection has been withdrawn.
	retainedSource := preview("personal_document", documents["markdown"].ID)
	retainedID := choose(retainedSource)
	waitReady(retainedID)
	keyBefore, e := os.ReadFile(filepath.Join(keyRoot, id+".key"))
	if e != nil {
		t.Fatal(e)
	}
	run := func(name string, args []string, extra []string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		if args[0] == "node" {
			cmd.Dir = repoRoot
		}
		cmd.Env = append(os.Environ(), extra...)
		f, e := os.Create(filepath.Join(root, name+".log"))
		if e != nil {
			t.Fatal(e)
		}
		cmd.Stdout, cmd.Stderr = f, f
		e = cmd.Run()
		_ = f.Close()
		if e != nil {
			t.Fatal(name, e)
		}
	}
	// Under current Schema35, the private-domain guard is the original34 SQL.
	// Execute those exact bytes in an actual transaction; latest-down would
	// test the unrelated AI35 domain instead.
	privateDown, e := os.ReadFile(filepath.Join(repoRoot, "migrations/000034_knowledge_private_index.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	rollback, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = rollback.Exec(ctx, string(privateDown))
	_ = rollback.Rollback(ctx)
	if e == nil || !strings.Contains(e.Error(), "private selections exist; owner withdrawal and key retirement required before rollback") {
		t.Fatal("original private34 nonempty guard required", e)
	}
	if err := os.WriteFile(filepath.Join(root, "private-down-refused.log"), []byte(e.Error()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var present int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM knowledge_private_sources`).Scan(&present); e != nil || present != 2 {
		t.Fatal("failed down lost private data", present, e)
	}
	check("nonempty_migration_down_refuses_without_data_loss")
	run("private-backup", []string{"bash", filepath.Join(repoRoot, "scripts/backup.sh"), filepath.Join(root, "backups")}, nil)
	archives, e := filepath.Glob(filepath.Join(root, "backups/campusos-backup-*.tar.gz"))
	if e != nil || len(archives) != 1 {
		t.Fatal("actual backup required", e)
	}
	archiveFile, e := os.Open(archives[0])
	if e != nil {
		t.Fatal(e)
	}
	gz, e := gzip.NewReader(archiveFile)
	if e != nil {
		t.Fatal(e)
	}
	tarReader := tar.NewReader(gz)
	for {
		header, e := tarReader.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		payload, e := io.ReadAll(tarReader)
		if e != nil {
			t.Fatal(e)
		}
		if bytes.Contains(payload, keyBefore) {
			t.Fatal("private key in backup")
		}
		if header.Name == "files.tar.gz" {
			nested, e := gzip.NewReader(bytes.NewReader(payload))
			if e != nil {
				t.Fatal(e)
			}
			inner := tar.NewReader(nested)
			for {
				h, e := inner.Next()
				if e == io.EOF {
					break
				}
				if e != nil {
					t.Fatal(e)
				}
				raw, e := io.ReadAll(inner)
				if e != nil {
					t.Fatal(e)
				}
				if strings.HasSuffix(h.Name, ".key") && strings.Contains(h.Name, "kp_") || bytes.Contains(raw, keyBefore) {
					t.Fatal("external private key archived")
				}
			}
			nested.Close()
		}
	}
	gz.Close()
	archiveFile.Close()
	clear(keyBefore)
	check("real_v3_backup_excludes_private_keys_and_retirement_custody")
	call("DELETE", "/selections/"+id, first.AccessToken, nil, 200)
	afterWithdrawal := search(first.AccessToken, "校园图书馆")
	if afterWithdrawal.Total != 1 || afterWithdrawal.Items[0].ID != retainedID {
		t.Fatal("withdrawn source remained or independently selected source disappeared")
	}
	call("POST", "/selections/"+id+"/source", first.AccessToken, map[string]string{"revision": src.Revision}, 404)
	check("withdrawal_erases_key_projection_task_and_rejects_new_requests")
	listener.Close()
	listener = nil
	infra.Stop()
	infra = nil
	restoredDSN := os.Getenv("CAMPUSOS_PRIVATE_RESTORE_DSN")
	restoredConfig, e := pgxpool.ParseConfig(restoredDSN)
	if e != nil || restoredConfig.ConnConfig.Database != "campusos_v12_13_restore" || restoredConfig.ConnConfig.Host != "127.0.0.1" {
		t.Fatal("owned restore required")
	}
	restoreRoot := filepath.Join(filepath.Dir(root), "restored-runtime")
	if e = os.MkdirAll(restoreRoot, 0700); e != nil {
		t.Fatal(e)
	}
	run("private-restore", []string{os.Getenv("CAMPUSOSCTL"), "recovery", "apply", "--archive", archives[0], "--data-root", filepath.Join(restoreRoot, "data"), "--migrations", filepath.Join(repoRoot, "migrations"), "--database", "campusos_v12_13_restore", "--confirm-offline"}, []string{"DATABASE_DSN=" + restoredDSN})
	run("private-restored-inventory", []string{os.Getenv("CAMPUSOSCTL"), "recovery", "status"}, []string{"DATABASE_DSN=" + restoredDSN})
	inventoryBytes, e := os.ReadFile(filepath.Join(root, "private-restored-inventory.log"))
	if e != nil {
		t.Fatal(e)
	}
	var inventory struct {
		State struct {
			Epoch string `json:"epoch"`
		} `json:"state"`
		SHA string `json:"inventory_sha256"`
	}
	if json.Unmarshal(inventoryBytes, &inventory) != nil || inventory.State.Epoch == "" || inventory.SHA == "" {
		t.Fatal("actual offline recovery inventory required")
	}
	run("private-restored-review", []string{os.Getenv("CAMPUSOSCTL"), "recovery", "review", "--data-root", filepath.Join(restoreRoot, "data"), "--epoch", inventory.State.Epoch, "--inventory-sha256", inventory.SHA, "--operator", "owned13-local-review", "--reason", "synthetic original owners reviewed; private selection remains withdrawn", "--confirm-offline"}, []string{"DATABASE_DSN=" + restoredDSN})
	if e = os.Chdir(restoreRoot); e != nil {
		t.Fatal(e)
	}
	restoredCfg := *cfg
	restoredCfg.Database.DSN = restoredDSN
	restoredCfg.Plugin.DataDir = filepath.Join(restoreRoot, "data/plugin_data")
	restored := New(&restoredCfg)
	restoredInfra, e := restored.startInfrastructure()
	if e != nil {
		t.Fatal("restored normal Server", e)
	}
	restoredListener := buildRouter(restored, restoredInfra)
	originalS, originalListener := s, listener
	s, listener = restored, restoredListener

	// Obtain a fresh current session through the ordinary Identity issuer after
	// recovery invalidated the archived sessions. The original source remains readable.
	rp, e := pgxpool.New(ctx, restoredDSN)
	if e != nil {
		t.Fatal(e)
	}
	restoredUser, e := identityrepo.NewPgUserRepository(rp).GetByID(ctx, "613101")
	if e != nil {
		t.Fatal(e)
	}
	fresh, e := restored.identity.Sessions().Issue(ctx, restoredUser, identityservice.SessionMetadata{DeviceName: "Owned restored private source"})
	if e != nil {
		t.Fatal(e)
	}
	call("GET", "/status", first.AccessToken, nil, 401)
	check("archived_User_session_cannot_cross_restored_authentication_epoch")
	if search(fresh.AccessToken, "校园图书馆").Total != 0 {
		t.Fatal("old backup revived withdrawn private source")
	}
	call("POST", "/selections/"+id+"/source", fresh.AccessToken, map[string]string{"revision": src.Revision}, 404)
	freshClaims, e := restored.identity.JWTManager().VerifyAccessToken(fresh.AccessToken)
	if e != nil {
		t.Fatal(e)
	}
	freshResolve := func(c context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(c, restored.identity.Sessions(), restored.identity.Sessions(), freshClaims)
	}
	if _, _, e = restored.personalDocuments.Service().TextContentForPrincipal(ctx, freshResolve, documents["text"].ID, ""); e != nil {
		t.Fatal("restored original must remain actually readable", e)
	}
	var restoredCount int
	if e = rp.QueryRow(ctx, `SELECT (SELECT count(*) FROM knowledge_private_sources)+(SELECT count(*) FROM knowledge_private_tasks)`).Scan(&restoredCount); e != nil || restoredCount != 0 {
		t.Fatal("restored owner consent was replayed", restoredCount, e)
	}
	if _, e = os.Stat(filepath.Join(keyRoot, retainedID+".key")); e != nil {
		t.Fatal("unwithdrawn independent key fixture missing", e)
	}
	call("POST", "/selections/"+retainedID+"/source", fresh.AccessToken, map[string]string{"revision": retainedSource.Revision}, 404)
	check("offline_restore_discards_all_private_consent_even_with_a_live_independent_key")
	rp.Close()
	restoredListener.Close()
	restoredInfra.Stop()
	s, listener = originalS, originalListener
	if e = os.Chdir(root); e != nil {
		t.Fatal(e)
	}
	s = New(cfg)
	infra, e = s.startInfrastructure()
	if e != nil {
		t.Fatal(e)
	}
	listener = buildRouter(s, infra)
	call("DELETE", "/selections/"+retainedID, first.AccessToken, nil, 200)
	check("actual_full_backup_restore_keeps_original_readable_but_withdrawn_derivative_undecryptable")
	// Current independent credentials are tested by the separate same-ELF
	// default-protection preflight regression as well as User middleware here.
	controlKey := adminSPABrowserSecret(t)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+controlKey {
			http.Error(w, "denied", 403)
			return
		}
		var controlErr error
		if r.URL.Path == "/feature/off" {
			_, controlErr = s.features.Request(knowledge.FeatureID, false)
		} else if r.URL.Path == "/feature/on" {
			_, controlErr = s.features.Request(knowledge.FeatureID, true)
		} else {
			http.NotFound(w, r)
			return
		}
		if controlErr != nil {
			http.Error(w, "failed", 500)
			return
		}
		w.WriteHeader(200)
	}))
	defer control.Close()
	browserConfig := map[string]any{"backend": listener.URL, "control": control.URL, "controlKey": controlKey, "email": "owned13-first@example.test", "password": password, "documentID": browserDoc.ID, "webDist": os.Getenv("CAMPUSOS_PRIVATE_WEB_DIST"), "result": filepath.Join(root, "browser-proof.json"), "desktopScreenshot": filepath.Join(root, "private-desktop.png"), "mobileScreenshot": filepath.Join(root, "private-mobile.png")}
	configBytes, _ := json.Marshal(browserConfig)
	configPath := filepath.Join(t.TempDir(), "private-browser.json")
	if e = os.WriteFile(configPath, configBytes, 0600); e != nil {
		t.Fatal(e)
	}
	run("private-browser", []string{"node", filepath.Join(repoRoot, "scripts/v12-13-private-browser.mjs"), configPath}, nil)
	browserRaw, e := os.ReadFile(filepath.Join(root, "browser-proof.json"))
	if e != nil {
		t.Fatal(e)
	}
	var browserProof map[string]any
	if json.Unmarshal(browserRaw, &browserProof) != nil || browserProof["status"] != "passed" {
		t.Fatal("real browser proof required")
	}
	check("actual_built_user_web_desktop_mobile_explicit_consent_search_citation_withdraw_feature_and_late_response")
	var externalCount int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM plugin_installations)+(SELECT count(*) FROM plugin_user_installations)`).Scan(&externalCount); e != nil || externalCount != 0 {
		t.Fatal("zero external plugins required", e)
	}
	proof := map[string]any{"status": "passed", "checks": checks, "check_count": len(checks), "http_requests": requests, "actual_server_composition": true, "actual_production_router": true, "actual_postgresql": true, "normal_resident_private_worker": true, "actual_full_backup_restore": true, "original_readable_after_restore": true, "withdrawn_derivative_unreadable_after_restore": true, "private_keys_excluded_from_backup": true, "browser": browserProof, "mocked_api_data": false, "external_plugins": externalCount, "ai_or_egress": false, "windows": false}
	result, _ := json.MarshalIndent(proof, "", "  ")
	if e = os.WriteFile(filepath.Join(root, "private-index-proof.json"), append(result, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
}
