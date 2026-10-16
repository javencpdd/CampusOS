package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func businessActorPG(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_author_write" {
		t.Fatal("business actor tests require owned campusos_v12_02b_author_write database")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("source location unavailable")
	}
	down, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../..", "migrations/000018_v1_2_business_actor_domains.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return pool, string(down)
}

func TestBusinessActorPostgreSQLDomainsAndRollbackGuard(t *testing.T) {
	pool, down := businessActorPG(t)
	ctx := context.Background()
	rows := []struct{ table, key, value, legacy, kind, ref string }{
		{"threads", "id", "119201", "moderation_by", "moderation_actor_kind", "moderation_actor_id"},
		{"content_revisions", "id", "119301", "created_by", "created_actor_kind", "created_actor_id"},
		{"content_moderation_cases", "id", "119401", "opened_by", "opened_actor_kind", "opened_actor_id"},
		{"content_moderation_cases", "id", "119401", "resolved_by", "resolved_actor_kind", "resolved_actor_id"},
		{"content_moderation_actions", "id", "119501", "actor_id", "actor_kind", "actor_ref_id"},
		{"richtext_article_contents", "id", "119601", "updated_by", "updated_actor_kind", "updated_actor_id"},
		{"asset_lifecycle_audits", "id", "119701", "actor_user_id", "actor_kind", "actor_ref_id"},
		{"user_spaces", "id", "119801", "disabled_by", "disabled_actor_kind", "disabled_actor_id"},
		{"user_storage_quotas", "user_id", "119101", "updated_by", "updated_actor_kind", "updated_actor_id"},
	}
	for _, r := range rows {
		t.Run(r.table+"/"+r.legacy, func(t *testing.T) {
			read := `SELECT ` + r.kind + `,` + r.ref + `,COALESCE(` + r.legacy + `::text,'') FROM ` + r.table + ` WHERE ` + r.key + `=$1`
			var originalKind, originalRef, originalLegacy string
			if err := pool.QueryRow(ctx, read, r.value).Scan(&originalKind, &originalRef, &originalLegacy); err != nil {
				t.Fatal(err)
			}
			expected := "user"
			if r.table == "user_spaces" {
				expected = "legacy_unknown"
			}
			if originalKind != expected || originalRef != originalLegacy {
				t.Fatal("legacy provenance lost", originalKind, originalRef, originalLegacy)
			}
			clear := "NULL"
			if r.table == "user_spaces" {
				clear = "''"
			}
			update := `UPDATE ` + r.table + ` SET ` + r.legacy + `=` + clear + `,` + r.kind + `='admin',` + r.ref + `=$2 WHERE ` + r.key + `=$1`
			for _, adminID := range []string{"119101", "adm.actual.119101"} {
				if _, err := pool.Exec(ctx, update, r.value, adminID); err != nil {
					t.Fatal("opaque Admin entered legacy User column", err)
				}
				var kind, ref, legacy string
				if err := pool.QueryRow(ctx, read, r.value).Scan(&kind, &ref, &legacy); err != nil {
					t.Fatal(err)
				}
				if kind != "admin" || ref != adminID || legacy != "" {
					t.Fatal("actor domains mixed", kind, ref, legacy)
				}
				var before, after string
				rowSQL := `SELECT to_jsonb(t)::text FROM ` + r.table + ` t WHERE ` + r.key + `=$1`
				if err := pool.QueryRow(ctx, rowSQL, r.value).Scan(&before); err != nil {
					t.Fatal(err)
				}
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				_, err = tx.Exec(ctx, down)
				var pe *pgconn.PgError
				if !errors.As(err, &pe) || pe.Code != "P0001" {
					tx.Rollback(ctx)
					t.Fatal("domain-losing down accepted", err)
				}
				if err := tx.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, rowSQL, r.value).Scan(&after); err != nil || before != after {
					t.Fatal("rejected down changed actor history", before, after, err)
				}
			}
			// An Admin plus a numeric legacy User is rejected even when IDs are equal.
			_, err := pool.Exec(ctx, `UPDATE `+r.table+` SET `+r.legacy+`=$2 WHERE `+r.key+`=$1`, r.value, originalLegacy)
			// A legacy-only trusted update deliberately infers the old User/unknown
			// provenance. Explicit incompatible typed writes must still fail.
			if err != nil {
				t.Fatal(err)
			}
			_, err = pool.Exec(ctx, `UPDATE `+r.table+` SET `+r.kind+`='admin',`+r.ref+`='adm.incompatible' WHERE `+r.key+`=$1`, r.value)
			var pe *pgconn.PgError
			if !errors.As(err, &pe) || pe.Code != "23514" {
				t.Fatal("incompatible legacy+Admin domain accepted", err)
			}
			_, err = pool.Exec(ctx, `UPDATE `+r.table+` SET `+r.kind+`='anonymous' WHERE `+r.key+`=$1`, r.value)
			if !errors.As(err, &pe) || pe.Code != "23514" {
				t.Fatal("anonymous actor persisted", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE `+r.table+` SET `+r.legacy+`=$2,`+r.kind+`=$3,`+r.ref+`=$4 WHERE `+r.key+`=$1`, r.value, originalLegacy, originalKind, originalRef); err != nil {
				t.Fatal(err)
			}
		})
	}
	// No domain was invented for a NULL or untyped historical text/system actor.
	for _, q := range []string{`SELECT created_actor_kind='legacy_unknown' AND created_actor_id='' FROM content_revisions WHERE id=119302`, `SELECT actor_kind='legacy_unknown' AND actor_ref_id='' AND actor_type='system' FROM asset_lifecycle_audits WHERE id=119702`, `SELECT actor_kind='user' AND actor_ref_id='119101' AND actor_type='admin' FROM asset_lifecycle_audits WHERE id=119701`} {
		var ok bool
		if err := pool.QueryRow(ctx, q).Scan(&ok); err != nil || !ok {
			t.Fatal("historical domain incorrectly inferred", q, err)
		}
	}
	var first, second, third int64
	if err := pool.QueryRow(ctx, `SELECT facts_version FROM threads WHERE id=119201`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `UPDATE threads SET moderation_by=NULL,moderation_actor_kind='admin',moderation_actor_id='adm.aba' WHERE id=119201 RETURNING facts_version`).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `UPDATE threads SET moderation_by=119101,moderation_actor_kind='user',moderation_actor_id='119101' WHERE id=119201 RETURNING facts_version`).Scan(&third); err != nil {
		t.Fatal(err)
	}
	if !(first < second && second < third) {
		t.Fatal("actor ABA reused Thread facts version", first, second, third)
	}
	if err := pool.QueryRow(ctx, `UPDATE threads SET view_count=view_count+1 WHERE id=119201 RETURNING facts_version`).Scan(&second); err != nil || second != third {
		t.Fatal("display counter changed facts version", second, third, err)
	}
}

func TestBusinessActorPostgreSQLDeletedUserHistory(t *testing.T) {
	pool, down := businessActorPG(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES(119102,'actor-deleted-user','Actor Deleted','actor-deleted@example.test','active',1);
 INSERT INTO asset_lifecycle_audits(id,actor_user_id,actor_type,action,reason) VALUES(119703,119102,'user','trashed','Historic credential');
 UPDATE user_storage_quotas SET updated_by=119102 WHERE user_id=119101;
 DELETE FROM users WHERE id=119102;`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`SELECT actor_user_id IS NULL AND actor_kind='user' AND actor_ref_id='119102' FROM asset_lifecycle_audits WHERE id=119703`, `SELECT updated_by IS NULL AND updated_actor_kind='user' AND updated_actor_id='119102' FROM user_storage_quotas WHERE user_id=119101`} {
		var ok bool
		if err := pool.QueryRow(ctx, q).Scan(&ok); err != nil || !ok {
			t.Fatal("FK SET NULL erased historical domain actor", q, err)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, down)
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != "P0001" {
		t.Fatal("loss of deleted User history accepted", err)
	}
}
