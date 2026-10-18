package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	communitydomain "github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	campusossdk "github.com/campusos/CampusOS/sdk/go"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func v5ManagedOwnedPool(t *testing.T) (context.Context, *pgxpool.Pool, string) {
	t.Helper()
	dsn, work := os.Getenv("CAMPUSOS_V5_MANAGED_DATABASE_URL"), os.Getenv("CAMPUSOS_V5_MANAGED_OUTPUT")
	if dsn == "" {
		t.Skip("requires owned03c exit database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_03c_exit" || work == "" {
		t.Fatal("requires exact owned03c exit database/output")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("owned database unavailable")
	}
	t.Cleanup(pool.Close)
	if os.MkdirAll(work, 0700) != nil {
		t.Fatal("owned output unavailable")
	}
	return ctx, pool, work
}

// Delegates every public read to the actual checked Core service/repository.
// The author branch is deliberately unavailable; no raw repository fallback.
type v5PGPublicQuery struct {
	service *communityservice.ThreadService
}

func (q v5PGPublicQuery) GetPublicThread(ctx context.Context, id string) (*communitydomain.Thread, error) {
	return q.service.GetPublicThread(ctx, id)
}
func (q v5PGPublicQuery) ListPublicThreads(ctx context.Context, f communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error) {
	return q.service.ListPublicThreads(ctx, f)
}
func (q v5PGPublicQuery) ListAuthorThreads(context.Context, string, communitydomain.ThreadListFilter) ([]*communitydomain.Thread, int64, error) {
	return nil, 0, ErrV5Denied
}

func TestPostgresV5ManagedExit(t *testing.T) {
	ctx, pool, work := v5ManagedOwnedPool(t)
	// A five-second APPROVED contract budget accommodates native race+PG
	// verification cost. Native negative tests retain short deadlines.
	v, _, _ := newV5AuthorizedManagedFixture(t, 5000)
	trust := append([]byte(nil), v.s.catalog.trust...)
	admins, proof, _ := v5PGAdmin(t, ctx, pool)
	files, err := NewV5ManagedFiles(filepath.Join(work, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	catalog, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	fault := &v5ReliableFault{Store: reliability.NewPostgreSQLStore(pool)}
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), fault)
	v.s = NewV5Platform(NewPgV5StateStore(pool), files, catalog, reliable, admins)
	v.s.Users = v5ChatPGUsers{repository.NewPgUserRepository(pool)}
	v.admin = proof
	i := v.installed(t)
	v.mutate(t, V5Mutation{Kind: "grant", IdempotencyKey: "grant", Approved: true, Consumes: v.manifest.Consumes, Resources: v.manifest.HostResources}, true, proof)
	i, err = v.s.installation(ctx, v.c, "")
	if err != nil {
		t.Fatal(err)
	}
	globalOp := v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "enable"}, true, proof)
	v5ContributionPGReady(t, ctx, v.s, globalOp)
	sessions, userProof, sid := v5ChatPGSession(t, ctx, pool, reliable)
	userOp := v.mutate(t, V5Mutation{Kind: "install", IdempotencyKey: "user-install"}, false, userProof)
	v5ContributionPGReady(t, ctx, v.s, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: userOp.Generation, IdempotencyKey: "user-enable"}, false, userProof))
	h := NewV5ManagedRuntime(NewV5ContributionDirectory(v.s))
	t.Cleanup(func() { _ = h.Close(ctx) })
	ui, _ := v.s.installation(ctx, v.c, "2801001")
	userLease, err := h.Publish(ctx, v.c, "2801001", ui.ActiveGeneration, 5*time.Minute, v5ManagedTestAdapter{invoke: v5ManagedReply})
	if err != nil {
		t.Fatal(err)
	}
	features := v5ChatPGFeatures(t)
	consumer := V5Consumer{Feature: features, FeatureID: "contract-chat-consumer", Authorize: func(context.Context, identity.PrincipalContext, V5ContributionDescriptor, json.RawMessage) (string, error) {
		return "rev42", nil
	}}
	callWire := func(b V5ManagedBinding) ([]byte, []byte) {
		var fields map[string]any
		_ = json.Unmarshal(v5ManagedPayload(b), &fields)
		fields["deadline_ms"] = 4000
		payload := mustV5JSON(fields)
		var call campusossdk.V5DispatchCall
		_ = json.Unmarshal(v5ManagedEnvelope(b, payload), &call)
		call.DeadlineMS = time.Now().Add(4 * time.Second).UnixMilli()
		return mustV5JSON(call), payload
	}
	t.Run("all-three-typed-calls-current-user", func(t *testing.T) {
		for _, q := range v5ContributionQueries() {
			b, err := h.Select(ctx, "2801001", q, nil)
			if err != nil {
				t.Fatal(err)
			}
			wire, raw := callWire(b)
			if _, err = h.Invoke(ctx, consumer, b, wire, raw, userProof); err != nil {
				t.Fatal(q.Point, err)
			}
		}
	})
	t.Run("actual-session-revoked-during-io", func(t *testing.T) {
		h.instances[userLease.ID].adapter = v5ManagedTestAdapter{invoke: func(c context.Context, target V5ChatTarget, raw json.RawMessage, w io.Writer) error {
			if err := sessions.RevokeCurrent(c, "2801001", sid); err != nil {
				return err
			}
			return v5ManagedReply(c, target, raw, w)
		}}
		b, err := h.Select(ctx, "2801001", v5ContributionQueries()[1], nil)
		if err != nil {
			t.Fatal(err)
		}
		wire, raw := callWire(b)
		if out, err := h.Invoke(ctx, consumer, b, wire, raw, userProof); err == nil || len(out) > 0 {
			t.Fatal("revoked session released output", err)
		}
	})
	if err := h.Withdraw(ctx, userLease.ID, true); err != nil {
		t.Fatal(err)
	}
	i, _ = v.s.installation(ctx, v.c, "")
	lease, err := h.Publish(ctx, v.c, "", i.ActiveGeneration, 5*time.Minute, v5ManagedTestAdapter{invoke: v5ManagedReply, event: func(context.Context, json.RawMessage) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(303001,'03c group','owned03c-group','group','active'); INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(303002,'03c board','owned03c-board',303001,'board','active'); INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(303002,'discussion',true); INSERT INTO threads(id,title,content,author_id,category_id,status,publication_status,moderation_status,deletion_status) VALUES(303003,'Public metadata','PRIVATE-BODY-MARKER',2801001,303002,'published','published','clear','active')`)
	if err != nil {
		t.Fatal(err)
	}
	reader := v5PGPublicQuery{communityservice.NewThreadService(communityrepo.NewPgThreadRepository(pool), nil)}
	endpoint := V5PublicThreadEndpoint(reader, features, "contract-chat-consumer")
	if err = h.BindHost("community.thread.public.read", endpoint); err != nil {
		t.Fatal(err)
	}
	hostPayload := []byte(`{"thread_id":"303003"}`)
	hostWire := func() []byte {
		return mustV5JSON(campusossdk.V5DispatchCall{RequestID: "public-thread", Contract: "campusos.plugin-dispatch/v1", Target: campusossdk.V5DispatchTarget{Kind: "host_api", ID: "community.thread.public.read"}, InstanceGeneration: i.ActiveGeneration, DeadlineMS: time.Now().Add(4 * time.Second).UnixMilli(), RequestBytes: int64(len(hostPayload)), ResponseLimitBytes: 4096})
	}
	t.Run("actual-core-public-port-and-private-denial", func(t *testing.T) {
		out, err := h.HostCall(ctx, lease.Token, hostWire(), hostPayload)
		if err != nil || bytes.Contains(out, []byte("PRIVATE")) {
			t.Fatal("public projection", err)
		}
		var fields map[string]any
		if json.Unmarshal(out, &fields) != nil || len(fields) != 4 {
			t.Fatal("unsafe Host projection")
		}
		if _, err = pool.Exec(ctx, `UPDATE threads SET publication_status='private' WHERE id=303003`); err != nil {
			t.Fatal(err)
		}
		if out, err = h.HostCall(ctx, lease.Token, hostWire(), hostPayload); err == nil || len(out) > 0 {
			t.Fatal("private resource admitted")
		}
		if _, err = pool.Exec(ctx, `UPDATE threads SET publication_status='published' WHERE id=303003`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("public-aba-during-host-io-rejected", func(t *testing.T) {
		h.mu.Lock()
		altered := endpoint
		altered.Call = func(c context.Context, owner string, s PluginV5Consume, raw json.RawMessage, w io.Writer) error {
			if _, err := pool.Exec(c, `UPDATE threads SET publication_status='private' WHERE id=303003; UPDATE threads SET publication_status='published' WHERE id=303003`); err != nil {
				return err
			}
			return endpoint.Call(c, owner, s, raw, w)
		}
		h.hosts["community.thread.public.read"] = altered
		h.mu.Unlock()
		if out, err := h.HostCall(ctx, lease.Token, hostWire(), hostPayload); err == nil || len(out) > 0 {
			t.Fatal("ABA released data")
		}
		h.mu.Lock()
		h.hosts["community.thread.public.read"] = endpoint
		h.mu.Unlock()
	})
	queue := NewPgV5EventStore(pool)
	public := func(c context.Context, kind, ref string) (bool, error) {
		if kind == "knowledge.source.public.changed/v1" {
			return ref == "knowledge.source:owned-public", nil
		}
		if kind != "community.thread.public.changed/v1" || !strings.HasPrefix(ref, "thread:") {
			return false, nil
		}
		thread, err := reader.GetPublicThread(c, strings.TrimPrefix(ref, "thread:"))
		return err == nil && thread != nil, err
	}
	events := NewV5EventService(h, queue, public)
	count := func(source string) int {
		var n int
		if pool.QueryRow(ctx, `SELECT count(*) FROM plugin_v5_event_deliveries WHERE source_event_id=$1`, source).Scan(&n) != nil {
			t.Fatal("count")
		}
		return n
	}
	t.Run("rolled-back-source-and-capture-audit-atomicity", func(t *testing.T) {
		source, _ := reliability.NewEvent("thread.updated", "thread", "303003", map[string]string{"body": "PRIVATE"})
		if reliable.Execute(ctx, reliability.Command{Code: "owned.rollback", ActorType: "system", ActorID: "owned", ResourceType: "thread", ResourceID: "303003", Event: &source}, func(context.Context) error { return errors.New("owned rollback") }) == nil {
			t.Fatal("rollback absent")
		}
		if events.CaptureCommitted(ctx, source.ID) == nil || count(source.ID) != 0 {
			t.Fatal("uncommitted source delivered")
		}
		source = v5EventSource(t, v, "thread.updated", "thread", "303003")
		fault.fail = "audit"
		err := events.CaptureCommitted(ctx, source.ID)
		fault.fail = ""
		if err == nil || count(source.ID) != 0 {
			t.Fatal("capture escaped failed required audit", err)
		}
		if err = events.CaptureCommitted(ctx, source.ID); err != nil || count(source.ID) != 1 {
			t.Fatal("capture retry", err)
		}
		if _, err = events.ProcessOnce(ctx, 16); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("three-whitelisted-types-metadata-only-idempotent", func(t *testing.T) {
		var callbacks atomic.Int32
		h.instances[lease.ID].adapter = v5ManagedTestAdapter{event: func(_ context.Context, raw json.RawMessage) error {
			callbacks.Add(1)
			if bytes.Contains(raw, []byte("PRIVATE")) {
				return errors.New("private leak")
			}
			return nil
		}}
		for _, spec := range [][3]string{{"plugin.lifecycle.changed", "plugin_v5_operation", globalOp.ID}, {"thread.updated", "thread", "303003"}, {"knowledge.source.public.changed", "knowledge_source", "owned-public"}} {
			source := v5EventSource(t, v, spec[0], spec[1], spec[2])
			for n := 0; n < 2; n++ {
				if err := events.CaptureCommitted(ctx, source.ID); err != nil {
					t.Fatal(err)
				}
			}
			if count(source.ID) != 1 {
				t.Fatal("not idempotent")
			}
		}
		if callbacks.Load() != 0 {
			t.Fatal("core capture ran plugin")
		}
		if _, err := events.ProcessOnce(ctx, 16); err != nil || callbacks.Load() != 3 {
			t.Fatal("independent delivery", err, callbacks.Load())
		}
		var leaks int
		if pool.QueryRow(ctx, `SELECT count(*) FROM plugin_v5_event_deliveries WHERE state::text LIKE '%PRIVATE%'`).Scan(&leaks) != nil || leaks != 0 {
			t.Fatal("private source persisted")
		}
	})
	t.Run("database-rejects-private-payload-and-evidence-erasure", func(t *testing.T) {
		var id string
		var raw []byte
		if pool.QueryRow(ctx, `SELECT id,state FROM plugin_v5_event_deliveries ORDER BY id LIMIT 1`).Scan(&id, &raw) != nil {
			t.Fatal("missing event evidence")
		}
		for _, nested := range []bool{false, true} {
			var row map[string]any
			_ = json.Unmarshal(raw, &row)
			row["id"] = v5RandomID()
			if nested {
				row["event"].(map[string]any)["payload"].(map[string]any)["body"] = "PRIVATE"
			} else {
				row["body"] = "PRIVATE"
			}
			_, err := pool.Exec(ctx, `INSERT INTO plugin_v5_event_deliveries(id,state) VALUES($1,$2)`, row["id"], mustV5JSON(row))
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatal("private payload escaped schema", err)
			}
		}
		for _, sql := range []string{`UPDATE plugin_v5_event_deliveries SET state=jsonb_set(state,'{owner_user_id}','"2801001"') WHERE id=$1`, `DELETE FROM plugin_v5_event_deliveries WHERE id=$1`} {
			_, err := pool.Exec(ctx, sql, id)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "P0001" {
				t.Fatal("intent/evidence not guarded", err)
			}
		}
		if _, err := pool.Exec(ctx, `TRUNCATE plugin_v5_event_deliveries`); err == nil {
			t.Fatal("evidence erased")
		}
	})
	t.Run("rollback-guard-serializes-with-pending-writer", func(t *testing.T) {
		down, err := os.ReadFile("../../migrations/000028_plugin_v5_event_deliveries.down.sql")
		if err != nil {
			t.Fatal(err)
		}
		var raw []byte
		if pool.QueryRow(ctx, `SELECT state FROM plugin_v5_event_deliveries WHERE state->>'status'='done' LIMIT 1`).Scan(&raw) != nil {
			t.Fatal("no retained evidence")
		}
		var d V5EventDelivery
		if json.Unmarshal(raw, &d) != nil {
			t.Fatal("bad evidence")
		}
		d.ID = v5RandomID()
		d.Instance = v5RandomID()
		writer, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Rollback(ctx)
		if _, err = writer.Exec(ctx, `INSERT INTO plugin_v5_event_deliveries(id,state) VALUES($1,$2)`, d.ID, mustV5JSON(d)); err != nil {
			t.Fatal(err)
		}
		finish := make(chan error, 1)
		go func() {
			tx, err := pool.Begin(ctx)
			if err != nil {
				finish <- err
				return
			}
			defer tx.Rollback(ctx)
			_, err = tx.Exec(ctx, string(down))
			finish <- err
		}()
		select {
		case err := <-finish:
			t.Fatal("down checked before pending writer finished", err)
		case <-time.After(50 * time.Millisecond):
		}
		if err = writer.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-finish:
			if err == nil || !strings.Contains(err.Error(), "cannot roll back nonempty v5 event delivery queue") {
				t.Fatal("down lost committed delivery", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("down guard did not finish")
		}
		var retained bool
		if pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM plugin_v5_event_deliveries WHERE id=$1)`, d.ID).Scan(&retained) != nil || !retained {
			t.Fatal("pending writer evidence lost")
		}
	})
	t.Run("skip-locked-single-winner-and-stale-ack", func(t *testing.T) {
		source := v5EventSource(t, v, "thread.updated", "thread", "303003")
		if err := events.CaptureCommitted(ctx, source.ID); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UnixMilli()
		var wg sync.WaitGroup
		rows := make(chan []V5EventDelivery, 8)
		errs := make(chan error, 8)
		for n := 0; n < 8; n++ {
			wg.Add(1)
			go func() { defer wg.Done(); ds, err := queue.Claim(ctx, now, 1); rows <- ds; errs <- err }()
		}
		wg.Wait()
		close(rows)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		var first V5EventDelivery
		n := 0
		for ds := range rows {
			for _, d := range ds {
				first = d
				n++
			}
		}
		if n != 1 {
			t.Fatal("claim winners", n)
		}
		again, err := queue.Claim(ctx, now+31000, 1)
		if err != nil || len(again) != 1 || again[0].Lease <= first.Lease {
			t.Fatal("lease not fenced", err)
		}
		if queue.Finish(ctx, first, now+31000, "done", "", 0) != ErrV5Conflict {
			t.Fatal("stale ack")
		}
		if err := queue.Finish(ctx, again[0], now+31000, "done", "", 0); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("blocked-callback-does-not-block-core-commit", func(t *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		defer once.Do(func() { close(release) })
		h.instances[lease.ID].adapter = v5ManagedTestAdapter{event: func(c context.Context, _ json.RawMessage) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-c.Done():
				return c.Err()
			}
		}}
		source := v5EventSource(t, v, "thread.updated", "thread", "303003")
		if err := events.CaptureCommitted(ctx, source.ID); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := events.ProcessOnce(ctx, 1); done <- err }()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("callback not started")
		}
		commitCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		if err := reliable.Execute(commitCtx, reliability.Command{Code: "owned.core-progress", ActorType: "system", ActorID: "owned", ResourceType: "thread", ResourceID: "303003"}, func(tx context.Context) error {
			_, err := transaction.ExecutorFor(tx, pool).Exec(tx, `UPDATE threads SET title='Core commit while plugin blocked' WHERE id=303003`)
			return err
		}); err != nil {
			t.Fatal("plugin blocked Core", err)
		}
		once.Do(func() { close(release) })
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("five-failures-independent-dead-letter", func(t *testing.T) {
		var calls atomic.Int32
		h.instances[lease.ID].adapter = v5ManagedTestAdapter{event: func(context.Context, json.RawMessage) error { calls.Add(1); return errors.New("PRIVATE-secret-error") }}
		source := v5EventSource(t, v, "thread.updated", "thread", "303003")
		if err := events.CaptureCommitted(ctx, source.ID); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		events.now = func() time.Time { return now }
		for n := 0; n < 6; n++ {
			now = now.Add(10 * time.Second)
			if _, err := events.ProcessOnce(ctx, 16); err != nil {
				t.Fatal(err)
			}
		}
		var status, code string
		if pool.QueryRow(ctx, `SELECT state->>'status',state->>'error_code' FROM plugin_v5_event_deliveries WHERE source_event_id=$1`, source.ID).Scan(&status, &code) != nil || status != "dead" || code != "plugin.event_dead_letter" || calls.Load() != 5 {
			t.Fatal("dead letter", status, code, calls.Load())
		}
		events.now = time.Now
	})
	t.Run("source-privacy-and-grant-revocation-recheck", func(t *testing.T) {
		var calls atomic.Int32
		h.instances[lease.ID].adapter = v5ManagedTestAdapter{event: func(context.Context, json.RawMessage) error { calls.Add(1); return nil }}
		source := v5EventSource(t, v, "thread.updated", "thread", "303003")
		if err := events.CaptureCommitted(ctx, source.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE threads SET publication_status='private' WHERE id=303003`); err != nil {
			t.Fatal(err)
		}
		if _, err := events.ProcessOnce(ctx, 16); err != nil || calls.Load() != 0 {
			t.Fatal("private callback", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE threads SET publication_status='published' WHERE id=303003`); err != nil {
			t.Fatal(err)
		}
		source = v5EventSource(t, v, "thread.updated", "thread", "303003")
		if err := events.CaptureCommitted(ctx, source.ID); err != nil {
			t.Fatal(err)
		}
		g, _ := v.s.grant(ctx, v.c)
		v.mutate(t, V5Mutation{Kind: "revoke", ExpectedRevision: g.Revision, IdempotencyKey: "event-revoke"}, true, proof)
		if _, err := h.credential(ctx, lease.Token); err == nil {
			t.Fatal("revoked credential")
		}
		if _, err := events.ProcessOnce(ctx, 16); err != nil || calls.Load() != 0 {
			t.Fatal("revoked callback", err)
		}
	})
	// Restore a NEW approved/ready generation, then leave a delivery durable for
	// the separate post-database-restart process. Never persist the bearer token.
	g, _ := v.s.grant(ctx, v.c)
	v.mutate(t, V5Mutation{Kind: "grant", ExpectedRevision: g.Revision, IdempotencyKey: "regrant", Approved: true, Consumes: v.manifest.Consumes, Resources: v.manifest.HostResources}, true, proof)
	i, _ = v.s.installation(ctx, v.c, "")
	v5ContributionPGReady(t, ctx, v.s, v.mutate(t, V5Mutation{Kind: "enable", ExpectedGeneration: i.Generation, IdempotencyKey: "reenable"}, true, proof))
	i, _ = v.s.installation(ctx, v.c, "")
	lease, err = h.Publish(ctx, v.c, "", i.ActiveGeneration, 5*time.Minute, v5ManagedTestAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	source := v5EventSource(t, v, "thread.updated", "thread", "303003")
	if err = events.CaptureCommitted(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	descriptor, err := h.Select(ctx, "", v5ContributionQueries()[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"trust.json": trust, "descriptor.json": mustV5JSON(descriptor.Descriptor()), "queued.json": mustV5JSON(source.ID)} {
		if os.WriteFile(filepath.Join(work, name), raw, 0600) != nil {
			t.Fatal("save restart evidence")
		}
	}
}

func TestPostgresV5ManagedRestart(t *testing.T) {
	ctx, pool, work := v5ManagedOwnedPool(t)
	trust, err := os.ReadFile(filepath.Join(work, "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(work, "descriptor.json"))
	var descriptor V5ContributionDescriptor
	if err != nil || json.Unmarshal(raw, &descriptor) != nil {
		t.Fatal("saved descriptor")
	}
	raw, err = os.ReadFile(filepath.Join(work, "queued.json"))
	var source string
	if err != nil || json.Unmarshal(raw, &source) != nil {
		t.Fatal("saved source")
	}
	files, err := NewV5ManagedFiles(filepath.Join(work, "managed"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	catalog, err := NewPluginV5PackageCatalogWithIdentityStore(trust, NewPgPluginV5IdentityStore(pool))
	if err != nil {
		t.Fatal(err)
	}
	reliable := reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool))
	s := NewV5Platform(NewPgV5StateStore(pool), files, catalog, reliable, nil)
	h := NewV5ManagedRuntime(NewV5ContributionDirectory(s))
	if _, err = h.Select(ctx, "", v5ContributionQueries()[1], nil); err != ErrV5ContributionUnpublished {
		t.Fatal("restart revived publication", err)
	}
	if err = s.ReconcileStartup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Publish(ctx, descriptor.V5Coordinate, "", descriptor.InstanceGeneration, time.Minute, v5ManagedTestAdapter{}); err == nil {
		t.Fatal("test must reconcile before re-publishing")
	}
	queue := NewPgV5EventStore(pool)
	events := NewV5EventService(h, queue, nil)
	if _, err = events.ProcessOnce(ctx, 50); err != nil {
		t.Fatal(err)
	}
	var status, code string
	if pool.QueryRow(ctx, `SELECT state->>'status',state->>'error_code' FROM plugin_v5_event_deliveries WHERE source_event_id=$1`, source).Scan(&status, &code) != nil || status != "dead" || code != "plugin.event_stale_generation" {
		t.Fatal("restart lost queue or revived old callback", status, code)
	}
	if _, err = s.ProcessOnce(ctx, 50); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Select(ctx, "", v5ContributionQueries()[1], nil); err != ErrV5ContributionUnpublished {
		t.Fatal("worker fabricated runtime", err)
	}
}
