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
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This target uses the production module graph and its resident Core worker.
// It installs no fake consumer and does not call ProcessOnce or the callback.
func TestKnowledgeIndexRequestsResidentPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_INDEX_DSN")
	if dsn == "" {
		t.Skip("requires owned09b task PostgreSQL target")
	}
	parsed, e := pgxpool.ParseConfig(dsn)
	if e != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_09b_task" {
		t.Fatal("refuse non-owned09b database")
	}
	root := os.Getenv("CAMPUSOS_KNOWLEDGE_INDEX_ROOT")
	if !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-09b-task/") {
		t.Fatal("owned09b root required")
	}
	t.Chdir(root)
	ctx := t.Context()
	control, e := pgxpool.NewWithConfig(ctx, parsed)
	if e != nil {
		t.Fatal(e)
	}
	defer control.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := control.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	scalar := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if e := control.QueryRow(ctx, sql, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	version := func(table, id string) int64 {
		t.Helper()
		var v int64
		if table != "knowledge_collections" && table != "knowledge_official_sources" {
			t.Fatal("target table")
		}
		if e := control.QueryRow(ctx, "SELECT facts_version FROM "+table+" WHERE id=$1", id).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	wait := func(name string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if predicate() {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(20 * time.Millisecond):
			}
		}
		t.Fatal("resident worker deadline: " + name)
	}
	checks := []string{}
	check := func(name string) { checks = append(checks, name) }
	outbox := reliability.NewPostgreSQLStore(control)
	event := func(kind, id string) reliability.Event {
		t.Helper()
		op := "collection.save"
		if kind == "official" {
			op = "official.save"
		}
		ev, e := reliability.NewEvent("knowledge.selection.changed", "knowledge", id, map[string]string{"resource_id": id, "operation": op})
		if e != nil {
			t.Fatal(e)
		}
		return ev
	}
	enqueue := func(ev reliability.Event) {
		t.Helper()
		if _, e := outbox.Enqueue(ctx, &ev); e != nil {
			t.Fatal(e)
		}
	}
	hasReceipt := func(id string) bool {
		return scalar(`SELECT count(*) FROM outbox_consumer_receipts WHERE consumer_name=$1 AND event_id=$2`, knowledge.IndexRequestConsumerName, id) == 1
	}
	tasks := func(kind, id string, v int64) int {
		return scalar(`SELECT count(*) FROM knowledge_index_tasks WHERE target_kind=$1 AND target_id=$2 AND target_version=$3 AND status='pending'`, kind, id, v)
	}
	snapshot := func() string {
		t.Helper()
		var out string
		if e := control.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]')::text FROM knowledge_index_tasks t`).Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	seedCollection := func(id, title string) {
		t.Helper()
		data, _ := json.Marshal(map[string]string{"id": id, "title": title, "status": "active", "created_by_admin_id": "adm.owned09b"})
		exec(`INSERT INTO knowledge_collections(id,data) VALUES($1,$2)`, id, data)
	}
	preID := "kc_" + strings.Repeat("b", 32)
	seedCollection(preID, "PRESTART_PRIVATE_METADATA")
	preEvent := event("collection", preID)
	enqueue(preEvent)
	cfg := config.Load()
	cfg.Database.DSN = dsn
	cfg.Plugin.DataDir = filepath.Join(root, "data/plugin_data")
	cfg.Redis.Enabled = false
	var server *Server
	var infra *infrastructureBootstrap
	start := func() {
		t.Helper()
		server = New(cfg)
		var e error
		infra, e = server.startInfrastructure()
		if e != nil {
			t.Fatal(e)
		}
		if infra.database == nil || server.knowledge == nil || server.knowledge.Collections() == nil {
			t.Fatal("requires normal production PG composition")
		}
	}
	start()
	defer func() {
		if infra != nil {
			infra.Stop()
		}
	}()
	wait("pre-start event retained", func() bool {
		return hasReceipt(preEvent.ID) && tasks("collection", preID, version("knowledge_collections", preID)) == 1
	})
	if _, e = server.knowledge.Collections().PublicCollections(ctx); !errors.Is(e, knowledge.ErrDisabled) {
		t.Fatal("Feature defaults on", e)
	}
	check("pre-existing event is consumed by normal resident worker despite Core-before-Feature startup and default-off Knowledge")
	// Independent Admin production proof for real business commands.
	admins := identityrepo.NewPgAdminDomainRepository(control)
	account := "adm.knowledge.index"
	if e = admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: account, Username: "knowledge-index", Email: "knowledge-index@example.test", DisplayName: "09b owned"}); e != nil {
		t.Fatal(e)
	}
	if e = admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "arb.knowledge.index", AccountID: account, RoleID: "security_admin", GrantedBy: account}); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	sid := "as.knowledge.index"
	if e = identityrepo.NewPgAdminSessionRepository(control).CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: account, RefreshTokenDigest: strings.Repeat("a", 64), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, server.identity.AdminDomainSessions(), &auth.JWTClaims{UserID: account, SessionID: sid, AuthVersion: 1, TokenType: auth.AccessTokenType})
	}
	if _, e = server.features.Request(knowledge.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	svc := server.knowledge.Collections()
	official, e := svc.SaveOfficial(ctx, knowledge.Official{Title: "PRIVATE09B_TITLE", Content: "PRIVATE09B_BODY", Format: "text", Status: "draft"}, resolve)
	if e != nil {
		t.Fatal(e)
	}
	wait("official draft intent", func() bool { return tasks("official", official.ID, official.Version) == 1 })
	official.Status = "published"
	official, e = svc.SaveOfficial(ctx, official, resolve)
	if e != nil {
		t.Fatal(e)
	}
	wait("official publication intent", func() bool { return tasks("official", official.ID, official.Version) == 1 })
	check("current independent Admin audited draft and publication each become pending current-epoch metadata requests")
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ev, err := reliability.NewEvent("knowledge.selection.changed", "knowledge", official.ID, map[string]string{"resource_id": official.ID, "operation": "official.save"})
			if err != nil {
				t.Error(err)
				return
			}
			if _, err = outbox.Enqueue(ctx, &ev); err != nil {
				t.Error(err)
				return
			}
			ids <- ev.ID
		}()
	}
	wg.Wait()
	close(ids)
	for id := range ids {
		wait("duplicate receipt", func() bool { return hasReceipt(id) })
	}
	if tasks("official", official.ID, official.Version) != 1 {
		t.Fatal("duplicate task")
	}
	check("sixteen duplicate committed events get independent receipts and one current-version task")
	// A lower-ID event is uncommitted while a later business command commits.
	lateID := "kc_" + strings.Repeat("c", 32)
	seedCollection(lateID, "late-before")
	staged := event("collection", lateID)
	staged.ID = "10001"
	ready, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- transaction.NewPostgreSQL(control).Within(ctx, func(tx context.Context) error {
			if _, e := outbox.Enqueue(tx, &staged); e != nil {
				return e
			}
			close(ready)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-ready:
	case e := <-done:
		t.Fatal(e)
	case <-time.After(5 * time.Second):
		t.Fatal("staged event deadline")
	}
	late, e := svc.SaveCollection(ctx, knowledge.Collection{ID: lateID, Version: version("knowledge_collections", lateID), Title: "late-current", Status: "active"}, resolve)
	if e != nil {
		close(release)
		t.Fatal(e)
	}
	wait("newer command task", func() bool { return tasks("collection", lateID, late.Version) == 1 })
	if hasReceipt(staged.ID) {
		close(release)
		t.Fatal("uncommitted event visible")
	}
	// A periodic metadata backfill may already have requested the earlier
	// epoch. The delayed event must not add any epoch after the current one
	// has been requested; do not confuse that earlier valid scan with replay.
	beforeLateCommit := scalar(`SELECT count(*) FROM knowledge_index_tasks WHERE target_id=$1`, lateID)
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	wait("late lower-ID receipt", func() bool { return hasReceipt(staged.ID) })
	if scalar(`SELECT count(*) FROM knowledge_index_tasks WHERE target_id=$1`, lateID) != beforeLateCommit {
		t.Fatal("late event enqueued stale epoch")
	}
	check("late lower-ID commit after newer command ACK still gets a receipt and reloads current epoch without an event-ID cursor")
	// Insert failure: no task and no receipt until the real worker retries.
	exec(`CREATE FUNCTION owned09b_fail_task() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned09b task insert failure'; END $$;
 CREATE TRIGGER owned09b_fail_task BEFORE INSERT ON knowledge_index_tasks FOR EACH ROW EXECUTE FUNCTION owned09b_fail_task();`)
	failTask, e := svc.SaveCollection(ctx, knowledge.Collection{Title: "task insert failure", Status: "active"}, resolve)
	if e != nil {
		t.Fatal(e)
	}
	var failTaskEvent string
	if e = control.QueryRow(ctx, `SELECT id FROM platform_outbox WHERE aggregate_id=$1`, failTask.ID).Scan(&failTaskEvent); e != nil {
		t.Fatal(e)
	}
	wait("task insert retry", func() bool {
		return scalar(`SELECT count(*) FROM platform_outbox WHERE id=$1 AND status='retry'`, failTaskEvent) == 1
	})
	if tasks("collection", failTask.ID, failTask.Version) != 0 || hasReceipt(failTaskEvent) {
		t.Fatal("failed insertion was acknowledged")
	}
	exec(`DROP TRIGGER owned09b_fail_task ON knowledge_index_tasks; DROP FUNCTION owned09b_fail_task();`)
	wait("task insertion recovers", func() bool {
		return tasks("collection", failTask.ID, failTask.Version) == 1 && hasReceipt(failTaskEvent)
	})
	check("real task INSERT failure leaves no receipt; Core retry durably inserts before ACK")
	// Receipt failure: task transaction commits first, then a retry deduplicates.
	exec(`CREATE FUNCTION owned09b_fail_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.consumer_name='knowledge:index-request:v1' THEN RAISE EXCEPTION 'owned09b receipt failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER owned09b_fail_receipt BEFORE INSERT ON outbox_consumer_receipts FOR EACH ROW EXECUTE FUNCTION owned09b_fail_receipt();`)
	failReceipt, e := svc.SaveCollection(ctx, knowledge.Collection{Title: "receipt failure", Status: "active"}, resolve)
	if e != nil {
		t.Fatal(e)
	}
	var receiptEvent string
	if e = control.QueryRow(ctx, `SELECT id FROM platform_outbox WHERE aggregate_id=$1`, failReceipt.ID).Scan(&receiptEvent); e != nil {
		t.Fatal(e)
	}
	wait("committed task without receipt", func() bool {
		return tasks("collection", failReceipt.ID, failReceipt.Version) == 1 && scalar(`SELECT count(*) FROM platform_outbox WHERE id=$1 AND status='retry'`, receiptEvent) == 1
	})
	if hasReceipt(receiptEvent) {
		t.Fatal("injected receipt failure ignored")
	}
	exec(`DROP TRIGGER owned09b_fail_receipt ON outbox_consumer_receipts; DROP FUNCTION owned09b_fail_receipt();`)
	wait("receipt retry recovery", func() bool { return hasReceipt(receiptEvent) })
	if tasks("collection", failReceipt.ID, failReceipt.Version) != 1 {
		t.Fatal("ACK-window task duplicated")
	}
	check("real receipt failure after task commit retries without duplicating metadata request")
	// Feature off must not discard invalidation requests already committed.
	if _, e = server.features.Request(knowledge.FeatureID, false); e != nil {
		t.Fatal(e)
	}
	exec(`DELETE FROM knowledge_collections WHERE id=$1`, failReceipt.ID)
	missing := event("collection", failReceipt.ID)
	enqueue(missing)
	wait("absent target cleanup intent", func() bool { return tasks("collection", failReceipt.ID, 0) == 1 && hasReceipt(missing.ID) })
	check("absent target records version-zero cleanup intent even after Knowledge is disabled")
	beforeRestart := snapshot()
	infra.Stop()
	infra = nil
	restartID := "kc_" + strings.Repeat("d", 32)
	seedCollection(restartID, "restart backlog")
	restartEvent := event("collection", restartID)
	enqueue(restartEvent)
	if tasks("collection", restartID, version("knowledge_collections", restartID)) != 0 {
		t.Fatal("worker still active after full infrastructure stop")
	}
	start()
	wait("restarted resident worker backlog", func() bool {
		return hasReceipt(restartEvent.ID) && tasks("collection", restartID, version("knowledge_collections", restartID)) == 1
	})
	var previousAfter string
	if e = control.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]')::text FROM knowledge_index_tasks t WHERE target_id<>$1`, restartID).Scan(&previousAfter); e != nil || previousAfter != beforeRestart {
		t.Fatal("restart changed or lost previous metadata", e)
	}
	check("full production infrastructure restart reloads persisted tasks and consumes downtime backlog with no callback re-registration gap")
	if _, e = server.knowledge.Collections().PublicCollections(ctx); !errors.Is(e, knowledge.ErrDisabled) {
		t.Fatal("restart changed Feature scope", e)
	}
	check("metadata consumption while disabled does not enable Knowledge or grant public reading")
	before := snapshot()
	for _, sql := range []string{`UPDATE knowledge_index_tasks SET status=status`, `DELETE FROM knowledge_index_tasks`, `TRUNCATE knowledge_index_tasks`} {
		if _, e = control.Exec(ctx, sql); e == nil {
			t.Fatal("request intent mutable", sql)
		}
	}
	if before != snapshot() {
		t.Fatal("immutability refusal changed metadata")
	}
	check("database rejects request UPDATE DELETE and TRUNCATE while preserving all metadata")
	var columns []string
	rows, e := control.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='knowledge_index_tasks' ORDER BY column_name`)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var col string
		if e = rows.Scan(&col); e != nil {
			t.Fatal(e)
		}
		columns = append(columns, col)
	}
	rows.Close()
	if strings.Join(columns, ",") != "created_at,id,status,target_id,target_kind,target_version" {
		t.Fatal("unexpected task data columns", columns)
	}
	wire := snapshot()
	for _, value := range []string{"PRIVATE09B_TITLE", "PRIVATE09B_BODY", "PRESTART_PRIVATE_METADATA", "Authorization", "password", "token"} {
		if strings.Contains(wire, value) {
			t.Fatal("body/credential entered task metadata")
		}
	}
	check("task schema and rows contain only six bounded metadata columns with no body principal credential or event cursor")
	infra.Stop()
	infra = nil
	proof := map[string]any{"status": "passed", "actual_server_composition": true, "actual_postgresql": true, "normal_resident_worker": true, "test_callback_or_process_once": false, "actual_cmd_server_http": false, "full_infrastructure_restarts": 1, "checks": checks, "check_count": len(checks), "task_count": scalar(`SELECT count(*) FROM knowledge_index_tasks`), "task_schema_columns": columns, "index_executor_or_retrieval": false}
	data, _ := json.MarshalIndent(proof, "", "  ")
	if e = os.WriteFile(filepath.Join(root, "target-proof.json"), append(data, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
}
