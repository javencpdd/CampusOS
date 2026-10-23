package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/portadapt"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/projectaudit"
	"github.com/campusos/CampusOS/internal/transport/httpapi"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/observability"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Built Web assets and Chrome reach the ordinary production module graph/router
// and real PostgreSQL. Only the separately authenticated fixture controller is
// test-only; no public endpoint, retrieval Provider or response data is mocked.
func TestKnowledgePublicSearchActualBrowser(t *testing.T) {
	dsn, work := os.Getenv("CAMPUSOS_KNOWLEDGE_WEB_DSN"), os.Getenv("CAMPUSOS_KNOWLEDGE_WEB_ROOT")
	if dsn == "" {
		t.Skip("requires owned12 Web PostgreSQL/browser target")
	}
	parsed, e := pgxpool.ParseConfig(dsn)
	if e != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_12_web" || !filepath.IsAbs(work) || !strings.Contains(filepath.ToSlash(work), "/.cache/v12-12-public-search/") {
		t.Fatal("refuse non-owned12 target")
	}
	repo, e := projectaudit.FindRepositoryRoot(".")
	if e != nil {
		t.Fatal(e)
	}
	t.Chdir(work)
	ctx := t.Context()
	pool, e := pgxpool.NewWithConfig(ctx, parsed)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	sql := func(query string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	sql(`INSERT INTO users(id,username,nickname,email) VALUES(501101,'owned12-public','Owned12','owned12@example.test');
 INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(501110,'Owned12 group','owned12-group','group','active');
 INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(501111,'Owned12 board','owned12-board',501110,'board','active');
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(501111,'discussion',true);
 INSERT INTO personal_documents(id,owner_user_id,name,document_type) VALUES(501401,501101,'PRIVATE_DOCUMENT_12','text');`)
	for i := 1; i <= 23; i++ {
		sql(`INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status) VALUES($1,$2,$3,'markdown',501101,'Owned12',501111,'published','discussion','published','clear','active')`, 501200+i, fmt.Sprintf("图书馆资料%02d", i), fmt.Sprintf("PUBLIC_CAMPUS_BODY_%d\n<img src=\"/never-fetch-knowledge-image\" onerror=\"window.knowledgeInjected=true\">", 501200+i))
	}
	sql(`INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status) VALUES(501299,'图书馆私人资料','PRIVATE_THREAD_12','markdown',501101,'Owned12',501111,'private','discussion','private','clear','active')`)
	cfg := config.Load()
	cfg.Database.DSN = dsn
	cfg.Plugin.DataDir = filepath.Join(work, "data/plugin_data")
	cfg.Redis.Enabled = false
	s := New(cfg)
	infra, e := s.startInfrastructure()
	if e != nil || s.knowledge == nil {
		t.Fatal("normal Server required", e)
	}
	defer infra.Stop()
	pluginHandler, e := s.pluginHTTPHandler()
	if e != nil {
		t.Fatal(e)
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
	if e = s.identity.SyncRouteDescriptors(ctx, router.RouteDescriptors()); e != nil {
		t.Fatal(e)
	}
	api := httptest.NewServer(router)
	defer api.Close()
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account, sid := "adm.knowledge.web", "as.knowledge.web"
	if e = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "owned12-operator", Email: "owned12-operator@example.test", DisplayName: "Owned12"}); e != nil {
		t.Fatal(e)
	}
	if e = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.knowledge.web", AccountID: account, RoleID: "security_admin", GrantedBy: account}); e != nil {
		t.Fatal(e)
	}
	password := adminSPABrowserSecret(t)
	passwordHash, e := auth.HashPassword(password)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok, e := admins.SetCredentialCAS(ctx, account, passwordHash, 0, time.Now().UTC()); e != nil || !ok {
		t.Fatal("owned Admin credential", e)
	}
	now := time.Now().UTC()
	if e = identityrepo.NewPgAdminSessionRepository(pool).CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: account, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	resolve := func(ctx context.Context) (identity.PrincipalContext, error) {
		return identity.ResolveAdminSessionPrincipal(ctx, s.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: account, SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	controlKey := adminSPABrowserSecret(t)
	var primary, secondary knowledge.Collection
	seeded := false
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+controlKey {
			w.WriteHeader(403)
			return
		}
		var err error
		var out any = map[string]any{}
		switch r.URL.Path {
		case "/enable":
			_, err = s.features.Request(knowledge.FeatureID, true)
		case "/disable":
			_, err = s.features.Request(knowledge.FeatureID, false)
		case "/seed":
			if seeded {
				w.WriteHeader(409)
				return
			}
			seeded = true
			primary, err = s.knowledge.Collections().SaveCollection(r.Context(), knowledge.Collection{Title: "校园公开集合", Status: "active"}, resolve)
			if err == nil {
				for i := 1; i <= 23; i++ {
					var state knowledge.CollectionState
					state, err = s.knowledge.Collections().SaveSelection(r.Context(), primary.ID, primary.Version, knowledge.Selection{Kind: "thread", SourceID: strconv.Itoa(501200 + i), Active: true}, resolve)
					if err != nil {
						break
					}
					primary = state.Collection
				}
			}
			var official knowledge.Official
			if err == nil {
				official, err = s.knowledge.Collections().SaveOfficial(r.Context(), knowledge.Official{Title: "图书馆官方说明", Content: "OFFICIAL_PUBLIC_12", Format: "text", Status: "published"}, resolve)
			}
			if err == nil {
				var state knowledge.CollectionState
				state, err = s.knowledge.Collections().SaveSelection(r.Context(), primary.ID, primary.Version, knowledge.Selection{Kind: "official", SourceID: official.ID, Active: true}, resolve)
				primary = state.Collection
			}
			if err == nil {
				secondary, err = s.knowledge.Collections().SaveCollection(r.Context(), knowledge.Collection{Title: "官方说明集合", Status: "active"}, resolve)
			}
			if err == nil {
				_, err = s.knowledge.Collections().SaveSelection(r.Context(), secondary.ID, secondary.Version, knowledge.Selection{Kind: "official", SourceID: official.ID, Active: true}, resolve)
			}
			if err == nil {
				deadline := time.Now().Add(45 * time.Second)
				for {
					// A first readable generation can still have a resident cycle due.
					// Wait for both normal jobs to be idle with their schedule advanced;
					// never write job readiness or weaken current-generation checks.
					var ready int
					e := pool.QueryRow(r.Context(), `SELECT count(*) FROM knowledge_index_jobs j JOIN knowledge_collections c ON c.id=j.id WHERE j.id IN ($1,$2) AND j.state='idle' AND j.data->>'run_generation'='0' AND (j.data->>'ready_generation')::bigint>0 AND j.data->>'collection_version'=c.facts_version::text AND j.data->>'requested'=j.data->>'active_request' AND j.available_at_ms>$3`, primary.ID, secondary.ID, time.Now().Add(2*time.Second).UnixMilli()).Scan(&ready)
					if e == nil && ready == 2 {
						p, e := s.knowledge.Retrieval().SearchPublic(r.Context(), knowledge.PublicSearchRequest{CollectionID: primary.ID, Query: "图书馆", Page: 1, PageSize: 20})
						other, e2 := s.knowledge.Retrieval().SearchPublic(r.Context(), knowledge.PublicSearchRequest{CollectionID: secondary.ID, Query: "图书馆", Page: 1, PageSize: 20})
						if e == nil && e2 == nil && p.Total == 24 && other.Total == 1 {
							break
						}
					}
					if time.Now().After(deadline) {
						err = fmt.Errorf("normal indexer deadline")
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
			}
			out = map[string]any{"primary": primary.ID, "secondary": secondary.ID, "total": 24}
		case "/fail-job":
			var input struct {
				ID string `json:"id"`
			}
			if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&input) != nil || !strings.HasPrefix(input.ID, "kc_") || len(input.ID) != 35 {
				w.WriteHeader(400)
				return
			}
			deadline := time.Now().Add(30 * time.Second)
			for {
				result, e := pool.Exec(r.Context(), `UPDATE knowledge_index_jobs SET data=data || jsonb_build_object('state','dead','attempts',8,'last_error','page_failed','available_at_ms',($2::bigint)::text) WHERE id=$1 AND state='idle' AND available_at_ms>$3`, input.ID, time.Now().Add(time.Minute).UnixMilli(), time.Now().Add(time.Second).UnixMilli())
				if e != nil {
					err = e
					break
				}
				if result.RowsAffected() == 1 {
					break
				}
				if time.Now().After(deadline) {
					err = fmt.Errorf("owned job idle deadline")
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		case "/audit-fail-on":
			_, err = pool.Exec(r.Context(), `CREATE FUNCTION owned12_audit_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned12 audit fault'; END $$; CREATE TRIGGER owned12_audit_fail BEFORE INSERT ON platform_command_audits FOR EACH ROW WHEN (NEW.command_code='knowledge.index.retry') EXECUTE FUNCTION owned12_audit_fail()`)
		case "/audit-fail-off":
			_, err = pool.Exec(r.Context(), `DROP TRIGGER owned12_audit_fail ON platform_command_audits; DROP FUNCTION owned12_audit_fail()`)
		case "/revoke-manage":
			_, err = pool.Exec(r.Context(), `DELETE FROM admin_role_permissions WHERE role_id='security_admin' AND permission_code='knowledge.collection.manage'`)
		case "/restore-manage":
			_, err = pool.Exec(r.Context(), `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES('security_admin','knowledge.collection.manage','owned12') ON CONFLICT DO NOTHING`)
		case "/revise", "/private":
			var input struct {
				ID string `json:"id"`
			}
			if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&input) != nil {
				w.WriteHeader(400)
				return
			}
			id, e := strconv.Atoi(input.ID)
			if e != nil || id < 501201 || id > 501223 {
				w.WriteHeader(400)
				return
			}
			if r.URL.Path == "/private" {
				_, err = pool.Exec(r.Context(), `UPDATE threads SET publication_status='private',status='private',content='PRIVATE_CHANGED_12',updated_at=clock_timestamp() WHERE id=$1`, id)
			} else {
				_, err = pool.Exec(r.Context(), `UPDATE threads SET content='PUBLIC_REVISION_NEW_12',updated_at=clock_timestamp() WHERE id=$1`, id)
			}
		default:
			w.WriteHeader(404)
			return
		}
		if err != nil {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer controller.Close()
	scratch := t.TempDir()
	privateConfig := filepath.Join(scratch, "browser-config.json")
	wire, _ := json.Marshal(map[string]any{"backend": api.URL, "control": controller.URL, "controlKey": controlKey, "result": filepath.Join(work, "browser-result.json"), "evidence": work, "webDist": os.Getenv("CAMPUSOS_KNOWLEDGE_WEB_DIST"), "adminDist": os.Getenv("CAMPUSOS_KNOWLEDGE_ADMIN_DIST"), "adminUsername": "owned12-operator", "password": password})
	if os.WriteFile(privateConfig, wire, 0600) != nil {
		t.Fatal("private browser configuration")
	}
	nodeCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	node := exec.CommandContext(nodeCtx, "node", filepath.Join(repo, "scripts/v12-12-public-search-browser.mjs"), privateConfig)
	node.Dir = repo
	node.Env = append(os.Environ(), "TMPDIR=/tmp")
	output, err := node.CombinedOutput()
	if os.WriteFile(filepath.Join(work, "browser.log"), output, 0600) != nil {
		t.Fatal("browser log")
	}
	if err != nil {
		t.Fatal("real Web browser failed; inspect retained browser.log", err)
	}
	data, err := os.ReadFile(filepath.Join(work, "browser-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var proof struct {
		Status string   `json:"status"`
		Checks []string `json:"checks"`
		Mocked bool     `json:"mocked_api_data"`
	}
	if json.Unmarshal(data, &proof) != nil || proof.Status != "passed" || len(proof.Checks) < 12 || proof.Mocked {
		t.Fatal("incomplete browser proof")
	}
	assertKnowledgeDiagnosticLockOrder(t, s, pool, resolve)
	assertKnowledgeIndexMetrics(t, s, pool, infra.metrics)
	var audits int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM platform_command_audits WHERE command_code='knowledge.index.retry' AND actor_type='admin' AND actor_id=$1`, account).Scan(&audits); e != nil || audits != 1 {
		t.Fatal("actual typed retry audit", audits, e)
	}
	var n int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM personal_documents WHERE id=501401`).Scan(&n); err != nil || n != 1 {
		t.Fatal("private document fixture missing")
	}
}

// Hold the job row and wait until the real diagnostics transaction is blocked
// there. It must not already hold member rows needed by an executing index page.
func assertKnowledgeDiagnosticLockOrder(t *testing.T, s *Server, pool *pgxpool.Pool, resolve identity.SessionPrincipalResolver) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var id string
	if e := pool.QueryRow(ctx, `SELECT id FROM knowledge_collections WHERE data->>'title'='Admin12图书馆集合'`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	blocker, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	var blockerPID int
	if e = blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); e != nil {
		t.Fatal(e)
	}
	if _, e = blocker.Exec(ctx, `SELECT id FROM knowledge_index_jobs WHERE id=$1 FOR UPDATE`, id); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		d, e := s.knowledge.Collections().IndexDiagnostics(ctx, id, resolve)
		if e == nil && d.CollectionID != id {
			e = fmt.Errorf("diagnostic identity mismatch")
		}
		done <- e
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND $1=ANY(pg_blocking_pids(pid)) AND query LIKE 'SELECT data,facts_version FROM knowledge_index_jobs%' AND query LIKE '%FOR SHARE')`, blockerPID).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case e := <-done:
			t.Fatal("diagnostics did not wait on job", e)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("diagnostic job barrier deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	probe, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = probe.Exec(ctx, `SELECT id FROM knowledge_collection_sources WHERE collection_id=$1 FOR UPDATE NOWAIT`, id)
	_ = probe.Rollback(ctx)
	if e != nil {
		t.Fatal("diagnostics took member locks before job; inverse executor lock order", e)
	}
	if e = blocker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal("diagnostic release deadline")
	}
	t.Log("actual PostgreSQL diagnostic collection-job-member lock order passed")
}

func assertKnowledgeIndexMetrics(t *testing.T, s *Server, pool *pgxpool.Pool, meter interface{ Snapshot() observability.Snapshot }) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// Advance a genuine idle job's schedule, never its ready state/generation.
	// The ordinary resident loop must claim and execute the next page itself.
	deadline := time.Now().Add(5 * time.Second)
	for {
		tag, e := pool.Exec(ctx, `UPDATE knowledge_index_jobs SET data=data || jsonb_build_object('available_at_ms',($1::bigint)::text) WHERE id=(SELECT id FROM knowledge_collections WHERE data->>'title'='Admin12图书馆集合') AND state='idle'`, time.Now().Add(-2*time.Second).UnixMilli())
		if e != nil {
			t.Fatal(e)
		}
		if tag.RowsAffected() == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle telemetry job deadline")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for {
		seen := map[string]bool{}
		for _, metric := range meter.Snapshot().Metrics {
			switch metric.Name {
			case "campusos_knowledge_index_pages_total":
				seen[metric.Name] = metric.Value > 0
			case "campusos_knowledge_index_page_duration_seconds", "campusos_knowledge_index_schedule_lag_seconds":
				seen[metric.Name] = metric.Count > 0
			default:
				continue
			}
			for key := range metric.Labels {
				if key != "result" && key != "state" {
					t.Fatal("unsafe Knowledge metric label")
				}
			}
		}
		if len(seen) == 3 && seen["campusos_knowledge_index_pages_total"] && seen["campusos_knowledge_index_page_duration_seconds"] && seen["campusos_knowledge_index_schedule_lag_seconds"] {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("actual normal meter binding/page/lag missing")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Log("actual normal resident index page/state-delay metrics passed")
}
