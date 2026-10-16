package space

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func avatarUploadPG(t *testing.T, owner string, existing bool) (*Service, *PgRepository, *pgxpool.Pool, identityport.SessionPrincipalResolver, string, *corestorage.ObjectService) {
	t.Helper()
	s, repo, pool, proof, claims := spaceOwnerPG(t, owner, existing)
	adapter, err := corestorage.NewLocalAdapterWithQuotaRepository(s.fileStore.cfg.RootDir, 1024*1024, corestorage.NewPgQuotaRepository(pool))
	if err != nil {
		t.Fatal(err)
	}
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewPgObjectRepository(pool), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAvatarStorage(adapter, objects)
	if _, err := corestorage.NewPgQuotaRepository(pool).Upsert(context.Background(), corestorage.QuotaRecord{UserID: owner, QuotaBytes: 1024 * 1024}); err != nil {
		t.Fatal(err)
	}
	if _, err := objects.Usage(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	return s, repo, pool, proof, claims.SessionID, objects
}
func avatarUploadPGState(t *testing.T, pool *pgxpool.Pool, owner string) string {
	t.Helper()
	var ledger string
	if err := pool.QueryRow(context.Background(), `SELECT jsonb_build_object('account',(SELECT to_jsonb(a) FROM user_storage_accounts a WHERE user_id=$1),'quota',(SELECT to_jsonb(q) FROM user_storage_quotas q WHERE user_id=$1))::text`, owner).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	return spaceOwnerPGState(t, pool, owner) + ledger
}
func TestSpaceAvatarUploadPostgreSQLActualQuotaLocksAndRequiredSQLRollback(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, existing := range []bool{false, true} {
		for _, failure := range []string{"none", "platform_command_audits", "platform_outbox", "user_storage_accounts"} {
			t.Run(fmt.Sprint(existing)+"/"+failure, func(t *testing.T) {
				index++
				owner := fmt.Sprint(234201 + index*10)
				s, repo, pool, proof, session, objects := avatarUploadPG(t, owner, existing)
				before, files := avatarUploadPGState(t, pool, owner), avatarUploadFileBytes(t, s, owner)
				usage, err := objects.Usage(ctx, owner)
				if err != nil {
					t.Fatal(err)
				}
				ran := false
				s.repo = &spaceOwnerPGHook{PgRepository: repo, after: func(tx context.Context, space *Space) {
					ran = true
					for _, row := range []struct{ query, id string }{{"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", owner}, {"SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT", session}, {"SELECT user_id FROM user_storage_accounts WHERE user_id=$1 FOR UPDATE NOWAIT", owner}, {"SELECT user_id FROM user_storage_quotas WHERE user_id=$1 FOR UPDATE NOWAIT", owner}} {
						_, err := pool.Exec(ctx, row.query, row.id)
						var p *pgconn.PgError
						if !errors.As(err, &p) || p.Code != "55P03" {
							t.Fatal("actual write lock missing", row, err)
						}
					}
					var acquired bool
					if err := pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('personal-space:' || $1::text,0))", owner).Scan(&acquired); err != nil || acquired {
						t.Fatal("owner admission missing", err)
					}
					if existing {
						_, err := pool.Exec(ctx, "SELECT id FROM user_spaces WHERE user_id=$1 FOR UPDATE NOWAIT", owner)
						var p *pgconn.PgError
						if !errors.As(err, &p) || p.Code != "55P03" {
							t.Fatal("Space row lock missing", err)
						}
					}
					if len(avatarUploadFileBytes(t, s, owner)) != len(files)+1 {
						t.Fatal("new bytes not staged")
					}
				}}
				if failure != "none" {
					action := "INSERT"
					if failure == "user_storage_accounts" {
						action = "UPDATE"
					}
					if _, err := pool.Exec(ctx, "CREATE FUNCTION avatar_upload_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required avatar upload rejected'; END $$; CREATE TRIGGER avatar_upload_reject BEFORE "+action+" ON "+failure+" FOR EACH ROW EXECUTE FUNCTION avatar_upload_reject()"); err != nil {
						t.Fatal(err)
					}
					defer func() {
						if _, err := pool.Exec(context.Background(), "DROP TRIGGER avatar_upload_reject ON "+failure+"; DROP FUNCTION avatar_upload_reject()"); err != nil {
							t.Fatal(err)
						}
					}()
				}
				ack, err := s.UploadAvatarForPrincipal(ctx, bytes.NewReader(tinyPNG), proof)
				if (err == nil) != (failure == "none") || ran != (failure != "user_storage_accounts") {
					t.Fatal("actual upload path not reached", ack, err, ran)
				}
				current := avatarUploadFileBytes(t, s, owner)
				if failure != "none" {
					var p *pgconn.PgError
					if !errors.As(err, &p) || p.Code != "P0001" || ack != nil || before != avatarUploadPGState(t, pool, owner) || !reflect.DeepEqual(files, current) {
						t.Fatal("actual SQL failure not fully compensated", ack, err)
					}
				} else {
					if ack == nil || ack.OwnerID != owner || ack.CleanupPending || len(current) != len(files)+1 {
						t.Fatal(ack, len(files), len(current))
					}
					after, e := objects.Usage(ctx, owner)
					if e != nil {
						t.Fatal(e)
					}
					var size int64
					for name, data := range current {
						if old, ok := files[name]; ok {
							if !bytes.Equal(old, data) {
								t.Fatal("old avatar lost")
							}
						} else {
							size += int64(len(data))
						}
					}
					if after.UsedBytes-usage.UsedBytes != size {
						t.Fatal("new bytes not fully accounted", usage, after, size)
					}
				}
			})
		}
	}
}
func TestSpaceAvatarUploadPostgreSQLHeldProofAndActualExtraWritesClose(t *testing.T) {
	ctx := context.Background()
	for index, mode := range []string{"quota_lower", "space_aba", "session", "extra_space", "extra_session", "cancel", "replacement_pending"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(235201 + index*10)
			s, repo, pool, proof, session, objects := avatarUploadPG(t, owner, true)
			before, files := avatarUploadPGState(t, pool, owner), avatarUploadFileBytes(t, s, owner)
			usage, err := objects.Usage(ctx, owner)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			ran := false
			reader := &avatarUploadReader{Reader: bytes.NewReader(tinyPNG)}
			if mode == "quota_lower" || mode == "space_aba" || mode == "session" {
				reader.after = func() {
					ran = true
					switch mode {
					case "quota_lower":
						_, err = corestorage.NewPgQuotaRepository(pool).Upsert(ctx, corestorage.QuotaRecord{UserID: owner, QuotaBytes: 1})
					case "session":
						_, err = pool.Exec(ctx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='avatar test' WHERE id=$1", session)
					case "space_aba":
						var old string
						err = pool.QueryRow(ctx, "SELECT bio FROM user_spaces WHERE user_id=$1", owner).Scan(&old)
						if err == nil {
							_, err = pool.Exec(ctx, "UPDATE user_spaces SET bio='ABA' WHERE user_id=$1", owner)
						}
						if err == nil {
							_, err = pool.Exec(ctx, "UPDATE user_spaces SET bio=$2 WHERE user_id=$1", owner, old)
						}
					}
					if err != nil {
						t.Fatal("committed mutation failed", err)
					}
				}
			} else {
				s.repo = &spaceOwnerPGHook{PgRepository: repo, after: func(tx context.Context, space *Space) {
					ran = true
					switch mode {
					case "extra_space":
						_, err = transaction.ExecutorFor(tx, pool).Exec(tx, "UPDATE user_spaces SET cover_image='extra' WHERE user_id=$1", owner)
					case "extra_session":
						_, err = transaction.ExecutorFor(tx, pool).Exec(tx, "UPDATE sessions SET revoked_at=NOW(),revoke_reason='avatar test' WHERE id=$1", session)
					case "cancel":
						cancel()
					case "replacement_pending":
						current, e := s.fileStore.avatarFiles(owner)
						if e != nil {
							t.Fatal(e)
						}
						found := false
						for _, file := range current {
							if _, ok := files[file.name]; !ok {
								mutateSelectedAvatar(t, s, owner, file.name, "inode")
								found = true
							}
						}
						if !found {
							t.Fatal("staged bytes absent")
						}
					}
					if err != nil {
						t.Fatal("actual SQL mutation failed", err)
					}
				}}
			}
			ack, err := s.UploadAvatarForPrincipal(ctx, reader, proof)
			if !ran || err == nil || ack != nil {
				t.Fatal("changed upload accepted", ack, err, ran)
			}
			current := avatarUploadFileBytes(t, s, owner)
			for name, data := range files {
				if !bytes.Equal(data, current[name]) {
					t.Fatal("old avatar deleted")
				}
			}
			if mode == "replacement_pending" {
				if !errors.Is(err, corestorage.ErrInlineImageCleanupPending) || len(current) != len(files)+1 {
					t.Fatal("changed file wrongly deleted", err)
				}
			} else if !reflect.DeepEqual(files, current) {
				t.Fatal("exact failure cleanup missing")
			}
			if mode == "extra_space" || mode == "extra_session" || mode == "cancel" || mode == "replacement_pending" {
				if before != avatarUploadPGState(t, pool, owner) {
					t.Fatal("actual metadata/ledger did not roll back")
				}
			}
			after, e := objects.Usage(context.Background(), owner)
			if e != nil || usage.UsedBytes != after.UsedBytes {
				t.Fatal(usage, after, e)
			}
		})
	}
}
