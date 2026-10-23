package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/modules/features/personaldocuments"
	platformroute "github.com/campusos/CampusOS/internal/platform/route"
	"github.com/campusos/CampusOS/internal/transport/httpapi"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This target runs ordinary production modules, current PostgreSQL-backed User
// sessions and the production router. It never installs a private parser or
// protected index and never substitutes fake authentication or endpoint data.
func TestKnowledgePrivatePreflightPostgres(t *testing.T) {
	dsn, root := os.Getenv("CAMPUSOS_KNOWLEDGE_PRIVATE_DSN"), os.Getenv("CAMPUSOS_KNOWLEDGE_PRIVATE_ROOT")
	if dsn == "" {
		t.Skip("requires owned13 private-preflight PostgreSQL target")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_13_private" || !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-13-private-preflight/") {
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
	sql(`INSERT INTO users(id,username,nickname,email) VALUES(513101,'owned13-first','PRIVATE_OWNER_13','owned13-first@example.test'),(513102,'owned13-second','PRIVATE_OTHER_OWNER_13','owned13-second@example.test')`)
	cfg := config.Load()
	cfg.Database.DSN, cfg.Plugin.DataDir, cfg.Redis.Enabled = dsn, filepath.Join(root, "data/plugin_data"), false
	s := New(cfg)
	infra, err := s.startInfrastructure()
	if err != nil || s.knowledge == nil || s.personalDocuments == nil {
		t.Fatal("normal Server composition required", err)
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
	const path = "/api/v1/knowledge/private/sources/preflight"
	found := false
	for _, descriptor := range router.RouteDescriptors() {
		if descriptor.Path == path {
			if descriptor.Method != http.MethodPost || descriptor.Audience != platformroute.AudienceAuthenticated || descriptor.FeatureID != knowledge.FeatureID || descriptor.PermissionCode != "" {
				t.Fatal("private preflight route authority drift", descriptor)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("normal production private preflight route missing")
	}
	if err = s.identity.SyncRouteDescriptors(ctx, router.RouteDescriptors()); err != nil {
		t.Fatal(err)
	}
	listener := httptest.NewServer(router)
	defer listener.Close()
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
	first, second := issue("513101"), issue("513102")
	claims, err := s.identity.JWTManager().VerifyAccessToken(first.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(current context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(current, s.identity.Sessions(), s.identity.Sessions(), claims)
	}
	documents := map[string]personaldocuments.DocumentDetail{}
	for _, format := range []string{"text", "markdown", "campusdoc"} {
		content := "PRIVATE_DOCUMENT_BODY_13"
		if format == "campusdoc" {
			content = `{"version":1,"blocks":[{"type":"paragraph","text":"PRIVATE_DOCUMENT_BODY_13"}]}`
		}
		extension := map[string]string{"text": "txt", "markdown": "md", "campusdoc": "campusdoc"}[format]
		d, e := s.personalDocuments.Service().CreateTextForPrincipal(ctx, resolve, personaldocuments.CreateRequest{Name: "PRIVATE_DOCUMENT_NAME_13." + extension, Format: format, Content: content})
		if e != nil {
			t.Fatal("ordinary checked document fixture", e)
		}
		documents[format] = d
	}
	pdf := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF")
	var office bytes.Buffer
	writer := zip.NewWriter(&office)
	for _, entry := range []string{"[Content_Types].xml", "word/document.xml"} {
		file, e := writer.Create(entry)
		if e != nil {
			t.Fatal(e)
		}
		content := "<document>PRIVATE_BINARY_13</document>"
		if entry == "[Content_Types].xml" {
			content = `<Types><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`
		}
		if _, e = io.WriteString(file, content); e != nil {
			t.Fatal(e)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		format, mime string
		body         []byte
	}{{"pdf", "application/pdf", pdf}, {"docx", "application/zip", office.Bytes()}} {
		d, e := s.personalDocuments.Service().UploadForPrincipal(ctx, resolve, "PRIVATE_DOCUMENT_13."+fixture.format, fixture.format, fixture.mime, int64(len(fixture.body)), bytes.NewReader(fixture.body))
		if e != nil {
			t.Fatal(e)
		}
		documents[fixture.format] = d
	}
	// A real independent Admin account/session with the same textual ID must
	// still fail the User JWT boundary.
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account, adminSID := "513101", "as.knowledge.private"
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
	// Compare complete business/index rows, original metadata and audit/outbox
	// row counts around every request. The independent resident backfiller's
	// periodic global checkpoint is deliberately outside this business proof.
	snapshot := func() map[string]string {
		t.Helper()
		result := map[string]string{}
		for _, table := range []string{"knowledge_collections", "knowledge_official_sources", "knowledge_official_revisions", "knowledge_source_selections", "knowledge_collection_sources", "knowledge_index_tasks", "knowledge_index_jobs", "knowledge_index_documents", "personal_documents", "personal_document_versions", "storage_objects"} {
			var digest string
			if e := pool.QueryRow(ctx, "SELECT md5(COALESCE(string_agg(to_jsonb(t)::text,'' ORDER BY to_jsonb(t)::text),'')) FROM "+table+" t").Scan(&digest); e != nil {
				t.Fatal(e)
			}
			result[table] = digest
		}
		for _, table := range []string{"platform_command_audits", "authorization_audits", "platform_outbox"} {
			var count int64
			if e := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil {
				t.Fatal(e)
			}
			result[table] = fmt.Sprint(count)
		}
		return result
	}
	files := func() map[string]string {
		t.Helper()
		result := map[string]string{}
		storageRoot := filepath.Join(root, "data/personal-space")
		if e := filepath.WalkDir(storageRoot, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(raw)
			rel, err := filepath.Rel(storageRoot, path)
			if err != nil {
				return err
			}
			result[filepath.ToSlash(rel)] = hex.EncodeToString(digest[:])
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		return result
	}
	checks := []string{}
	requests := 0
	request := func(name, token, body, query string, want int, code string) json.RawMessage {
		t.Helper()
		beforeRows, beforeFiles := snapshot(), files()
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, listener.URL+path+query, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		req.Header.Set("X-Actor-ID", "513102")
		req.Header.Set("X-Claimed-Role", "admin")
		res, e := (&http.Client{Timeout: 8 * time.Second}).Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if e != nil {
			t.Fatal(e)
		}
		requests++
		if res.StatusCode != want || !strings.Contains(res.Header.Get("Cache-Control"), "no-store") {
			t.Fatalf("%s status=%d cache=%q want=%d body=%s", name, res.StatusCode, res.Header.Get("Cache-Control"), want, raw)
		}
		var envelope struct {
			Code  int             `json:"code"`
			Data  json.RawMessage `json:"data"`
			Error *struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &envelope) != nil {
			t.Fatal("malformed envelope")
		}
		if want == 200 {
			var data map[string]json.RawMessage
			if envelope.Code != 0 || envelope.Error != nil || json.Unmarshal(envelope.Data, &data) != nil || len(data) != 5 || string(data["can_index"]) != "false" {
				t.Fatal("unsafe readiness result", string(raw))
			}
		} else if envelope.Error == nil || envelope.Error.Code != code || len(envelope.Data) != 0 {
			t.Fatal("denial contract", name, string(raw))
		}
		for _, forbidden := range []string{"PRIVATE_DOCUMENT_NAME_13", "PRIVATE_DOCUMENT_BODY_13", "PRIVATE_OWNER_13", "PRIVATE_OTHER_OWNER_13", "PRIVATE_BINARY_13", "source_object_id", "owner_id", "storage_key", "nonce", "ciphertext"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatal("private metadata/body leak", name)
			}
		}
		if !reflect.DeepEqual(beforeRows, snapshot()) || !reflect.DeepEqual(beforeFiles, files()) {
			t.Fatal("preflight mutated business/index/audit/outbox/files", name)
		}
		checks = append(checks, name)
		return envelope.Data
	}
	body := func(d personaldocuments.DocumentDetail) string {
		raw, e := json.Marshal(map[string]string{"document_id": d.ID, "version_id": d.CurrentVersionID})
		if e != nil {
			t.Fatal(e)
		}
		return string(raw)
	}
	bearer := "Bearer " + first.AccessToken
	for _, format := range []string{"text", "markdown", "campusdoc", "pdf", "docx"} {
		d := documents[format]
		raw := request("current_owner_"+format, bearer, body(d), "", 200, "")
		var data struct{ DocumentID, VersionID, Format, Reason string }
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		_ = json.Unmarshal(fields["document_id"], &data.DocumentID)
		_ = json.Unmarshal(fields["version_id"], &data.VersionID)
		_ = json.Unmarshal(fields["format"], &data.Format)
		_ = json.Unmarshal(fields["reason"], &data.Reason)
		reason := "private_storage_unavailable"
		if format == "pdf" || format == "docx" {
			reason = "unsupported_format"
		}
		if data.DocumentID != d.ID || data.VersionID != d.CurrentVersionID || data.Format != format || data.Reason != reason {
			t.Fatal("current exact readiness mismatch", string(raw))
		}
	}
	d := documents["text"]
	request("anonymous_denied", "", body(d), "", 401, "auth.required")
	request("different_owner_denied", "Bearer "+second.AccessToken, body(d), "", 404, "resource.not_found")
	request("same_id_admin_denied", "Bearer "+adminToken, body(d), "", 401, "auth.invalid_token")
	request("machine_scheme_denied", "Integration "+machineToken.Token, body(d), "", 401, "auth.required")
	request("machine_bearer_denied", "Bearer "+machineToken.Token, body(d), "", 401, "auth.invalid_token")
	request("missing_document_opaque", bearer, `{"document_id":"513999","version_id":"513998"}`, "", 404, "resource.not_found")
	oldBody := body(d)
	d, err = s.personalDocuments.Service().SaveForPrincipal(ctx, resolve, d.ID, personaldocuments.SaveRequest{ExpectedVersion: d.Version, Content: "PRIVATE_DOCUMENT_BODY_UPDATED_13"})
	if err != nil {
		t.Fatal(err)
	}
	request("old_version_conflict", bearer, oldBody, "", 409, "resource.conflict")
	request("new_current_version_denied_index", bearer, body(d), "", 200, "")
	if _, err = s.personalDocuments.Service().SetStatusForPrincipal(ctx, resolve, d.ID, d.Version, personaldocuments.StatusTrashed); err != nil {
		t.Fatal(err)
	}
	request("trashed_source_denied", bearer, body(d), "", 404, "resource.not_found")
	d = documents["markdown"]
	for _, feature := range []string{personaldocuments.FeatureID, knowledge.FeatureID} {
		if _, err = s.features.Request(feature, false); err != nil {
			t.Fatal(err)
		}
		request(feature+"_hot_disabled", bearer, body(d), "", 503, "knowledge.disabled")
		if _, err = s.features.Request(feature, true); err != nil {
			t.Fatal(err)
		}
		request(feature+"_restored_still_no_index", bearer, body(d), "", 200, "")
	}
	for i, invalid := range []string{`{}`, `{"document_id":"0513101","version_id":"1"}`, `{"document_id":513101,"version_id":"1"}`, `{"document_id":"1","document_id":"2","version_id":"3"}`, `{"document_id":"1","version_id":null}`, `{"document_id":"1","version_id":"2","owner_id":"513101"}`, `{"document_id":"1","version_id":"2","protected":true}`, `{"document_id":"1","version_id":"2","consent":true}`} {
		request(fmt.Sprintf("closed_invalid_%d", i), bearer, invalid, "", 400, "request.invalid")
	}
	request("query_override_denied", bearer, body(d), "?owner_id=513101", 400, "request.invalid")
	// Remove just this owned fixture's bytes temporarily. Checked metadata is
	// still current, so an unchanged result proves this endpoint never opens,
	// parses or persists the original. Restoration occurs before return.
	var key string
	if err = pool.QueryRow(ctx, `SELECT storage_key FROM storage_objects WHERE id=$1`, d.CurrentVersion.SourceObjectID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "data/personal-space", "513101", "file", "objects", key)
	parked := filepath.Join(root, "owned-original-parked")
	if err = os.Rename(source, parked); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if e := os.Rename(parked, source); e != nil {
				t.Fatal(e)
			}
		}()
		request("metadata_only_without_original_bytes", bearer, body(d), "", 200, "")
	}()
	if err = s.identity.Sessions().RevokeCurrent(ctx, "513101", first.Session.ID); err != nil {
		t.Fatal(err)
	}
	request("current_session_revoked", bearer, body(d), "", 401, "auth.invalid_token")
	var privateRows int64
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM knowledge_collections)+(SELECT count(*) FROM knowledge_source_selections)+(SELECT count(*) FROM knowledge_collection_sources)+(SELECT count(*) FROM knowledge_index_tasks)+(SELECT count(*) FROM knowledge_index_jobs)+(SELECT count(*) FROM knowledge_index_documents)`).Scan(&privateRows); err != nil || privateRows != 0 {
		t.Fatal("private source was implicitly indexed", privateRows, err)
	}
	proof := map[string]any{"schema": "campusos.v12-13-private-preflight-target/v1", "status": "passed", "check_count": len(checks), "checks": checks, "http_requests": requests, "actual_server_composition": true, "actual_production_router": true, "actual_loopback_http": true, "actual_postgresql": true, "actual_user_sessions": true, "actual_same_id_independent_admin": true, "actual_integration_credential": true, "mocked_api_or_identity": false, "private_index_enabled": false, "original_bytes_required": false, "private_projection_rows": privateRows, "business_index_task_audit_outbox_files_unchanged_per_request": true, "independent_global_backfill_checkpoint_excluded": true, "browser_or_standalone_cmd_server": false}
	raw, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "target-proof.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
