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
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestKnowledgeCollectionsProductionPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_EXIT_DSN")
	if dsn == "" {
		t.Skip("requires owned09a exit PostgreSQL")
	}
	parsed, e := pgxpool.ParseConfig(dsn)
	if e != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_09a_exit" {
		t.Fatal("refuse non-owned09a exit database")
	}
	root := os.Getenv("CAMPUSOS_KNOWLEDGE_EXIT_ROOT")
	if !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-09a-exit/") {
		t.Fatal("owned09a exit root required")
	}
	t.Chdir(root)
	cfg := config.Load()
	cfg.Database.DSN = dsn
	cfg.Plugin.DataDir = filepath.Join(root, "data/plugin_data")
	cfg.Redis.Enabled = false
	s := New(cfg)
	infra, e := s.startInfrastructure()
	if e != nil {
		t.Fatal(e)
	}
	defer infra.Stop()
	pool := infra.database
	ctx := t.Context()
	svc := s.knowledge.Collections()
	if pool == nil || svc == nil {
		t.Fatal("actual PostgreSQL Knowledge composition required")
	}
	checks := []string{}
	check := func(name string) { checks = append(checks, name) }
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = svc.PublicCollections(ctx); !errors.Is(e, knowledge.ErrDisabled) {
		t.Fatal("default-off", e)
	}
	check("Knowledge public collection default-off")
	if _, e = s.features.Request(knowledge.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account := "adm.knowledge.native"
	if e = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "knowledge-native", Email: "knowledge-native@example.test", DisplayName: "09a native"}); e != nil {
		t.Fatal(e)
	}
	if e = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.knowledge.native", AccountID: account, RoleID: "security_admin", GrantedBy: account}); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	sessions := identityrepo.NewPgAdminSessionRepository(pool)
	sid := "as.knowledge.native"
	if e = sessions.CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: account, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	resolver := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, s.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: account, SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	c, e := svc.SaveCollection(ctx, knowledge.Collection{Title: "Native public knowledge", Status: "active"}, resolver)
	if e != nil {
		t.Fatal(e)
	}
	check("current independent Admin MFA and exact management code create collection")
	exec(`INSERT INTO users(id,username,nickname,email) VALUES(410101,'owned09a-exit-alice','Alice','owned09a-exit-alice@example.test'),(410102,'owned09a-exit-bob','Bob','owned09a-exit-bob@example.test');
 INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(410110,'09a group','owned09a-exit-group','group','active');
 INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(410111,'09a board','owned09a-exit-board',410110,'board','active'),(410112,'09a moved','owned09a-exit-moved',410110,'board','active');
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(410111,'article',true),(410111,'discussion',true),(410112,'discussion',true);
 INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status) VALUES
 (410201,'09a public discussion','09a plaintext current','markdown',410101,'Alice',410111,'published','discussion','published','clear','active'),
 (410202,'09a article','09a excerpt only','safe_html',410102,'Bob',410111,'published','article','published','clear','active'),
 (410203,'09a PRIVATE title','09a PRIVATE body','markdown',410101,'Alice',410111,'private','discussion','private','clear','active');
 INSERT INTO richtext_article_contents(id,thread_id,title,content_html,status,created_by) VALUES(410302,410202,'09a article','<p>09a full article primary text</p>','published',410102);`)
	official, e := svc.SaveOfficial(ctx, knowledge.Official{Title: "Native official source", Content: "Native official full body", Format: "text", Status: "draft"}, resolver)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "official", SourceID: official.ID, Active: true}, resolver); !errors.Is(e, knowledge.ErrNotFound) {
		t.Fatal("draft selected", e)
	}
	official.Status = "published"
	official, e = svc.SaveOfficial(ctx, official, resolver)
	if e != nil {
		t.Fatal(e)
	}
	state, e := svc.SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "official", SourceID: official.ID, Active: true}, resolver)
	if e != nil {
		t.Fatal(e)
	}
	check("official draft stays unavailable; publish records independent member and revision")
	state, e = svc.SaveSelection(ctx, c.ID, state.Collection.Version, knowledge.Selection{Kind: "thread", SourceID: "410201", Active: true}, resolver)
	if e != nil {
		t.Fatal(e)
	}
	state, e = svc.SaveSelection(ctx, c.ID, state.Collection.Version, knowledge.Selection{Kind: "thread", SourceID: "410202", Active: true}, resolver)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.SaveSelection(ctx, c.ID, state.Collection.Version, knowledge.Selection{Kind: "thread", SourceID: "410203", Active: true}, resolver); e == nil {
		t.Fatal("private source selected")
	}
	check("single discussion and owning RichText source materialize; private source denied")
	state, e = svc.SaveSelection(ctx, c.ID, state.Collection.Version, knowledge.Selection{Kind: "board", SourceID: "410111", Active: true}, resolver)
	if e != nil {
		t.Fatal(e)
	}
	if len(state.Members) != 3 {
		t.Fatal("board rule became members without refresh")
	}
	var board knowledge.Selection
	for _, r := range state.Selections {
		if r.Kind == "board" {
			board = r
		}
	}
	refreshed, e := svc.Refresh(ctx, c.ID, board.ID, state.Collection.Version, board.Version, 1, resolver)
	if e != nil || refreshed.Materialized != 2 || refreshed.Examined != 2 || refreshed.HasMore {
		t.Fatal(refreshed, e)
	}
	state = refreshed.State
	page, e := svc.Sources(ctx, c.ID, 1, 50)
	if e != nil || page.Total != 3 {
		t.Fatal("dedup/current visibility", page, e)
	}
	check("board selection separate from members; bounded current public refresh and overlap dedup")
	var article, plain, off knowledge.VisibleSource
	for _, v := range page.Items {
		switch v.Source.Reference.Kind {
		case knowledge.PublicArticleKind:
			article = v
		case knowledge.PublicThreadKind:
			plain = v
		case knowledge.OfficialKind:
			off = v
		}
	}
	if article.Source.Content != "09a full article primary text" || plain.Source.Content != "09a plaintext current" || off.Source.Content != official.Content {
		t.Fatal("origin mismatch")
	}
	check("published collection returns current full Community RichText official text via G1")
	exec(`UPDATE threads SET publication_status='private' WHERE id=410201`)
	page, e = svc.Sources(ctx, c.ID, 1, 50)
	if e != nil || page.Total != 2 {
		t.Fatal("private stale count", page, e)
	}
	if out, e := svc.Source(ctx, c.ID, plain.MemberID, ""); e == nil || out != (knowledge.VisibleSource{}) {
		t.Fatal("old member exposed private body")
	}
	exec(`UPDATE threads SET publication_status='published' WHERE id=410201`)
	if _, e = svc.Source(ctx, c.ID, plain.MemberID, plain.Revision); !errors.Is(e, knowledge.ErrConflict) {
		t.Fatal("public ABA", e)
	}
	check("source privacy immediate; list total exact; old reference fails after public ABA")
	exec(`UPDATE richtext_article_contents SET content_html='<p>09a newer article text</p>' WHERE thread_id=410202`)
	if _, e = svc.Source(ctx, c.ID, article.MemberID, article.Revision); !errors.Is(e, knowledge.ErrConflict) {
		t.Fatal("article-only edit not bound", e)
	}
	current, e := svc.Source(ctx, c.ID, article.MemberID, "")
	if e != nil || current.Source.Content != "09a newer article text" {
		t.Fatal(current, e)
	}
	check("article-only revision changes current result and rejects old collection condition")
	// Required audit failure must roll back both current official and its revision.
	exec(`CREATE FUNCTION owned09a_reject_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.command_code LIKE 'knowledge.%' THEN RAISE EXCEPTION 'owned09a required audit failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER owned09a_reject_audit BEFORE INSERT ON platform_command_audits FOR EACH ROW EXECUTE FUNCTION owned09a_reject_audit();`)
	var before string
	if e = pool.QueryRow(ctx, `SELECT jsonb_build_object('sources',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM knowledge_official_sources t),'revisions',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM knowledge_official_revisions t),'outbox',(SELECT count(*) FROM platform_outbox WHERE event_type='knowledge.selection.changed'))::text`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	official.Content = "must never commit"
	if _, e = svc.SaveOfficial(ctx, official, resolver); e == nil {
		t.Fatal("audit failure ignored")
	}
	var after string
	if e = pool.QueryRow(ctx, `SELECT jsonb_build_object('sources',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM knowledge_official_sources t),'revisions',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM knowledge_official_revisions t),'outbox',(SELECT count(*) FROM platform_outbox WHERE event_type='knowledge.selection.changed'))::text`).Scan(&after); e != nil || before != after {
		t.Fatal("audit failure changed durable data", e)
	}
	exec(`DROP TRIGGER owned09a_reject_audit ON platform_command_audits; DROP FUNCTION owned09a_reject_audit();`)
	check("required audit failure atomically rolls back official current revision history and outbox")
	// Same-transaction withdraw/re-publish cannot revive a saved official condition.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"withdrawn", "published"} {
		if _, err = tx.Exec(ctx, `UPDATE knowledge_official_sources SET data=jsonb_set(data,'{status}',to_jsonb($2::text)) WHERE id=$1`, official.ID, status); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, e = svc.Source(ctx, c.ID, off.MemberID, off.Revision); !errors.Is(e, knowledge.ErrConflict) {
		t.Fatal("official ABA", e)
	}
	check("non-reusing official facts prevent same-transaction status ABA")
	var rejected bool
	_, e = pool.Exec(ctx, `UPDATE knowledge_official_revisions SET data=data`)
	rejected = e != nil
	if !rejected {
		t.Fatal("revision history mutable")
	}
	check("database rejects modification of official revision history")
	// One CAS winner under actual concurrent independent Admin commands.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := svc.SaveCollection(ctx, knowledge.Collection{ID: c.ID, Version: state.Collection.Version, Title: "concurrent", Status: "active"}, resolver)
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for e := range results {
		if e == nil {
			wins++
		} else if errors.Is(e, knowledge.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal(wins, conflicts)
	}
	check("PostgreSQL concurrent collection CAS has one winner")
	state, e = svc.CollectionState(ctx, c.ID, resolver)
	if e != nil {
		t.Fatal(e)
	}
	board.Active = false
	state, e = svc.SaveSelection(ctx, c.ID, state.Collection.Version, board, resolver)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Refresh(ctx, c.ID, board.ID, state.Collection.Version, board.Version, 1, resolver); !errors.Is(e, knowledge.ErrConflict) {
		t.Fatal("stale board refresh accepted", e)
	}
	check("withdrawal invalidates member generation and stale materialization")
	exec(`DELETE FROM admin_role_permissions WHERE role_id='security_admin' AND permission_code='knowledge.collection.manage'`)
	if _, e = svc.SaveCollection(ctx, knowledge.Collection{Title: "must deny", Status: "active"}, resolver); e == nil {
		t.Fatal("current management revocation ignored")
	}
	exec(`INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES('security_admin','knowledge.collection.manage','system:migration-30')`)
	check("current Admin permission revocation denies new management command")
	exec(`UPDATE admin_sessions SET revoked_at=now(),revoke_reason='owned09a-target',version=version+1 WHERE id=$1`, sid)
	if _, e = svc.Inventory(ctx, "collections", resolver); e == nil {
		t.Fatal("revoked session accepted")
	}
	check("current independent Admin session revocation denies inventory")
	if _, e = s.features.Request(knowledge.FeatureID, false); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Sources(ctx, c.ID, 1, 50); !errors.Is(e, knowledge.ErrDisabled) {
		t.Fatal(e)
	}
	if _, e = s.features.Request(knowledge.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	check("hot feature withdrawal denies previously saved collection references")
	var audits, events int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM platform_command_audits WHERE command_code LIKE 'knowledge.%' AND actor_type='admin' AND actor_id=$1`, account).Scan(&audits); e != nil || audits < 8 {
		t.Fatal(audits, e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM platform_outbox WHERE event_type='knowledge.selection.changed' AND payload::text NOT LIKE '%Native official full body%'`).Scan(&events); e != nil || events < audits {
		t.Fatal(events, audits, e)
	}
	check("typed required command audits and metadata-only durable events persisted")
	proof := map[string]any{"status": "passed", "actual_server_composition": true, "actual_postgresql": true, "checks": checks, "check_count": len(checks), "typed_admin_audits": audits, "metadata_events": events, "indexing_or_models": false}
	b, _ := json.MarshalIndent(proof, "", "  ")
	if e = os.WriteFile(filepath.Join(root, "target-proof.json"), append(b, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
}
