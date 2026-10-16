package space

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func spaceQuotaPGState(t *testing.T, g spaceAdminPGData) string {
	t.Helper()
	base := spaceAdminPGState(t, g)
	var raw string
	err := g.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('base',$2::jsonb,
 'quota',(SELECT to_jsonb(q)FROM user_storage_quotas q WHERE user_id=$1),
 'account',(SELECT to_jsonb(a)FROM user_storage_accounts a WHERE user_id=$1),
 'objects',(SELECT COALESCE(jsonb_agg(to_jsonb(o)ORDER BY id),'[]'::jsonb)FROM storage_objects o WHERE owner_user_id=$1),
 'reservations',(SELECT COALESCE(jsonb_agg(to_jsonb(r)ORDER BY id),'[]'::jsonb)FROM user_storage_reservations r WHERE user_id=$1))::text`, g.owner, base).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func spaceQuotaFiles(t *testing.T, g spaceAdminPGData) map[string]string {
	t.Helper()
	root := g.s.fileStore.cfg.RootDir
	result := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[name] = fmt.Sprint(info.Mode(), "/", info.Size(), "/", info.ModTime().UnixNano(), "/", fmt.Sprintf("%x", sha256.Sum256(data)))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestSpaceQuotaAdminPostgreSQLActualCommandLocksTypedActorsRequiredRollbackAndNoFileLoss(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, exists := range []bool{false, true} {
		for _, opaque := range []bool{false, true} {
			for _, failure := range []string{"none", "platform_command_audits", "platform_outbox", "user_storage_quotas", "quota_aba", "user_email", "permission_aba"} {
				t.Run(fmt.Sprint(exists, "/", opaque, "/", failure), func(t *testing.T) {
					index++
					g := spaceAdminPostgres(t, 2550201+index*10, exists, opaque)
					quotaRepo := corestorage.NewPgQuotaRepository(g.pool)
					adapter := g.s.quotaManager.(*corestorage.LocalAdapter)
					if exists {
						if _, err := quotaRepo.Upsert(ctx, corestorage.QuotaRecord{UserID: g.owner, QuotaBytes: 2097152, UpdatedBy: g.owner}); err != nil {
							t.Fatal(err)
						}
					}
					path, err := adapter.Path(g.owner, corestorage.FileDir, "quota-preserved.bin")
					if err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, make([]byte, 2*1048576), 0600); err != nil {
						t.Fatal(err)
					}
					before, files, cached := spaceQuotaPGState(t, g), spaceQuotaFiles(t, g), adapter.QuotaBytes(g.owner)
					locked, written := false, false
					g.s.SetQuotaManager(&spaceQuotaAdapterHook{LocalAdapter: adapter, afterLock: func(tx context.Context, present bool) {
						locked = true
						if present != exists {
							t.Fatal("quota presence changed", present, exists)
						}
						for _, row := range []struct{ query, id string }{{"SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT", g.actor}, {"SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT", g.claims.SessionID}, {"SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT", g.role}, {"SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT", g.role}, {"SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT", g.role}, {"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", g.owner}} {
							assertSpaceAdminPGLock(t, g.pool, row.query, row.id)
						}
						if exists {
							assertSpaceAdminPGLock(t, g.pool, "SELECT user_id FROM user_storage_quotas WHERE user_id=$1 FOR UPDATE NOWAIT", g.owner)
						}
						var acquired bool
						if err := g.pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('storage-quota:'||$1::text,0))", g.owner).Scan(&acquired); err != nil || acquired {
							t.Fatal("actual quota admission lock missing", acquired, err)
						}
					}, after: func(tx context.Context) {
						written = true
						db := transaction.ExecutorFor(tx, g.pool)
						exec := func(sql string, args ...any) {
							t.Helper()
							if _, err := db.Exec(tx, sql, args...); err != nil {
								t.Fatal(err)
							}
						}
						switch failure {
						case "quota_aba":
							exec("UPDATE user_storage_quotas SET quota_bytes=quota_bytes+1 WHERE user_id=$1", g.owner)
							exec("UPDATE user_storage_quotas SET quota_bytes=quota_bytes-1 WHERE user_id=$1", g.owner)
						case "user_email":
							exec("UPDATE users SET email=$2 WHERE id=$1", g.owner, g.owner+"-in-command@example.test")
						case "permission_aba":
							exec("DELETE FROM admin_role_permissions WHERE role_id=$1", g.role)
							exec("INSERT INTO admin_role_permissions(role_id,permission_code,created_by)VALUES($1,'personal_space.space.manage','restore')", g.role)
						}
					}})
					if failure == "platform_command_audits" || failure == "platform_outbox" || failure == "user_storage_quotas" {
						if _, err := g.pool.Exec(ctx, "CREATE FUNCTION space_quota_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required quota write rejected'; END $$"); err != nil {
							t.Fatal(err)
						}
						if _, err := g.pool.Exec(ctx, "CREATE TRIGGER space_quota_reject BEFORE INSERT OR UPDATE ON "+failure+" FOR EACH ROW EXECUTE FUNCTION space_quota_reject()"); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							if _, err := g.pool.Exec(ctx, "DROP TRIGGER space_quota_reject ON "+failure+";DROP FUNCTION space_quota_reject()"); err != nil {
								t.Fatal(err)
							}
						})
					}
					ack, err := g.s.SetStorageQuotaForPrincipal(ctx, g.owner, 1048576, g.proof)
					if !locked || (err == nil) != (failure == "none") || !reflect.DeepEqual(files, spaceQuotaFiles(t, g)) {
						t.Fatal("quota command locks/result/files", ack, err, locked, written)
					}
					if failure != "none" {
						if ack != nil || spaceQuotaPGState(t, g) != before || adapter.QuotaBytes(g.owner) != cached {
							t.Fatal("failed quota command retained data/cache", ack, err)
						}
						if strings.HasPrefix(failure, "platform_") || failure == "user_storage_quotas" {
							var pgErr *pgconn.PgError
							if !errors.As(err, &pgErr) || pgErr.Code != "P0001" {
								t.Fatal("actual SQL rejection not reached", err)
							}
						}
						return
					}
					if !written || ack.OwnerID != g.owner || ack.QuotaBytes != 1048576 || adapter.QuotaBytes(g.owner) != 1048576 {
						t.Fatal(ack, err, written)
					}
					record, present, err := adapter.CurrentQuota(ctx, g.owner)
					if err != nil || !present || record.UpdatedBy != "" || record.UpdatedActorKind != "admin" || record.UpdatedActorID != g.actor || record.FactsVersion < 1 {
						t.Fatal(record, present, err)
					}
					status, err := g.s.AdminStorageStatusForPrincipal(ctx, g.owner, g.proof)
					if err != nil || status.UsedBytes <= status.QuotaBytes || status.AvailableBytes != 0 || status.QuotaBytes != 1048576 {
						t.Fatal("lowered quota deleted bytes or stale read", status, err)
					}
					var actorType, payload string
					if err := g.pool.QueryRow(ctx, "SELECT actor_type FROM platform_command_audits WHERE actor_id=$1 ORDER BY created_at DESC LIMIT 1", g.actor).Scan(&actorType); err != nil || actorType != "admin" {
						t.Fatal(actorType, err)
					}
					if err := g.pool.QueryRow(ctx, "SELECT payload::text FROM platform_outbox WHERE aggregate_id=$1 ORDER BY created_at DESC LIMIT 1", g.owner).Scan(&payload); err != nil {
						t.Fatal(err)
					}
					var fields map[string]json.RawMessage
					if json.Unmarshal([]byte(payload), &fields) != nil || len(fields) != 2 || fields["owner_id"] == nil || fields["quota_bytes"] == nil {
						t.Fatal("unsafe event", payload)
					}
				})
			}
		}
	}
}
func TestSpaceQuotaAdminPostgreSQLActualMetadataReadsCurrentInactiveAndNoWrites(t *testing.T) {
	ctx := context.Background()
	for i, mode := range []string{"default", "override", "inactive", "password"} {
		t.Run(mode, func(t *testing.T) {
			g := spaceAdminPostgres(t, 2560201+i*10, mode != "default", true)
			adapter := g.s.quotaManager.(*corestorage.LocalAdapter)
			if mode == "override" {
				if _, err := corestorage.NewPgQuotaRepository(g.pool).Upsert(ctx, corestorage.QuotaRecord{UserID: g.owner, QuotaBytes: 2097152, UpdatedActorKind: "admin", UpdatedActorID: g.actor}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "inactive" {
				if _, err := g.pool.Exec(ctx, "UPDATE users SET status='suspended' WHERE id=$1", g.owner); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "password" {
				if _, err := g.pool.Exec(ctx, "UPDATE admin_sessions SET authentication_strength='password',mfa_authenticated_at=NULL,version=version+1 WHERE id=$1", g.claims.SessionID); err != nil {
					t.Fatal(err)
				}
			}
			before, files := spaceQuotaPGState(t, g), spaceQuotaFiles(t, g)
			status, err := g.s.AdminStorageStatusForPrincipal(ctx, g.owner, g.proof)
			if err != nil || status.UserID != g.owner || status.UsedBytes <= 0 {
				t.Fatal(status, err)
			}
			want := adapter.DefaultQuotaBytes()
			if mode == "override" {
				want = 2097152
			}
			if status.QuotaBytes != want || status.CustomQuota != (mode == "override") {
				t.Fatal("startup quota cache was used", status)
			}
			summary, err := g.s.AdminSummaryForPrincipal(ctx, g.proof)
			if err != nil || summary == nil || summary.TotalSpaces < 1 {
				t.Fatal(summary, err)
			}
			if spaceQuotaPGState(t, g) != before || !reflect.DeepEqual(files, spaceQuotaFiles(t, g)) {
				t.Fatal("metadata read wrote state")
			}
		})
	}
}

type quotaAdminInventoryPGHook struct {
	*PgRepository
	after func()
}

type quotaAdminCurrentReadPGHook struct {
	*corestorage.LocalAdapter
	after func()
}

func (a *quotaAdminCurrentReadPGHook) CurrentQuota(ctx context.Context, owner string) (corestorage.QuotaRecord, bool, error) {
	record, exists, err := a.LocalAdapter.CurrentQuota(ctx, owner)
	if err == nil && a.after != nil {
		f := a.after
		a.after = nil
		f()
	}
	return record, exists, err
}

func (r *quotaAdminInventoryPGHook) LoadSpaceAdminInventory(ctx context.Context) ([]spaceAdminSummaryRow, error) {
	rows, err := r.PgRepository.LoadSpaceAdminInventory(ctx)
	if err == nil && r.after != nil {
		f := r.after
		r.after = nil
		f()
	}
	return rows, err
}
func TestSpaceQuotaAdminPostgreSQLCommittedQuotaIdentityInventoryAndCurrentAuthorityChangesClose(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, operation := range []string{"quota", "storage", "summary"} {
		for _, mode := range []string{"permission_aba", "admin_session", "user_email", "quota_aba", "space_aba", "guard"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				index++
				g := spaceAdminPostgres(t, 2570201+index*10, true, true)
				qrepo := corestorage.NewPgQuotaRepository(g.pool)
				if _, err := qrepo.Upsert(ctx, corestorage.QuotaRecord{UserID: g.owner, QuotaBytes: 2097152}); err != nil {
					t.Fatal(err)
				}
				mutated := false
				mutate := func() {
					mutated = true
					exec := func(sql string, args ...any) {
						t.Helper()
						if _, err := g.pool.Exec(ctx, sql, args...); err != nil {
							t.Fatal(err)
						}
					}
					switch mode {
					case "permission_aba":
						exec("DELETE FROM admin_role_permissions WHERE role_id=$1", g.role)
						exec("INSERT INTO admin_role_permissions(role_id,permission_code,created_by)VALUES($1,'personal_space.space.manage','restore')", g.role)
					case "admin_session":
						exec("UPDATE admin_sessions SET revoked_at=now(),revoke_reason='test',version=version+1 WHERE id=$1", g.claims.SessionID)
					case "user_email":
						exec("UPDATE users SET email=$2 WHERE id=$1", g.owner, g.owner+"-committed@example.test")
					case "quota_aba":
						exec("UPDATE user_storage_quotas SET quota_bytes=quota_bytes+1 WHERE user_id=$1", g.owner)
						exec("UPDATE user_storage_quotas SET quota_bytes=quota_bytes-1 WHERE user_id=$1", g.owner)
					case "space_aba":
						var title string
						if err := g.pool.QueryRow(ctx, "SELECT title FROM user_spaces WHERE user_id=$1", g.owner).Scan(&title); err != nil {
							t.Fatal(err)
						}
						exec("UPDATE user_spaces SET title='changed' WHERE user_id=$1", g.owner)
						exec("UPDATE user_spaces SET title=$2 WHERE user_id=$1", g.owner, title)
					case "guard":
						if operation == "summary" {
							exec("ALTER TABLE user_spaces DISABLE TRIGGER trg_user_spaces_facts_version")
							t.Cleanup(func() { exec("ALTER TABLE user_spaces ENABLE TRIGGER trg_user_spaces_facts_version") })
						} else {
							exec("ALTER TABLE user_storage_quotas DISABLE TRIGGER trg_user_storage_quotas_facts_version")
							t.Cleanup(func() { exec("ALTER TABLE user_storage_quotas ENABLE TRIGGER trg_user_storage_quotas_facts_version") })
						}
					}
				}
				if operation == "summary" {
					g.s.repo = &quotaAdminInventoryPGHook{PgRepository: g.repo, after: mutate}
				} else {
					g.s.SetQuotaManager(&quotaAdminCurrentReadPGHook{LocalAdapter: g.s.quotaManager.(*corestorage.LocalAdapter), after: mutate})
				}
				var result any
				var err error
				switch operation {
				case "quota":
					result, err = g.s.SetStorageQuotaForPrincipal(ctx, g.owner, 1048576, g.proof)
				case "storage":
					result, err = g.s.AdminStorageStatusForPrincipal(ctx, g.owner, g.proof)
				case "summary":
					result, err = g.s.AdminSummaryForPrincipal(ctx, g.proof)
				}
				// Summary is independent of target profile/quota; quota command does
				// not depend on Space settings. These unrelated changes may proceed.
				allowed := operation == "summary" && (mode == "user_email" || mode == "quota_aba") || operation == "quota" && mode == "space_aba"
				if !mutated || (err == nil) != allowed {
					t.Fatal("current facts binding", mutated, result, err, allowed)
				}
				if !allowed {
					var count int
					if err := g.pool.QueryRow(ctx, "SELECT count(*) FROM platform_command_audits WHERE actor_id=$1", g.actor).Scan(&count); err != nil || count != 0 {
						t.Fatal(count, err)
					}
				}
			})
		}
	}
}
