package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// No fake scan port, manual page call, ProcessOnce or event callback is used.
// All pages below are executed by the normal production module lifecycle.
func TestKnowledgeIndexBackfillResidentPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_BACKFILL_DSN")
	if dsn == "" {
		t.Skip("requires owned09b backfill PostgreSQL target")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_09b_backfill" {
		t.Fatal("refuse non-owned09b backfill database")
	}
	root := os.Getenv("CAMPUSOS_KNOWLEDGE_BACKFILL_ROOT")
	if !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-09b-backfill/") {
		t.Fatal("owned09b backfill root required")
	}
	t.Chdir(root)
	ctx := t.Context()
	control, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := control.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(sql string, args ...any) int64 {
		t.Helper()
		var value int64
		if err := control.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	snapshot := func(table, predicate string) string {
		t.Helper()
		var value string
		if table != "knowledge_index_tasks" && table != "knowledge_index_backfill_checkpoints" && table != "knowledge_collections" && table != "knowledge_official_sources" {
			t.Fatal("snapshot table")
		}
		if err := control.QueryRow(ctx, "SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]')::text FROM "+table+" t "+predicate).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	wait := func(name string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
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
		t.Fatal("production backfill deadline: " + name)
	}
	checks := []string{}
	check := func(name string) { checks = append(checks, name) }
	seed := func(kind string, number int) string {
		t.Helper()
		prefix, table := "kc_", "knowledge_collections"
		data := map[string]string{"title": "PRIVATE_BACKFILL_TITLE", "status": "active", "created_by_admin_id": "adm.backfill"}
		if kind == "official" {
			prefix, table = "ko_", "knowledge_official_sources"
			data["content"], data["format"] = "PRIVATE_BACKFILL_BODY", "text"
			data["status"] = []string{"draft", "published", "withdrawn"}[number%3]
		}
		id := fmt.Sprintf("%s%032x", prefix, number)
		data["id"] = id
		wire, _ := json.Marshal(data)
		exec("INSERT INTO "+table+"(id,data) VALUES($1,$2)", id, wire)
		return id
	}
	for i := 1; i <= 70; i++ {
		seed("collection", i)
	}
	for i := 1; i <= 35; i++ {
		seed("official", i)
	}
	originalCollections := snapshot("knowledge_collections", "")
	originalOfficials := snapshot("knowledge_official_sources", "")
	firstID := fmt.Sprintf("kc_%032x", 1)
	store := reliability.NewPostgreSQLStore(control)
	historical := func(id string) reliability.Event {
		t.Helper()
		event, err := reliability.NewEvent("knowledge.selection.changed", "knowledge", id, map[string]string{"resource_id": id, "operation": "collection.save"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Enqueue(ctx, &event); err != nil {
			t.Fatal(err)
		}
		return event
	}
	published := historical(firstID)
	exec(`UPDATE platform_outbox SET status='published' WHERE id=$1`, published.ID)
	purged := historical(fmt.Sprintf("kc_%032x", 2))
	exec(`DELETE FROM platform_outbox WHERE id=$1`, purged.ID)
	// Fail the second collection page after it inserted requests, precisely
	// at checkpoint UPDATE. A nontransactional counter observes each attempt.
	exec(`CREATE SEQUENCE owned09b_backfill_faults;
 CREATE FUNCTION owned09b_backfill_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF OLD.target_kind='collection' AND OLD.after_id<>'' THEN
 PERFORM nextval('owned09b_backfill_faults'); RAISE EXCEPTION 'owned09b checkpoint failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER owned09b_backfill_fail BEFORE UPDATE ON knowledge_index_backfill_checkpoints FOR EACH ROW EXECUTE FUNCTION owned09b_backfill_fail();`)
	cfg := config.Load()
	cfg.Database.DSN, cfg.Plugin.DataDir, cfg.Redis.Enabled = dsn, filepath.Join(root, "data/plugin_data"), false
	var server *Server
	var infra, concurrent *infrastructureBootstrap
	start := func() (*Server, *infrastructureBootstrap) {
		t.Helper()
		s := New(cfg)
		infrastructure, err := s.startInfrastructure()
		if err != nil || infrastructure.database == nil || s.knowledge == nil || s.knowledge.Collections() == nil {
			t.Fatal("normal production PG composition required", err)
		}
		return s, infrastructure
	}
	server, infra = start()
	defer func() {
		if concurrent != nil {
			concurrent.Stop()
		}
		if infra != nil {
			infra.Stop()
		}
	}()
	wait("first page and checkpoint failure", func() bool {
		return scalar(`SELECT count(*) FROM knowledge_index_tasks WHERE target_kind='collection'`) == 32 && scalar(`SELECT last_value FROM owned09b_backfill_faults`) >= 2
	})
	if snapshot("knowledge_collections", "") != originalCollections || snapshot("knowledge_official_sources", "") != originalOfficials {
		t.Fatal("metadata scan changed source facts")
	}
	if scalar(`SELECT count(*) FROM knowledge_index_backfill_checkpoints WHERE target_kind='collection' AND after_id=$1 AND completed_passes=0`, fmt.Sprintf("kc_%032x", 32)) != 1 {
		t.Fatal("failed page advanced checkpoint")
	}
	if _, err := server.knowledge.Collections().PublicCollections(ctx); !errors.Is(err, knowledge.ErrDisabled) {
		t.Fatal("backfill granted reading", err)
	}
	check("normal lifecycle scans first bounded page while disabled without changing source rows or granting reading")
	check("real checkpoint UPDATE fault rolls back all second-page requests and retains first-page progress across retries")
	wait("other target kind not starved", func() bool {
		return scalar(`SELECT count(*) FROM knowledge_index_tasks WHERE target_kind='official'`) == 35
	})
	wait("failed page visible in health", func() bool { return server.knowledge.Health(ctx).Status == platformmodule.HealthDegraded })
	check("official drafts published and withdrawn metadata finish independently while collection page is failing and health degrades")
	beforeTasks := snapshot("knowledge_index_tasks", "WHERE target_kind='collection'")
	beforeCP := snapshot("knowledge_index_backfill_checkpoints", "WHERE target_kind='collection'")
	beforeRestartAttempts := scalar(`SELECT last_value FROM owned09b_backfill_faults`)
	infra.Stop()
	infra = nil
	// There is no loop, pump, or callback after Stop. The backlog remains.
	if scalar(`SELECT count(*) FROM knowledge_index_tasks WHERE target_kind='collection'`) != 32 {
		t.Fatal("scanner committed after Stop")
	}
	server, infra = start()
	wait("new graph retries persisted page", func() bool { return scalar(`SELECT last_value FROM owned09b_backfill_faults`) > beforeRestartAttempts })
	if snapshot("knowledge_index_tasks", "WHERE target_kind='collection'") != beforeTasks || snapshot("knowledge_index_backfill_checkpoints", "WHERE target_kind='collection'") != beforeCP {
		t.Fatal("restart lost checkpoint or re-committed failed page")
	}
	check("full infrastructure Stop and new graph preserve exact pending requests and partial cursor before retry")
	// A source update and a new random ID behind the cursor have no event.
	exec(`UPDATE knowledge_collections SET data=jsonb_set(data,'{title}','"PRIVATE_UPDATED_BEHIND_CURSOR"') WHERE id=$1`, firstID)
	currentVersion := scalar(`SELECT facts_version FROM knowledge_collections WHERE id=$1`, firstID)
	lowID := seed("collection", 0)
	exec(`DROP TRIGGER owned09b_backfill_fail ON knowledge_index_backfill_checkpoints; DROP FUNCTION owned09b_backfill_fail(); DROP SEQUENCE owned09b_backfill_faults;`)
	_, concurrent = start() // independent graph/pools contend on durable row locks
	wait("two resident graphs finish first pass", func() bool {
		return scalar(`SELECT count(*) FROM knowledge_index_backfill_checkpoints WHERE completed_passes=1 AND after_id=''`) == 2
	})
	if scalar(`SELECT count(*) FROM knowledge_index_tasks`) != 105 || scalar(`SELECT count(*) FROM knowledge_index_tasks WHERE target_id=$1`, lowID) != 0 || scalar(`SELECT count(*) FROM knowledge_index_tasks WHERE target_id=$1 AND target_version=$2`, firstID, currentVersion) != 0 {
		t.Fatal("first pass skipped/duplicated work or pretended to cover behind-cursor mutations")
	}
	check("two normal resident graphs serialize each target-kind checkpoint and finish more than two pages without duplicate requests")
	if scalar(`SELECT count(*) FROM outbox_consumer_receipts WHERE consumer_name=$1 AND event_id=$2`, knowledge.IndexRequestConsumerName, published.ID) != 0 || scalar(`SELECT count(*) FROM platform_outbox WHERE id=$1`, purged.ID) != 0 {
		t.Fatal("backfill replayed or fabricated historical events/receipts")
	}
	check("rows with published or physically purged Outbox history receive requests without replaying events or fabricating receipts")
	idleSnapshot := snapshot("knowledge_index_backfill_checkpoints", "")
	// Let a real scheduler tick run. Idle checkpoints must remain exactly
	// unchanged until their durable next_scan_at deadline.
	timer := time.NewTimer(1200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	if idleSnapshot != snapshot("knowledge_index_backfill_checkpoints", "") {
		t.Fatal("completed scanner did not honor durable idle deadline")
	}
	check("completed pass durably idles instead of rewriting checkpoints on every scheduler tick")
	// Move only the owned test deadline forward; neither cursor nor pass count
	// nor requests are reset. Production scheduling remains entirely resident.
	exec(`UPDATE knowledge_index_backfill_checkpoints SET next_scan_at=clock_timestamp()`)
	wait("new pass covers behind-cursor mutations", func() bool {
		return scalar(`SELECT count(*) FROM knowledge_index_backfill_checkpoints WHERE completed_passes=2 AND after_id=''`) == 2
	})
	if scalar(`SELECT count(*) FROM knowledge_index_tasks`) != 107 || scalar(`SELECT count(*) FROM knowledge_index_tasks WHERE target_id=$1 AND target_version=$2`, firstID, currentVersion) != 1 || scalar(`SELECT count(*) FROM knowledge_index_tasks WHERE target_id=$1`, lowID) != 1 {
		t.Fatal("next pass did not cover behind-cursor insert/update idempotently")
	}
	wait("health recovered", func() bool { return server.knowledge.Health(ctx).Status == platformmodule.HealthHealthy })
	check("next periodic pass reloads current epochs and finds inserts updates behind the old cursor while deduplicating unchanged targets")
	event := historical(firstID)
	wait("normal Outbox still active", func() bool {
		return scalar(`SELECT count(*) FROM outbox_consumer_receipts WHERE consumer_name=$1 AND event_id=$2`, knowledge.IndexRequestConsumerName, event.ID) == 1
	})
	if scalar(`SELECT count(*) FROM knowledge_index_tasks`) != 107 {
		t.Fatal("Outbox duplicated backfilled request")
	}
	check("Core resident Outbox consumption shares current-epoch idempotency with periodic backfill")
	concurrent.Stop()
	concurrent = nil
	infra.Stop()
	infra = nil
	metadata := snapshot("knowledge_index_tasks", "") + snapshot("knowledge_index_backfill_checkpoints", "")
	for _, forbidden := range []string{"PRIVATE_BACKFILL_BODY", "PRIVATE_BACKFILL_TITLE", "PRIVATE_UPDATED_BEHIND_CURSOR", "Authorization", "password", "token"} {
		if strings.Contains(metadata, forbidden) {
			t.Fatal("body/credential copied to metadata")
		}
	}
	if scalar(`SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='knowledge_index_backfill_checkpoints'`) != 5 {
		t.Fatal("checkpoint schema expanded beyond enumeration metadata")
	}
	check("both graphs stop and checkpoint schema contains only five enumeration metadata columns with no content authority or event watermark")
	proof := map[string]any{"status": "passed", "actual_server_composition": true, "actual_postgresql": true, "normal_resident_backfill": true, "normal_resident_outbox": true, "manual_page_or_callback": false, "actual_cmd_server_http": false, "full_infrastructure_restarts": 1, "concurrent_production_graphs": 2, "page_size": 32, "collection_targets": 71, "official_targets": 35, "task_count": 107, "completed_passes_per_kind": 2, "idle_deadline_advanced_by_owned_test": true, "index_executor_or_retrieval": false, "check_count": len(checks), "checks": checks}
	wire, _ := json.MarshalIndent(proof, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "target-proof.json"), append(wire, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
