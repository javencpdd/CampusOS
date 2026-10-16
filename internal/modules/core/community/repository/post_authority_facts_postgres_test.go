package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func postFactsPG(t *testing.T, id string) (*PgPostRepository, *pgxpool.Pool, *domain.Post) {
	t.Helper()
	pool, _ := businessActorPG(t)
	r := NewPgPostRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	p := &domain.Post{ID: id, ThreadID: "119201", AuthorID: "119101", AuthorName: "Post facts", Content: "original", Status: "published", CreatedAt: now, UpdatedAt: now, FactsVersion: 999, Metadata: json.RawMessage(`{"big":9007199254740993}`)}
	if err := r.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return r, pool, p
}

func TestPostAuthorityPostgreSQLManagedVersions(t *testing.T) {
	r, pool, p := postFactsPG(t, "121201")
	ctx := context.Background()
	get := func() *domain.Post {
		v, err := r.ReadPostCommandSnapshot(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	first := get()
	if first.FactsVersion < 1 || first.FactsVersion == 999 {
		t.Fatal("caller version accepted", first)
	}
	if _, err := pool.Exec(ctx, `UPDATE posts SET facts_version=-1,like_count=like_count+1,updated_at=NOW() WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if get().FactsVersion != first.FactsVersion {
		t.Fatal("counter/caller version changed authority version")
	}
	for _, q := range []string{`UPDATE posts SET content='other' WHERE id=$1`, `UPDATE posts SET content='original' WHERE id=$1`, `UPDATE posts SET metadata='{"big":9007199254740994}' WHERE id=$1`, `UPDATE posts SET metadata='{"big":9007199254740993}' WHERE id=$1`} {
		if _, err := pool.Exec(ctx, q, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	second := get()
	if second.FactsVersion <= first.FactsVersion {
		t.Fatal("facts ABA reused version")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var allocated int64
	if err := tx.QueryRow(ctx, `UPDATE posts SET content='rollback' WHERE id=$1 RETURNING facts_version`, p.ID).Scan(&allocated); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if get().FactsVersion != second.FactsVersion {
		t.Fatal("rollback changed row version")
	}
	if _, err := pool.Exec(ctx, `UPDATE posts SET content='next' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	third := get()
	if third.FactsVersion <= allocated {
		t.Fatal("rollback allocation reused")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM posts WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	p.FactsVersion = first.FactsVersion
	if err := r.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if get().FactsVersion <= third.FactsVersion {
		t.Fatal("delete/reinsert reused version")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE posts DISABLE TRIGGER trg_posts_facts_version`); err != nil {
		t.Fatal(err)
	}
	_, readErr := r.ReadPostCommandSnapshot(ctx, p.ID)
	if _, err := pool.Exec(ctx, `ALTER TABLE posts ENABLE TRIGGER trg_posts_facts_version`); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(readErr, ErrPostFactsUnavailable) {
		t.Fatal("disabled guard accepted", readErr)
	}
}

func TestPostAuthorityPostgreSQLLocksCASAndActorRetention(t *testing.T) {
	r, pool, p := postFactsPG(t, "121202")
	ctx := context.Background()
	if _, err := r.LockPostForWrite(ctx, p.ID); !errors.Is(err, ErrThreadWriteTransactionRequired) {
		t.Fatal(err)
	}
	originalVersion := p.FactsVersion
	err := transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		current, err := r.LockPostForWrite(tx, p.ID)
		if err != nil {
			return err
		}
		_, lockErr := pool.Exec(ctx, `SELECT id FROM posts WHERE id=$1 FOR UPDATE NOWAIT`, p.ID)
		var pe *pgconn.PgError
		if !errors.As(lockErr, &pe) || pe.Code != "55P03" {
			t.Fatal("actual post row lock absent", lockErr)
		}
		current.Content = "CAS updated"
		current.LikeCount = 999
		if err := r.UpdatePostIfFactsVersion(tx, current, originalVersion); err != nil {
			return err
		}
		if current.FactsVersion <= originalVersion || current.LikeCount != 0 {
			t.Fatal("post CAS facts/counters lost", current)
		}
		if err := r.UpdatePostIfFactsVersion(tx, current, originalVersion); !errors.Is(err, ErrPostFactsConflict) {
			t.Fatal("stale write accepted", err)
		}
		return r.DeletePostIfFactsVersion(tx, p.ID, current.FactsVersion, identityport.PrincipalRef{Kind: "admin", ID: "119101"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetByID(ctx, p.ID); !errors.Is(err, ErrPostNotFound) {
		t.Fatal("deleted body exposed through normal get", err)
	}
	deleted, err := r.ReadPostCommandSnapshot(ctx, p.ID)
	if err != nil || deleted.DeletedAt == nil || deleted.Status != "deleted" || deleted.DeletedActorKind != "admin" || deleted.DeletedActorID != "119101" || deleted.AuthorID != "119101" {
		t.Fatal("same-numeric actor domains mixed", deleted, err)
	}
	_, source, _, _ := runtime.Caller(0)
	down, err := os.ReadFile(filepath.Join(filepath.Dir(source), "../../../../..", "migrations/000019_v1_2_post_authority_facts.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	before := deleted.FactsVersion
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, string(down))
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != "P0001" {
		tx.Rollback(ctx)
		t.Fatal("typed deletion history erased on down", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := r.ReadPostCommandSnapshot(ctx, p.ID)
	if err != nil || current.FactsVersion != before || current.DeletedActorKind != "admin" || current.DeletedActorID != "119101" {
		t.Fatal("rejected down changed actor history", current, err)
	}
	// Opaque Admins require no coercion, and actor shape cannot precede deletion.
	if _, err := pool.Exec(ctx, `UPDATE posts SET deleted_actor_id='adm.actual' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE posts SET deleted_at=NULL WHERE id=$1`, p.ID)
	if !errors.As(err, &pe) || pe.Code != "23514" {
		t.Fatal("typed deletion actor without deletion accepted", err)
	}
}
