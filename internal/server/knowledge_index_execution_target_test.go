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

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/internal/modules/features/richtext"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// All indexing, source consumption, retries and reconciliation run through the
// normal Server lifecycle. No ProcessOnce, manual page or fake consumer is used.
func TestKnowledgeIndexExecutionResidentPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_EXECUTION_DSN")
	if dsn == "" {
		t.Skip("requires owned09b execution PostgreSQL")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_09b_exit" {
		t.Fatal("refuse non-owned09b execution database")
	}
	root := os.Getenv("CAMPUSOS_KNOWLEDGE_EXECUTION_ROOT")
	if !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-09b-exit/") {
		t.Fatal("owned execution root required")
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
		t.Fatal("execution deadline: " + name)
	}
	checks := []string{}
	check := func(name string) { checks = append(checks, name) }
	exec(`INSERT INTO users(id,username,nickname,email) VALUES(420101,'owned09b-exit-user','Index owner','owned09b-exit-user@example.test');
 INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(420110,'09b group','owned09b-exit-group','group','active');
 INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(420111,'09b board','owned09b-exit-board',420110,'board','active'),(420112,'09b moved','owned09b-exit-moved',420110,'board','active');
 INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(420111,'discussion',true),(420111,'article',true),(420112,'discussion',true);
 INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status)
 SELECT 420200+n,'09b public '||n,'09b current text '||n,'markdown',420101,'Index owner',420111,'published','discussion','published','clear','active' FROM generate_series(1,70) n;
 INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status) VALUES
 (420290,'09b article','EXCERPT_MUST_NOT_INDEX','safe_html',420101,'Index owner',420111,'published','article','published','clear','active'),
 (420291,'PRIVATE_SOURCE_TITLE','PRIVATE_SOURCE_BODY','markdown',420101,'Index owner',420111,'private','discussion','private','clear','active');
 INSERT INTO richtext_article_contents(id,thread_id,title,content_html,status,created_by) VALUES(420390,420290,'09b article','<p>Primary article text <strong>中文</strong></p>','published',420101);
 CREATE SEQUENCE owned09b_execution_faults;
 CREATE FUNCTION owned09b_execution_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF OLD.data->>'phase'='sources' AND OLD.data->>'source_after'<>'' AND NEW.data->>'state'='pending' AND NEW.data->>'phase'='sources' THEN
 PERFORM nextval('owned09b_execution_faults'); RAISE EXCEPTION 'owned09b execution checkpoint fault'; END IF;RETURN NEW;END $$;
 CREATE TRIGGER owned09b_execution_fault BEFORE UPDATE ON knowledge_index_jobs FOR EACH ROW EXECUTE FUNCTION owned09b_execution_fault();`)
	cfg := config.Load()
	cfg.Database.DSN, cfg.Plugin.DataDir, cfg.Redis.Enabled = dsn, filepath.Join(root, "data/plugin_data"), false
	var server *Server
	var infra, second *infrastructureBootstrap
	start := func() (*Server, *infrastructureBootstrap) {
		t.Helper()
		s := New(cfg)
		i, e := s.startInfrastructure()
		if e != nil || i.database == nil || s.knowledge == nil || s.knowledge.Indexer() == nil {
			t.Fatal("normal PG composition required", e)
		}
		return s, i
	}
	server, infra = start()
	defer func() {
		if second != nil {
			second.Stop()
		}
		if infra != nil {
			infra.Stop()
		}
	}()
	if _, err = server.features.Request(knowledge.FeatureID, true); err != nil {
		t.Fatal(err)
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	account := "adm.knowledge.execution"
	if err = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "execution-native", Email: "execution-native@example.test", DisplayName: "09b native"}); err != nil {
		t.Fatal(err)
	}
	if err = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.knowledge.execution", AccountID: account, RoleID: "security_admin", GrantedBy: account}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sid := "as.knowledge.execution"
	if err = identityrepo.NewPgAdminSessionRepository(pool).CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: account, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, server.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: account, SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	c, err := server.knowledge.Collections().SaveCollection(ctx, knowledge.Collection{Title: "09b complete index", Status: "active"}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	state, err := server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "board", SourceID: "420111", Active: true}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	c = state.Collection
	ready := func(id string) bool {
		return scalar(`SELECT count(*) FROM knowledge_index_jobs WHERE id=$1 AND state='idle' AND (data->>'ready_generation')::bigint>0 AND (data->>'run_generation')::bigint=0 AND data->>'requested'=data->>'active_request' AND available_at_ms>$2`, id, time.Now().Add(time.Second).UnixMilli()) == 1
	}
	wake := func(id string) {
		exec(`UPDATE knowledge_index_jobs SET data=jsonb_set(data,'{available_at_ms}','"0"') WHERE id=$1 AND state IN ('idle','dead')`, id)
	}
	count := func(id string) int64 {
		return scalar(`SELECT count(*) FROM knowledge_index_documents WHERE collection_id=$1`, id)
	}
	wait("second source page rolls back", func() bool {
		return scalar(`SELECT last_value FROM owned09b_execution_faults`) >= 1 && count(c.ID) == 32 && scalar(`SELECT count(*) FROM knowledge_index_jobs WHERE id=$1 AND state='retry'`, c.ID) == 1
	})
	cursor := text(`SELECT data->>'source_after' FROM knowledge_index_jobs WHERE id=$1`, c.ID)
	generation := text(`SELECT data->>'run_generation' FROM knowledge_index_jobs WHERE id=$1`, c.ID)
	if cursor != "420232" {
		t.Fatal("numeric source page", cursor)
	}
	if scalar(`SELECT count(*) FROM knowledge_index_documents WHERE collection_id=$1 AND (data->>'source_after') IS NOT NULL`, c.ID) != 0 {
		t.Fatal("bad document payload")
	}
	check("normal production executor materializes32 public sources and atomically rolls back second-page bodies/members/checkpoint on real SQL trigger failure")
	infra.Stop()
	infra = nil
	docsBefore := text(`SELECT jsonb_agg(to_jsonb(d) ORDER BY id)::text FROM knowledge_index_documents d`)
	server, infra = start()
	if text(`SELECT data->>'source_after' FROM knowledge_index_jobs WHERE id=$1`, c.ID) != cursor || text(`SELECT data->>'run_generation' FROM knowledge_index_jobs WHERE id=$1`, c.ID) != generation || text(`SELECT jsonb_agg(to_jsonb(d) ORDER BY id)::text FROM knowledge_index_documents d`) != docsBefore {
		t.Fatal("restart lost completed page")
	}
	_, second = start()
	exec(`DROP TRIGGER owned09b_execution_fault ON knowledge_index_jobs;DROP FUNCTION owned09b_execution_fault();DROP SEQUENCE owned09b_execution_faults;`)
	wait("two graphs finish durable cross-domain keyset", func() bool { return ready(c.ID) && count(c.ID) == 71 })
	check("full Stop/new Server and second production graph resume persisted numeric cursor and run generation and publish exactly71 public Community/RichText projections")
	ids := func(id string) []string {
		t.Helper()
		rows, e := pool.Query(ctx, `SELECT id FROM knowledge_index_documents WHERE collection_id=$1 ORDER BY id`, id)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				t.Fatal(e)
			}
			out = append(out, id)
		}
		if rows.Err() != nil {
			t.Fatal(rows.Err())
		}
		return out
	}
	for _, id := range ids(c.ID) {
		d, e := server.knowledge.Indexer().CurrentDocument(ctx, id)
		if e != nil || d.ParserVersion == "" || d.ChunkerVersion == "" || len(d.ProfileFingerprint) != 64 {
			t.Fatal("current completed document", e)
		}
	}
	if scalar(`SELECT count(*) FROM knowledge_index_documents WHERE data::text LIKE '%PRIVATE_SOURCE_%' OR data::text LIKE '%EXCERPT_MUST_NOT_INDEX%'`) != 0 {
		t.Fatal("private or excerpt body indexed")
	}
	articleID := text(`SELECT id FROM knowledge_index_documents WHERE collection_id=$1 AND data->'source'->>'kind'='richtext.article'`, c.ID)
	a, err := server.knowledge.Indexer().CurrentDocument(ctx, articleID)
	if err != nil || !strings.Contains(strings.Join(a.Chunks, ""), "Primary article text 中文") {
		t.Fatal("owning full sanitized article", err)
	}
	check("completed-generation reads recheck current G1 and parser/chunker/profile; private text and Community article excerpt absent")
	// Exercise actual authenticated owning commands; their Outbox deliveries are
	// independently ACKed by the resident Core source consumer.
	user, err := identityrepo.NewPgUserRepository(pool).GetByID(ctx, "420101")
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := server.identity.Sessions().Issue(ctx, user, identityservice.SessionMetadata{DeviceName: "09b source commands"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := server.identity.JWTManager().VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	userResolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, server.identity.Sessions(), server.identity.Sessions(), claims)
	}
	plainID := text(`SELECT id FROM knowledge_index_documents WHERE collection_id=$1 AND data->'source'->>'id'='420201'`, c.ID)
	old, err := server.knowledge.Indexer().CurrentDocument(ctx, plainID)
	if err != nil {
		t.Fatal(err)
	}
	body := "09b authoritative edited discussion"
	if _, err = server.community.ThreadService().UpdateThreadForPrincipal(ctx, "420201", domain.UpdateThreadRequest{Content: &body}, userResolve); err != nil {
		t.Fatal(err)
	}
	if d, e := server.knowledge.Indexer().CurrentDocument(ctx, plainID); e == nil && d.Source.Revision == old.Source.Revision {
		t.Fatal("old source revision released")
	}
	wait("Community event rebuild", func() bool {
		d, e := server.knowledge.Indexer().CurrentDocument(ctx, plainID)
		return e == nil && strings.Join(d.Chunks, "") == body
	})
	wait("Community source receipt", func() bool {
		return scalar(`SELECT count(*) FROM outbox_consumer_receipts r JOIN platform_outbox o ON o.id=r.event_id WHERE r.consumer_name=$1 AND o.aggregate_id='420201' AND o.event_type='thread.updated'`, knowledge.IndexSourceConsumerName) >= 1
	})
	check("actual User session Community author edit emits owning event; resident source consumer commits invalidation before receipt and new source revision replaces stale text")
	if _, err = server.richtext.Service().UpdateDraftForPrincipal(ctx, "420290", richtext.SaveArticleRequest{Title: "09b revised article", ContentHTML: "<p>09b owning richtext revision</p>"}, userResolve); err != nil {
		t.Fatal(err)
	}
	if _, err = server.knowledge.Indexer().CurrentDocument(ctx, articleID); err == nil {
		t.Fatal("draft article remained readable")
	}
	if _, err = server.richtext.Service().PublishForPrincipal(ctx, "420290", userResolve); err != nil {
		t.Fatal(err)
	}
	wait("RichText own revision", func() bool {
		d, e := server.knowledge.Indexer().CurrentDocument(ctx, articleID)
		return e == nil && strings.Join(d.Chunks, "") == "09b owning richtext revision" && d.Source.Revision != a.Source.Revision
	})
	check("actual authenticated RichText draft/publish uses its own thread events and article epoch; no inherited old article text")
	official, err := server.knowledge.Collections().SaveOfficial(ctx, knowledge.Official{Title: "09b official", Content: "09b official current", Format: "text", Status: "published"}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	state, err = server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, knowledge.Selection{Kind: "official", SourceID: official.ID, Active: true}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	c = state.Collection
	wait("official indexed", func() bool { return ready(c.ID) && count(c.ID) == 72 })
	offID := text(`SELECT id FROM knowledge_index_documents WHERE collection_id=$1 AND data->'source'->>'kind'='knowledge.official'`, c.ID)
	official.Content = "09b official revision2"
	official, err = server.knowledge.Collections().SaveOfficial(ctx, official, resolve)
	if err != nil {
		t.Fatal(err)
	}
	wait("official revision replaced", func() bool {
		d, e := server.knowledge.Indexer().CurrentDocument(ctx, offID)
		return e == nil && strings.Join(d.Chunks, "") == official.Content
	})
	official.Status = "withdrawn"
	official, err = server.knowledge.Collections().SaveOfficial(ctx, official, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.knowledge.Indexer().CurrentDocument(ctx, offID); err == nil {
		t.Fatal("withdrawn official readable")
	}
	wait("withdrawn body physically removed", func() bool { return scalar(`SELECT count(*) FROM knowledge_index_documents WHERE id=$1`, offID) == 0 })
	check("official current revision events invalidate dependent jobs after target lock release; withdrawal denies immediately and deletes projected bytes")
	// No event is produced for these authority changes. Only the idle deadline
	// in this owned fixture is advanced; run cursors/generations remain untouched.
	wait("before no-event changes", func() bool { return ready(c.ID) })
	movedID := text(`SELECT id FROM knowledge_index_documents WHERE collection_id=$1 AND data->'source'->>'id'='420202'`, c.ID)
	deletedID := text(`SELECT id FROM knowledge_index_documents WHERE collection_id=$1 AND data->'source'->>'id'='420203'`, c.ID)
	exec(`UPDATE threads SET category_id=420112 WHERE id=420202;DELETE FROM threads WHERE id=420203;
 DELETE FROM platform_outbox WHERE status='published';`)
	for _, id := range []string{movedID, deletedID} {
		if _, e := server.knowledge.Indexer().CurrentDocument(ctx, id); e == nil {
			t.Fatal("no-event vanished/moved source readable")
		}
	}
	wake(c.ID)
	wait("authoritative missing/moved eviction", func() bool { return ready(c.ID) && count(c.ID) == 69 })
	if scalar(`SELECT count(*) FROM knowledge_collection_sources WHERE collection_id=$1 AND data->>'source_id' IN ('420202','420203')`, c.ID) != 0 {
		t.Fatal("stale members retained")
	}
	check("periodic authoritative source/member/doc pages remove moved and physically vanished sources with published events purged, without fabricated receipts")
	beforeArticle, err := server.knowledge.Indexer().CurrentDocument(ctx, articleID)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE richtext_article_contents SET content_html='<p>no-event temporary revision</p>' WHERE id=420390`)
	exec(`UPDATE richtext_article_contents SET content_html='<p>09b owning richtext revision</p>' WHERE id=420390`)
	if _, err = server.knowledge.Indexer().CurrentDocument(ctx, articleID); err == nil {
		t.Fatal("no-event article ABA reused proof")
	}
	wake(c.ID)
	wait("no-event article ABA reconciliation", func() bool {
		d, e := server.knowledge.Indexer().CurrentDocument(ctx, articleID)
		return e == nil && d.Source.Revision != beforeArticle.Source.Revision && ready(c.ID)
	})
	check("RichText-only no-event HTML ABA is denied immediately and rebuilt from its non-reused article epoch after retention gaps")

	exec(`UPDATE categories SET lifecycle_status='archived',version=version+1 WHERE parent_id=420110;UPDATE categories SET lifecycle_status='archived',version=version+1 WHERE id=420110`)
	if _, err = server.knowledge.Indexer().CurrentDocument(ctx, plainID); err == nil {
		t.Fatal("archived parent released")
	}
	wake(c.ID)
	wait("parent lifecycle eviction", func() bool { return ready(c.ID) && count(c.ID) == 0 })
	exec(`UPDATE categories SET lifecycle_status='active',version=version+1 WHERE id=420110;UPDATE categories SET lifecycle_status='active',version=version+1 WHERE parent_id=420110`)
	wake(c.ID)
	wait("source restore authoritative", func() bool { return ready(c.ID) && count(c.ID) == 69 })
	check("no-event board/group archive immediately revokes bytes; paginated authority pass evicts all and later restore rematerializes current members")
	// Rule ABA bumps both rule and collection epochs. Old completion is never
	// authority for the new rule even when IDs and source bytes are unchanged.
	board := state.Selections[0]
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
	if _, err = server.knowledge.Indexer().CurrentDocument(ctx, plainID); err == nil {
		t.Fatal("inactive rule returned old projection")
	}
	for _, r := range state.Selections {
		if r.ID == board.ID {
			board = r
		}
	}
	board.Active = true
	state, err = server.knowledge.Collections().SaveSelection(ctx, c.ID, c.Version, board, resolve)
	if err != nil {
		t.Fatal(err)
	}
	c = state.Collection
	wait("rule ABA rebuilt generation", func() bool { return ready(c.ID) && count(c.ID) == 69 })
	check("rule disable/enable ABA requires new rule and collection revisions and a complete fenced generation")
	// Apply the existing process-local hot gate after stopping the second graph.
	second.Stop()
	second = nil
	if _, err = server.features.Request(knowledge.FeatureID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = server.knowledge.Indexer().CurrentDocument(ctx, plainID); !errors.Is(err, knowledge.ErrDisabled) {
		t.Fatal("feature-off bytes", err)
	}
	wake(c.ID)
	wait("feature-off projection purge", func() bool {
		return count(c.ID) == 0 && scalar(`SELECT count(*) FROM knowledge_index_jobs WHERE id=$1 AND state='idle' AND data->>'ready_generation'='0'`, c.ID) == 1
	})
	if _, err = server.features.Request(knowledge.FeatureID, true); err != nil {
		t.Fatal(err)
	}
	wake(c.ID)
	wait("feature-on new completed pass", func() bool { return ready(c.ID) && count(c.ID) == 69 })
	check("Feature-off denies checked reads and purges body projections in bounded pages; re-enable rebuilds through current G1")
	orphan, err := server.knowledge.Collections().SaveCollection(ctx, knowledge.Collection{Title: "09b orphan reconciliation", Status: "active"}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.knowledge.Collections().SaveSelection(ctx, orphan.ID, orphan.Version, knowledge.Selection{Kind: "thread", SourceID: "420201", Active: true}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	wait("orphan fixture ready", func() bool { return ready(orphan.ID) && count(orphan.ID) == 1 })
	// Remove the owning reference facts in their legal FK order in one
	// maintenance transaction; leave derived jobs/documents intact and emit no
	// event. The executor must handle this disappeared target independently.
	remove, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = remove.Exec(ctx, `SELECT id FROM knowledge_collections WHERE id=$1 FOR UPDATE`, orphan.ID); err != nil {
		_ = remove.Rollback(ctx)
		t.Fatal(err)
	}
	for _, table := range []string{"knowledge_collection_sources", "knowledge_source_selections", "knowledge_collections"} {
		field := "collection_id"
		if table == "knowledge_collections" {
			field = "id"
		}
		if _, err = remove.Exec(ctx, "DELETE FROM "+table+" WHERE "+field+"=$1", orphan.ID); err != nil {
			_ = remove.Rollback(ctx)
			t.Fatal(err)
		}
	}
	if err = remove.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wake(orphan.ID)
	wait("disappeared collection cleanup", func() bool {
		return count(orphan.ID) == 0 && scalar(`SELECT count(*) FROM knowledge_index_jobs WHERE id=$1 AND state='idle' AND data->>'ready_generation'='0'`, orphan.ID) == 1
	})
	check("persisted execution job survives physical collection deletion and purges orphan bodies even without a target row or event")
	infra.Stop()
	infra = nil
	stopped := text(`SELECT jsonb_agg(to_jsonb(j) ORDER BY id)::text FROM knowledge_index_jobs j`)
	time.Sleep(250 * time.Millisecond)
	if text(`SELECT jsonb_agg(to_jsonb(j) ORDER BY id)::text FROM knowledge_index_jobs j`) != stopped {
		t.Fatal("execution changed after Stop returned")
	}
	proof := map[string]any{"status": "passed", "actual_server_composition": true, "actual_postgresql": true, "normal_resident_executor": true, "normal_resident_outbox": true, "manual_page_or_callback": false, "full_infrastructure_restarts": 1, "production_graphs": 2, "checks": checks, "check_count": len(checks), "source_page_size": 32, "initial_public_documents": 71, "final_public_documents": count(c.ID), "windows": "deferred_by_user"}
	wire, _ := json.MarshalIndent(proof, "", "  ")
	if err = os.WriteFile(filepath.Join(root, "target-proof.json"), append(wire, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
