package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

func publicReadPostgres(t *testing.T) (*repository.PgThreadRepository, *pgxpool.Pool, context.Context) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("CAMPUSOS_COMMUNITY_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("CAMPUSOS_COMMUNITY_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_public" {
		t.Fatal("public detail tests require the owned campusos_v12_02b_public drill database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
DELETE FROM threads WHERE id BETWEEN 97200 AND 97299;
INSERT INTO users (id,username,nickname,email) VALUES
 (97101,'public-policy-owner','Owner','public-policy-owner@test.local'),
 (97102,'public-policy-other','Other','public-policy-other@test.local') ON CONFLICT (id) DO NOTHING;
INSERT INTO categories (id,name,slug,node_kind,lifecycle_status) VALUES
 (97110,'Public policy group','public-policy-group','group','active') ON CONFLICT (id) DO NOTHING;
INSERT INTO categories (id,name,slug,parent_id,node_kind,lifecycle_status) VALUES
 (97111,'Public policy board','public-policy-board',97110,'board','active'),
 (97112,'Public policy other board','public-policy-other-board',97110,'board','active') ON CONFLICT (id) DO NOTHING;
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES
 (97111,'discussion',TRUE),(97112,'discussion',TRUE) ON CONFLICT (category_id,thread_type) DO NOTHING;`)
	if err != nil {
		t.Fatal(err)
	}
	return repository.NewPgThreadRepository(pool), pool, ctx
}

func TestPublicThreadDetailPostgreSQLRevocation(t *testing.T) {
	repo, _, _ := publicReadPostgres(t)
	exercisePublicReadRevocation(t, repo, "9720")
}

// Direct SQL writers share the fact epoch with the repository. Content,
// ownership, move and state edits advance it even when current_revision and
// updated_at are deliberately unchanged; counters never invalidate a read.
func TestPublicThreadDetailPostgreSQLVersion(t *testing.T) {
	repo, pool, ctx := publicReadPostgres(t)
	thread := publicReadFixture(t, repo, "97220")
	initial := thread.FactsVersion
	if initial <= 0 {
		t.Fatal("create did not return server version")
	}
	for _, update := range []string{
		"publication_status='private'", "publication_status='published'",
		"moderation_status='taken_down'", "moderation_status='clear'",
		"deletion_status='trashed'", "deletion_status='active'",
		"content='new body'", "title='new title'", "content_format='html'",
		"author_id=97102", "category_id=97112", "tags=ARRAY['new-tag']::text[]",
		"current_revision=current_revision+1",
	} {
		if _, err := pool.Exec(ctx, "UPDATE threads SET "+update+" WHERE id=97220"); err != nil {
			t.Fatal(err)
		}
		current, err := repo.GetByID(ctx, "97220")
		if err != nil || current.FactsVersion <= thread.FactsVersion {
			t.Fatalf("version failed to advance for %s: current=%+v err=%v", update, current, err)
		}
		thread = current
	}
	if _, err := repo.RecheckPublicThread(ctx, thread.ID, initial, false); !errors.Is(err, repository.ErrThreadNotFound) {
		t.Fatalf("stale ABA version accepted: %v", err)
	}
	for _, update := range []string{"view_count=view_count+1", "reply_count=reply_count+1", "like_count=like_count+1", "updated_at=NOW()", "facts_version=1"} {
		if _, err := pool.Exec(ctx, "UPDATE threads SET "+update+" WHERE id=97220"); err != nil {
			t.Fatal(err)
		}
		current, err := repo.GetByID(ctx, thread.ID)
		if err != nil || current.FactsVersion != thread.FactsVersion {
			t.Fatalf("non-fact write/reset changed version %s: current=%+v err=%v", update, current, err)
		}
	}
	if _, err := repo.RecheckPublicThread(ctx, thread.ID, thread.FactsVersion, true); err != nil {
		t.Fatal(err)
	}
	aba, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer aba.Rollback(ctx)
	var privateVersion, restoredVersion int64
	if err = aba.QueryRow(ctx, "UPDATE threads SET publication_status='private' WHERE id=97220 RETURNING facts_version").Scan(&privateVersion); err != nil {
		t.Fatal(err)
	}
	if err = aba.QueryRow(ctx, "UPDATE threads SET publication_status='published' WHERE id=97220 RETURNING facts_version").Scan(&restoredVersion); err != nil {
		t.Fatal(err)
	}
	if err = aba.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if restoredVersion <= privateVersion || privateVersion <= thread.FactsVersion {
		t.Fatal("same-transaction ABA reused version")
	}
	thread.FactsVersion = restoredVersion
	// Sequence allocation is not rolled back, so a discarded update cannot
	// reuse its version for a later committed update.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var rolledVersion int64
	if err = tx.QueryRow(ctx, "UPDATE threads SET content='rolled body' WHERE id=97220 RETURNING facts_version").Scan(&rolledVersion); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	thread.Content = "committed after rollback"
	if err = repo.Update(ctx, thread); err != nil {
		t.Fatal(err)
	}
	if thread.FactsVersion <= rolledVersion {
		t.Fatal("rollback allocation reused")
	}
	// Physical deletion and reinsertion at the same ID also get a fresh epoch.
	if _, err = pool.Exec(ctx, "DELETE FROM threads WHERE id=97220"); err != nil {
		t.Fatal(err)
	}
	recreated := publicReadFixture(t, repo, "97220")
	if recreated.FactsVersion <= thread.FactsVersion {
		t.Fatal("recreated ID reused a version")
	}
}

func TestPublicThreadDetailPostgreSQLCommitRace(t *testing.T) {
	repo, pool, ctx := publicReadPostgres(t)
	thread := publicReadFixture(t, repo, "97230")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "UPDATE threads SET publication_status='private',content='commit-private-body' WHERE id=97230"); err != nil {
		t.Fatal(err)
	}
	// The final UPDATE waits for the writer. After commit PostgreSQL must
	// re-evaluate its version/public predicate instead of returning private data.
	done := make(chan error, 1)
	go func() {
		_, err := repo.RecheckPublicThread(ctx, thread.ID, thread.FactsVersion, true)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("final view read did not wait for writer: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, repository.ErrThreadNotFound) {
			t.Fatalf("committed revocation accepted: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("final read did not finish after commit")
	}
	current, err := repo.GetByID(ctx, thread.ID)
	if err != nil || current.ViewCount != 0 {
		t.Fatalf("revoked read counted: %+v %v", current, err)
	}
	if got, err := NewThreadService(repo, nil).GetPublicThread(ctx, thread.ID); got != nil || !errors.Is(err, repository.ErrThreadNotFound) {
		t.Fatalf("new request read private commit: %+v %v", got, err)
	}
}
