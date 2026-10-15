package space

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"reflect"
	"testing"
	"time"
)

func TestSpaceFactsPostgreSQLWholeRowsMetadataABAAndReinsert(t *testing.T) {
	ctx := context.Background()
	_, repo, _, pool, _, _ := filesPostgres(t, "220201")
	snapshot := &StyleSnapshot{ID: "2202018", UserID: "220201", SnapshotType: "before_apply", Theme: "default", Layout: "blog", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := repo.SaveStyleSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	raw, err := repo.LoadSpaceAuthority(ctx, "220201", false)
	if err != nil {
		t.Fatal(err)
	}
	space, err := repo.GetByUserID(ctx, "220201")
	if err != nil || space.FactsVersion != spaceFactVersion(t, raw) {
		t.Fatal(space, err, string(raw))
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 26 {
		t.Fatal("actual full row not captured", len(fields), string(raw))
	}
	for _, field := range []string{"title", "bio", "visibility", "style_manifest", "disabled_reason", "created_at", "sync_categories", "disabled_actor_id"} {
		t.Run(field, func(t *testing.T) {
			before, err := repo.LoadSpaceAuthority(ctx, "220201", false)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			var old string
			if err := tx.QueryRow(ctx, "SELECT to_jsonb(s)::text FROM user_spaces s WHERE user_id=$1", space.UserID).Scan(&old); err != nil {
				t.Fatal(err)
			}
			value := map[string]string{"title": "'changed title'", "bio": "'changed bio'", "visibility": "'private'", "style_manifest": "'{\"tokens\":{\"color\":\"red\"}}'::jsonb", "disabled_reason": "'changed reason'", "created_at": "created_at+interval '1 second'", "sync_categories": "ARRAY['changed']::text[]", "disabled_actor_id": "'changed provenance'"}[field]
			if field == "disabled_actor_id" {
				if _, err := tx.Exec(ctx, "UPDATE user_spaces SET disabled_by='changed provenance' WHERE user_id=$1", space.UserID); err != nil {
					t.Fatal(err)
				}
			} else if _, err := tx.Exec(ctx, "UPDATE user_spaces SET "+field+"="+value+" WHERE user_id=$1", space.UserID); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `UPDATE user_spaces s SET (title,bio,visibility,style_manifest,disabled_reason,created_at,sync_categories,disabled_by,disabled_actor_kind,disabled_actor_id)=
 (SELECT x.title,x.bio,x.visibility,x.style_manifest,x.disabled_reason,x.created_at,x.sync_categories,x.disabled_by,x.disabled_actor_kind,x.disabled_actor_id FROM jsonb_populate_record(NULL::user_spaces,$2::jsonb)x) WHERE user_id=$1`, space.UserID, old); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			after, err := repo.LoadSpaceAuthority(ctx, "220201", false)
			if err != nil || spaceFactVersion(t, after) <= spaceFactVersion(t, before) {
				t.Fatal("actual metadata ABA reused proof", field, err, string(before), string(after))
			}
		})
	}
	before, err := repo.LoadSpaceAuthority(ctx, "220201", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE user_spaces SET last_sync_at=NOW(),last_sync_error='operational',updated_at=NOW(),facts_version=1 WHERE user_id=$1", space.UserID); err != nil {
		t.Fatal(err)
	}
	after, err := repo.LoadSpaceAuthority(ctx, "220201", false)
	if err != nil || spaceFactVersion(t, before) != spaceFactVersion(t, after) {
		t.Fatal("operational metadata/forged input changed nonce", err)
	}
	oldRows, err := repo.LoadStyleSnapshotAuthority(ctx, space.UserID, false)
	if err != nil || len(oldRows) != 1 {
		t.Fatal(oldRows, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE user_space_style_snapshots SET created_at=created_at+interval '1 second' WHERE id=$1", snapshot.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE user_space_style_snapshots SET created_at=$2 WHERE id=$1", snapshot.ID, snapshot.CreatedAt); err != nil {
		t.Fatal(err)
	}
	newRows, err := repo.LoadStyleSnapshotAuthority(ctx, space.UserID, false)
	if err != nil || spaceFactVersion(t, newRows[0]) <= spaceFactVersion(t, oldRows[0]) {
		t.Fatal("snapshot ordering ABA reused proof", err)
	}
	var stored string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(s)::text FROM user_space_style_snapshots s WHERE id=$1", snapshot.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM user_space_style_snapshots WHERE id=$1", snapshot.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO user_space_style_snapshots SELECT * FROM jsonb_populate_record(NULL::user_space_style_snapshots,$1::jsonb)", stored); err != nil {
		t.Fatal(err)
	}
	reinserted, err := repo.LoadStyleSnapshotAuthority(ctx, space.UserID, false)
	if err != nil || spaceFactVersion(t, reinserted[0]) <= spaceFactVersion(t, newRows[0]) {
		t.Fatal("reinsert reused facts", err)
	}
}
func TestSpaceFactsPostgreSQLActualRowEmptyAdmissionAndOuterRollback(t *testing.T) {
	ctx := context.Background()
	_, repo, _, pool, _, _ := filesPostgres(t, "220202")
	base, err := repo.LoadSpaceAuthority(ctx, "220202", false)
	if err != nil {
		t.Fatal(err)
	}
	err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		if _, err := repo.LoadSpaceAuthority(tx, "220202", true); err != nil {
			return err
		}
		empty, err := repo.LoadStyleSnapshotAuthority(tx, "220202", true)
		if err != nil || empty == nil || len(empty) != 0 {
			t.Fatal(empty, err)
		}
		_, err = pool.Exec(ctx, "SELECT id FROM user_spaces WHERE user_id=$1 FOR UPDATE NOWAIT", "220202")
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Fatal("actual space row lock missing", err)
		}
		attempt, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		snapshot := &StyleSnapshot{ID: "2202028", UserID: "220202", CreatedAt: time.Now()}
		if err := repo.SaveStyleSnapshot(attempt, snapshot); err == nil || attempt.Err() == nil {
			t.Fatal("empty inventory admission did not block supported writer", err, attempt.Err())
		}
		space, err := repo.GetByUserID(tx, "220202")
		if err != nil {
			return err
		}
		space.Bio = "actual transaction change"
		if err = repo.Upsert(tx, space); err != nil {
			return err
		}
		if err = repo.SaveStyleSnapshot(tx, snapshot); err != nil {
			return err
		}
		rows, err := repo.LoadStyleSnapshotAuthority(tx, "220202", true)
		if err != nil || len(rows) != 1 {
			t.Fatal(rows, err)
		}
		if err = repo.SetDisabled(tx, "220202", nil, "", "rolled back"); err != nil {
			return err
		}
		return errors.New("required command rejected")
	})
	if err == nil {
		t.Fatal("missing rollback")
	}
	after, err := repo.LoadSpaceAuthority(ctx, "220202", false)
	if err != nil || !reflect.DeepEqual(base, after) {
		t.Fatal("context executor escaped transaction", err, string(after))
	}
	rows, err := repo.LoadStyleSnapshotAuthority(ctx, "220202", false)
	if err != nil || len(rows) != 0 {
		t.Fatal("snapshot escaped transaction", rows, err)
	}
	snapshot := &StyleSnapshot{ID: "2202029", UserID: "220202", CreatedAt: time.Now()}
	if err = repo.SaveStyleSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
		if _, err := repo.LoadStyleSnapshotAuthority(tx, "220202", true); err != nil {
			return err
		}
		_, err := pool.Exec(ctx, "SELECT id FROM user_space_style_snapshots WHERE id=$1 FOR UPDATE NOWAIT", snapshot.ID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Fatal("actual snapshot lock missing", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestSpaceFactsPostgreSQLMissingGuardsAndNontransactionLocksClosed(t *testing.T) {
	ctx := context.Background()
	_, repo, _, pool, _, _ := filesPostgres(t, "220203")
	if _, err := repo.LoadSpaceAuthority(ctx, "220203", true); !errors.Is(err, ErrSpaceFactsUnavailable) {
		t.Fatal(err)
	}
	for _, table := range []string{"user_spaces", "user_space_style_snapshots"} {
		trigger := map[string]string{"user_spaces": "trg_user_spaces_facts_version", "user_space_style_snapshots": "trg_space_style_snapshots_facts_version"}[table]
		if _, err := pool.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DISABLE TRIGGER %s", table, trigger)); err != nil {
			t.Fatal(err)
		}
		if err := repo.CheckSpaceAuthority(ctx); !errors.Is(err, ErrSpaceFactsUnavailable) {
			t.Fatal("disabled actual guard accepted", table, err)
		}
		if _, err := repo.LoadStyleSnapshotAuthority(ctx, "absent-owner", false); !errors.Is(err, ErrSpaceFactsUnavailable) {
			t.Fatal("empty read ignored invalid guard", err)
		}
		if _, err := pool.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ENABLE TRIGGER %s", table, trigger)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "ALTER SEQUENCE personal_space_facts_version_seq CYCLE"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckSpaceAuthority(ctx); !errors.Is(err, ErrSpaceFactsUnavailable) {
		t.Fatal("cycle accepted", err)
	}
	if _, err := pool.Exec(ctx, "ALTER SEQUENCE personal_space_facts_version_seq NO CYCLE"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckSpaceAuthority(ctx); err != nil {
		t.Fatal("current restored guard unavailable", err)
	}
}
