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

	"github.com/campusos/CampusOS/internal/modules/features/knowledge"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This is a native component target, compiled into server.test. It uses the
// normal production infrastructure/module composition and actual PG16 facts;
// there is no test HTTP route, mock query, or assertion of collection delivery.
func TestKnowledgePublicSourceProductionPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_KNOWLEDGE_TEST_DSN")
	if dsn == "" {
		t.Skip("requires owned09a PostgreSQL target")
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || parsed.ConnConfig.Host != "127.0.0.1" || parsed.ConnConfig.Database != "campusos_v12_09a_public_source" {
		t.Fatal("refuse non-owned knowledge target database")
	}
	root := os.Getenv("CAMPUSOS_KNOWLEDGE_TARGET_ROOT")
	if !filepath.IsAbs(root) || !strings.Contains(filepath.ToSlash(root), "/.cache/v12-09a-public-source/") {
		t.Fatal("requires owned09a target root")
	}
	t.Chdir(root)
	cfg := config.Load()
	cfg.Database.DSN = dsn
	cfg.Plugin.DataDir = filepath.Join(root, "data", "plugin_data")
	cfg.Redis.Enabled = false
	s := New(cfg)
	infra, err := s.startInfrastructure()
	if err != nil {
		t.Fatal(err)
	}
	defer infra.Stop()
	if infra.database == nil {
		t.Fatal("real PostgreSQL is required")
	}
	value, found := s.appContext.Lookup(knowledge.PortPublicSourceCandidates)
	reader, typed := value.(knowledge.PublicSourceCandidates)
	if !found || !typed {
		t.Fatal("production knowledge port is missing")
	}
	ctx := t.Context()
	pool := infra.database
	proof := map[string]any{"status": "passed", "actual_server_composition": true, "actual_cmd_server_http": false, "postgres": true, "source_port_only": true, "migrations": 29}
	checks := []string{}
	check := func(name string) { checks = append(checks, name) }
	deny := func(candidate knowledge.PublicThreadCandidate, err, want error) {
		t.Helper()
		if candidate != (knowledge.PublicThreadCandidate{}) || !errors.Is(err, want) {
			t.Fatalf("denied source released content or wrong reason: %v", err)
		}
	}
	c, e := reader.PreviewPublicThread(ctx, "390201")
	deny(c, e, knowledge.ErrDisabled)
	check("default disabled without source access")
	if _, e = s.features.Request(knowledge.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO users(id,username,nickname,email) VALUES
 (390101,'owned09a-alice','Alice','owned09a-alice@example.test'),(390102,'owned09a-bob','Bob','owned09a-bob@example.test');
INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(390110,'Owned source group','owned09a-group','group','active');
INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES
 (390111,'Owned source board','owned09a-board',390110,'board','active'),(390112,'Owned moved board','owned09a-moved',390110,'board','active');
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES (390111,'discussion',TRUE),(390112,'discussion',TRUE);
INSERT INTO threads(id,title,content,content_format,author_id,author_name,category_id,status,thread_type,publication_status,moderation_status,deletion_status) VALUES
 (390201,'Owned public Alice','PUBLIC09A ALICE exact text','markdown',390101,'Alice',390111,'published','discussion','published','clear','active'),
 (390202,'Owned public Bob','PUBLIC09A BOB exact text','text',390102,'Bob',390111,'published','discussion','published','clear','active'),
 (390203,'PRIVATE09A ALICE title','PRIVATE09A ALICE body','markdown',390101,'Alice',390111,'private','discussion','private','clear','active'),
 (390204,'PRIVATE09A BOB title','PRIVATE09A BOB body','markdown',390102,'Bob',390111,'private','discussion','private','clear','active');`)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"390201", "390202"} {
		got, e := reader.PreviewPublicThread(ctx, id)
		if e != nil || got.Reference.ID != id || got.CategoryID != "390111" || got.URL != "/threads/"+id || !strings.HasPrefix(got.Content, "PUBLIC09A ") {
			t.Fatal("current production public source failed", e)
		}
		current, e := reader.RecheckPublicThread(ctx, got.Reference)
		if e != nil || current != got {
			t.Fatal("current production reference failed", e)
		}
	}
	check("two owners public text Markdown and canonical origin")
	for _, id := range []string{"390203", "390204", "390299"} {
		c, e := reader.PreviewPublicThread(ctx, id)
		deny(c, e, knowledge.ErrSourceUnavailable)
	}
	check("both owners private and missing source deny without metadata")
	var before, after string
	const records = `SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM threads t`
	if err = pool.QueryRow(ctx, records).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := reader.PreviewPublicThread(ctx, "390201"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if err = pool.QueryRow(ctx, records).Scan(&after); err != nil || before != after {
		t.Fatal("source reads mutated original rows/counters", err)
	}
	check("32 concurrent source reads preserve all original row bytes and counters")
	exec := func(sql string) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	preview := func() knowledge.PublicThreadCandidate {
		t.Helper()
		got, e := reader.PreviewPublicThread(ctx, "390201")
		if e != nil {
			t.Fatal(e)
		}
		return got
	}
	for _, edit := range []string{"title='Owned renamed source'", "content='PUBLIC09A edited body'", "category_id=390112"} {
		old := preview()
		exec("UPDATE threads SET " + edit + " WHERE id=390201")
		c, e := reader.RecheckPublicThread(ctx, old.Reference)
		deny(c, e, knowledge.ErrSourceChanged)
		current := preview()
		if current.Reference == old.Reference {
			t.Fatal("source edit reused reference")
		}
	}
	check("title body and category move invalidate exact references")
	old := preview()
	exec("BEGIN; UPDATE threads SET publication_status='private' WHERE id=390201; UPDATE threads SET publication_status='published' WHERE id=390201; COMMIT")
	c, e = reader.RecheckPublicThread(ctx, old.Reference)
	deny(c, e, knowledge.ErrSourceChanged)
	if preview().Content != old.Content {
		t.Fatal("ABA test changed content")
	}
	check("same transaction public private public ABA never revives reference")
	for _, tc := range []struct{ name, set, restore string }{
		{"private", "publication_status='private'", "publication_status='published'"},
		{"draft", "publication_status='draft'", "publication_status='published'"},
		{"pending", "moderation_status='pending'", "moderation_status='clear'"},
		{"taken_down", "moderation_status='taken_down'", "moderation_status='clear'"},
		{"rejected", "moderation_status='rejected'", "moderation_status='clear'"},
		{"trashed", "deletion_status='trashed'", "deletion_status='active'"},
		{"soft_deleted", "deleted_at=NOW()", "deleted_at=NULL"},
	} {
		old := preview()
		exec("UPDATE threads SET " + tc.set + " WHERE id=390201")
		c, e := reader.PreviewPublicThread(ctx, "390201")
		deny(c, e, knowledge.ErrSourceUnavailable)
		c, e = reader.RecheckPublicThread(ctx, old.Reference)
		deny(c, e, knowledge.ErrSourceUnavailable)
		exec("UPDATE threads SET " + tc.restore + " WHERE id=390201")
		check(tc.name + " revokes preview and old reference")
	}
	for _, set := range []string{"content_format='html'", "thread_type='article'", "thread_type='mutual_aid'", "thread_type='secondhand'"} {
		exec("UPDATE threads SET " + set + " WHERE id=390201")
		c, e := reader.PreviewPublicThread(ctx, "390201")
		deny(c, e, knowledge.ErrUnsupportedSource)
		exec("UPDATE threads SET content_format='markdown',thread_type='discussion' WHERE id=390201")
	}
	check("HTML and three Feature-owned thread types require separate adapters")
	exec("UPDATE threads SET content=repeat('x',65537) WHERE id=390201")
	c, e = reader.PreviewPublicThread(ctx, "390201")
	deny(c, e, knowledge.ErrSourceTooLarge)
	exec("UPDATE threads SET content='PUBLIC09A edited body' WHERE id=390201")
	check("source byte budget enforced against actual PostgreSQL content")
	old = preview()
	exec("UPDATE threads SET view_count=view_count+1,updated_at=NOW() WHERE id=390201")
	if c, e = reader.RecheckPublicThread(ctx, old.Reference); e != nil || c != old {
		t.Fatal("unrelated counters invalidated source", e)
	}
	check("counter and wall clock updates do not invalidate semantic reference")
	if _, e = s.features.Request(knowledge.FeatureID, false); e != nil {
		t.Fatal(e)
	}
	c, e = reader.RecheckPublicThread(ctx, old.Reference)
	deny(c, e, knowledge.ErrDisabled)
	if _, e = s.features.Request(knowledge.FeatureID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = reader.RecheckPublicThread(ctx, old.Reference); e != nil {
		t.Fatal(e)
	}
	check("Feature disable blocks old references and current reenable uses live facts")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	c, e = reader.PreviewPublicThread(cancelled, "390201")
	deny(c, e, context.Canceled)
	check("cancelled read releases no content")
	proof["checks"] = checks
	proof["check_count"] = len(checks)
	wire, e := json.MarshalIndent(proof, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "target-proof.json"), append(wire, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	t.Logf("V12_KNOWLEDGE_PUBLIC_SOURCE_PROOF %d production component checks passed", len(checks))
}
