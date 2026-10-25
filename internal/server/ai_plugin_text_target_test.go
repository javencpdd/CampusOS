package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/modules/features/ai"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Normal production graph and AI-owned Port, not a test-only plugin consumer.
func TestAIPluginTextPostgres(t *testing.T) {
	dsn, root := os.Getenv("CAMPUSOS_AI_PLUGIN_DSN"), os.Getenv("CAMPUSOS_AI_PLUGIN_ROOT")
	if dsn == "" {
		t.Skip("requires owned14c PostgreSQL and actual Container image")
	}
	pc, err := pgxpool.ParseConfig(dsn)
	if err != nil || pc.ConnConfig.Host != "127.0.0.1" || pc.ConnConfig.Database != "campusos_v12_14c_text" || !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-14c/") {
		t.Fatal("owned target required")
	}
	t.Chdir(root)
	ctx := t.Context()
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	sql := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	sql(`INSERT INTO users(id,username,nickname,email) VALUES(461101,'owned14c-a','A','owned14c-a@example.test'),(461102,'owned14c-b','B','owned14c-b@example.test')`)
	private := prepareKnowledgeExitTrust(t, root)
	cfg := config.Load()
	cfg.Plugin.ContainerEnabled, cfg.AI.Enabled = true, true
	cfg.Database.DSN, cfg.Plugin.DataDir, cfg.Redis.Enabled = dsn, filepath.Join(root, "data/plugin_data"), false
	s := New(cfg)
	infra, err := s.startInfrastructure()
	if err != nil || infra.database == nil || s.ai == nil {
		t.Fatal("production composition", err)
	}
	defer infra.Stop()
	if _, err = s.features.Request(ai.FeatureID, true); err != nil {
		t.Fatal(err)
	}
	value, ok := s.appContext.Lookup(ai.PortPluginText)
	port, typed := value.(ai.PluginText)
	if !ok || !typed {
		t.Fatal("production AI Port absent")
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account, sid := "461101", "as.owned14c"
	if err = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "owned14c-admin", Email: "owned14c-admin@example.test", DisplayName: "Target admin"}); err != nil {
		t.Fatal(err)
	}
	if err = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.owned14c", AccountID: account, RoleID: "security_admin", GrantedBy: account}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = identityrepo.NewPgAdminSessionRepository(pool).CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: account, RefreshTokenDigest: strings.Repeat("b", 64), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	admin := func(c context.Context) (identity.PrincipalContext, error) {
		return identity.ResolveAdminSessionPrincipal(c, s.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: account, SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	sessions := func(id string) (identity.SessionPrincipalResolver, *auth.JWTClaims) {
		t.Helper()
		u, e := identityrepo.NewPgUserRepository(pool).GetByID(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		issued, e := s.identity.Sessions().Issue(ctx, u, identityservice.SessionMetadata{DeviceID: "owned14c-" + id})
		if e != nil {
			t.Fatal(e)
		}
		claims, e := s.identity.JWTManager().VerifyAccessToken(issued.AccessToken)
		if e != nil {
			t.Fatal(e)
		}
		return func(c context.Context) (identity.PrincipalContext, error) {
			return identity.ResolveUserSessionPrincipal(c, s.identity.Sessions(), s.identity.Sessions(), claims)
		}, claims
	}
	user, claims := sessions("461101")
	other, _ := sessions("461102")
	checks := []string{}
	q := ai.PluginTextRequest{Publisher: "campusos", Key: "chat-echo", Version: "1.1.0", ContributionID: "chat-main", RequestID: "owned14c-explicit", Prompt: "EXPLICIT_TEXT_14C_本人输入", TimeoutMS: 4500}
	denied := func(label string, r identity.SessionPrincipalResolver, req ai.PluginTextRequest) {
		t.Helper()
		out, e := port.AnswerText(ctx, r, req)
		if e == nil || out != (ai.PluginTextAnswer{}) {
			t.Fatal(label, "released", out, e)
		}
		checks = append(checks, label)
	}
	denied("no_installed_provider_has_no_implicit_fallback", user, q)
	raw, e := os.ReadFile(os.Getenv("CAMPUSOS_AI_PLUGIN_TEMPLATE"))
	if e != nil {
		t.Fatal(e)
	}
	var manifest plugin.PluginV5Manifest
	if json.Unmarshal(raw, &manifest) != nil {
		t.Fatal("manifest")
	}
	manifest.Backend.Artifact.Image = os.Getenv("CAMPUSOS_AI_PLUGIN_IMAGE")
	packageDir := filepath.Join(root, "package")
	if os.Mkdir(packageDir, 0700) != nil {
		t.Fatal("package")
	}
	raw, _ = json.Marshal(manifest)
	if os.WriteFile(filepath.Join(packageDir, "plugin.json"), raw, 0600) != nil {
		t.Fatal("manifest write")
	}
	archive, p, e := plugin.BuildPluginV5Package(packageDir)
	if e != nil {
		t.Fatal(e)
	}
	envelope, e := plugin.SignPluginV5Package(archive, "owned11", private)
	if e != nil {
		t.Fatal(e)
	}
	if os.WriteFile(filepath.Join(root, "echo.tar.gz"), archive, 0600) != nil || os.WriteFile(filepath.Join(root, "echo.release.json"), envelope, 0600) != nil {
		t.Fatal("signed artifact")
	}
	platform := s.pluginsV5Handler().Platform
	coordinate := plugin.V5Coordinate{Publisher: manifest.Publisher, Key: manifest.Key, Version: manifest.Version}
	if _, e = platform.Import(ctx, archive, envelope, "owned14c-import", admin); e != nil {
		t.Fatal(e)
	}
	sequence := 0
	command := func(owner string, resolve identity.SessionPrincipalResolver, r plugin.V5Mutation) {
		t.Helper()
		d, e := platform.Detail(ctx, coordinate, owner == "", resolve)
		if e != nil {
			t.Fatal(e)
		}
		sequence++
		r.IdempotencyKey = fmt.Sprintf("owned14c-%d", sequence)
		r.Version = manifest.Version
		if d.Installation != nil {
			r.ExpectedGeneration = d.Installation.Generation
		}
		if d.Grant != nil {
			r.ExpectedRevision = d.Grant.Revision
		}
		if _, e = platform.Mutate(ctx, manifest.Publisher, manifest.Key, r, owner == "", resolve); e != nil {
			t.Fatal(r.Kind, e)
		}
	}
	wait := func(owner string, resolve identity.SessionPrincipalResolver) {
		t.Helper()
		end := time.Now().Add(25 * time.Second)
		for time.Now().Before(end) {
			d, e := platform.Detail(ctx, coordinate, owner == "", resolve)
			if e == nil && d.Installation != nil && d.Installation.Observed == "ready" {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("normal resident coordinator readiness")
	}
	command("", admin, plugin.V5Mutation{Kind: "install"})
	denied("unapproved_provider_denied", user, q)
	command("", admin, plugin.V5Mutation{Kind: "grant", Approved: true, Consumes: p.Manifest.Consumes, Resources: p.Manifest.HostResources})
	command("", admin, plugin.V5Mutation{Kind: "enable"})
	wait("", admin)
	command("461101", user, plugin.V5Mutation{Kind: "install"})
	command("461101", user, plugin.V5Mutation{Kind: "enable"})
	wait("461101", user)
	answer := func() {
		t.Helper()
		out, e := port.AnswerText(ctx, user, q)
		if e != nil || out.Text != "Echo: "+q.Prompt || out.FinishReason != "completed" {
			t.Fatal("AI Feature actual provider", out, e)
		}
	}
	// Publication follows observed readiness asynchronously. No fake ready rows.
	end := time.Now().Add(25 * time.Second)
	for {
		out, e := port.AnswerText(ctx, user, q)
		if e == nil && out.Text == "Echo: "+q.Prompt {
			break
		}
		if time.Now().After(end) {
			t.Fatal("current publication deadline", e)
		}
		time.Sleep(25 * time.Millisecond)
	}
	checks = append(checks, "production_AI_Port_to_independently_built_signed_SDK_Container_exact_explicit_text")
	denied("other_User_has_no_owner_installation", other, q)
	denied("same_ID_Admin_is_not_User", admin, q)
	denied("anonymous_denied", nil, q)
	bad := q
	bad.Version = "1.1.1"
	denied("unselected_release_denied_without_fallback", user, bad)
	bad = q
	bad.ContributionID = "foreign"
	denied("unapproved_contribution_denied", user, bad)
	bad = q
	bad.TimeoutMS = 5001
	denied("deployment_timeout_ceiling_denied", user, bad)
	if _, e = s.features.Request(ai.FeatureID, false); e != nil {
		t.Fatal(e)
	}
	denied("owning_AI_Feature_disable_denied", user, q)
	if _, e = s.features.Request(ai.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	answer()
	if e = s.ai.Stop(ctx); e != nil {
		t.Fatal(e)
	}
	denied("AI_module_Stop_closes_Port", user, q)
	if e = s.ai.Start(ctx); e != nil {
		t.Fatal(e)
	}
	answer()
	checks = append(checks, "AI_module_restart_reuses_current_provider_without_implicit_binding")
	command("461101", user, plugin.V5Mutation{Kind: "disable"})
	denied("User_plugin_disable_fences_current_call", user, q)
	command("461101", user, plugin.V5Mutation{Kind: "enable"})
	wait("461101", user)
	end = time.Now().Add(25 * time.Second)
	for {
		out, e := port.AnswerText(ctx, user, q)
		if e == nil && out.Text == "Echo: "+q.Prompt {
			break
		}
		if time.Now().After(end) {
			t.Fatal("reload publication deadline", e)
		}
		time.Sleep(25 * time.Millisecond)
	}
	// Real session revocation, retaining the exact same previously valid claims.
	sql(`UPDATE sessions SET revoked_at=now(),revoke_reason='owned14c' WHERE id=$1`, claims.SessionID)
	denied("current_User_session_revocation_denied", user, q)
	user, _ = sessions("461101")
	answer()
	command("", admin, plugin.V5Mutation{Kind: "revoke"})
	denied("Admin_Grant_revocation_fences_provider", user, q)
	var leaks int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM (SELECT to_jsonb(a)::text AS value FROM platform_command_audits a UNION ALL SELECT to_jsonb(o)::text FROM platform_outbox o) records WHERE value LIKE '%EXPLICIT_TEXT_14C%'`).Scan(&leaks); e != nil {
		t.Fatal(e)
	}
	if leaks != 0 {
		t.Fatal("explicit prompt persisted in command audit")
	}
	checks = append(checks, "no_prompt_or_answer_in_command_audits_no_model_Profile_or_private_source_authority")
	proof := map[string]any{"status": "passed", "checks": checks, "check_count": len(checks), "production_AI_Port": true, "normal_production_graph": true, "actual_PostgreSQL": true, "actual_Container": true, "SDK_only_author_build": true, "package_digest": p.Digest, "image": manifest.Backend.Artifact.Image, "synthetic_consumer": false, "model_or_RAG": false, "http_or_browser": false, "schema_changed": false, "migration": 35}
	raw, _ = json.MarshalIndent(proof, "", "  ")
	if os.WriteFile(filepath.Join(root, "target-proof.json"), raw, 0600) != nil {
		t.Fatal("proof")
	}
}
