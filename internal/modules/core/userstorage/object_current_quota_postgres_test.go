package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func objectQuotaPostgres(t *testing.T, owner string) (*ObjectService, *LocalAdapter, *PgQuotaRepository, *PgObjectRepository, *pgxpool.Pool) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil || config.ConnConfig.Database != "campusos_v12_02b_author_write" {
		t.Fatal("Object quota test requires the exact owned author-write database", err)
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES($1,$2,'Quota owner',$3,'active',1)", owner, "quota-"+owner, "quota-"+owner+"@example.test"); err != nil {
		t.Fatal(err)
	}
	quotas := NewPgQuotaRepository(pool)
	if _, err := quotas.Upsert(ctx, QuotaRecord{UserID: owner, QuotaBytes: 100}); err != nil {
		t.Fatal(err)
	}
	adapter, err := NewLocalAdapterWithQuotaRepository(t.TempDir(), 50, quotas)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewPgObjectRepository(pool)
	objects, err := NewObjectService(adapter, adapter, repo, 100)
	if err != nil {
		t.Fatal(err)
	}
	return objects, adapter, quotas, repo, pool
}

type currentQuotaLockProbe struct {
	*PgQuotaRepository
	after func(context.Context, string, bool)
}

func (r *currentQuotaLockProbe) GetOwnerQuotaForObjectCommand(ctx context.Context, owner string) (QuotaRecord, bool, func(), error) {
	record, exists, release, err := r.PgQuotaRepository.GetOwnerQuotaForObjectCommand(ctx, owner)
	if err == nil && r.after != nil {
		r.after(ctx, owner, exists)
	}
	return record, exists, release, err
}

func TestObjectCurrentQuotaPostgreSQLCurrentSourceAndLocks(t *testing.T) {
	ctx := context.Background()
	for i, mode := range []string{"lower", "raise", "missing_default"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(159201 + i)
			objects, adapter, quotas, _, pool := objectQuotaPostgres(t, owner)
			switch mode {
			case "lower":
				_, err := quotas.Upsert(ctx, QuotaRecord{UserID: owner, QuotaBytes: 10})
				if err != nil {
					t.Fatal(err)
				}
			case "raise":
				_, err := quotas.Upsert(ctx, QuotaRecord{UserID: owner, QuotaBytes: 120})
				if err != nil {
					t.Fatal(err)
				}
			case "missing_default":
				if _, err := pool.Exec(ctx, "DELETE FROM user_storage_quotas WHERE user_id=$1", owner); err != nil {
					t.Fatal(err)
				}
			}
			if adapter.QuotaBytes(owner) != 100 {
				t.Fatal("fixture did not retain startup cache")
			}
			calls := 0
			adapter.quotaRepository = &currentQuotaLockProbe{PgQuotaRepository: quotas, after: func(txCtx context.Context, got string, exists bool) {
				calls++
				if _, ok := transaction.FromContext(txCtx); !ok || got != owner {
					t.Fatal("current quota did not use the actual command transaction")
				}
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				var acquired bool
				err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('storage-quota:' || $1::text,0))", owner).Scan(&acquired)
				_ = tx.Rollback(ctx)
				if err != nil || acquired {
					t.Fatal("owner admission lock missing, including empty override", acquired, err)
				}
				if exists {
					tx, err = pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					_, err = tx.Exec(ctx, "SELECT user_id FROM user_storage_quotas WHERE user_id=$1 FOR UPDATE NOWAIT", owner)
					_ = tx.Rollback(ctx)
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
						t.Fatal("actual quota row was not held FOR SHARE", err)
					}
				}
				blockCtx, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
				defer cancel()
				if _, err := quotas.Upsert(blockCtx, QuotaRecord{UserID: owner, QuotaBytes: 1}); err == nil {
					t.Fatal("supported quota updater bypassed owner admission")
				}
			}}
			size := int64(20)
			if mode == "raise" {
				size = 100
			}
			if mode == "missing_default" {
				size = 60 // Cache would admit; the configured current default must deny.
			}
			object, err := objects.Put(ctx, owner, PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "quota.txt", MimeType: "text/plain", SizeHint: size, Reader: bytes.NewReader(make([]byte, size))})
			if mode == "raise" {
				if err != nil || object.ID == "" || calls != 2 {
					t.Fatal("current raised quota was not checked at both reserve and commit", object, err, calls)
				}
			} else if !errors.Is(err, ErrObjectQuota) || object.ID != "" || calls != 1 {
				t.Fatal("actual current quota did not close admission", object, err, calls)
			}
			// Ordinary current reads and supported writes must join an outer transaction.
			rollback := errors.New("quota rollback")
			err = transaction.NewPostgreSQL(pool).Within(ctx, func(txCtx context.Context) error {
				if _, err := quotas.Upsert(txCtx, QuotaRecord{UserID: owner, QuotaBytes: 15}); err != nil {
					return err
				}
				record, exists, err := quotas.GetOwnerQuota(txCtx, owner)
				if err != nil || !exists || record.QuotaBytes != 15 {
					t.Fatal("quota read escaped outer transaction", record, exists, err)
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal(err)
			}
			record, exists, err := quotas.GetOwnerQuota(ctx, owner)
			if err != nil || (mode == "missing_default" && exists) || (exists && record.QuotaBytes == 15) {
				t.Fatal("quota write escaped rollback", record, exists, err)
			}
		})
	}
}

func TestObjectCurrentQuotaPostgreSQLIOFailureReleasesReceipt(t *testing.T) {
	for i, mode := range []string{"lower", "unavailable", "sql_commit_failure", "cancel", "other_object_capacity"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(159301 + i)
			objects, adapter, quotas, repo, pool := objectQuotaPostgres(t, owner)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "sql_commit_failure" {
				query := fmt.Sprintf(`CREATE FUNCTION fail_object_quota_commit_%s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.owner_user_id=%s AND NEW.status='ready' THEN RAISE EXCEPTION 'test object commit rejected'; END IF; RETURN NEW; END; $$;
CREATE TRIGGER fail_object_quota_commit_%s BEFORE UPDATE ON storage_objects FOR EACH ROW EXECUTE FUNCTION fail_object_quota_commit_%s();`, owner, owner, owner, owner)
				if _, err := pool.Exec(ctx, query); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_, err := pool.Exec(context.Background(), fmt.Sprintf("DROP TRIGGER fail_object_quota_commit_%s ON storage_objects; DROP FUNCTION fail_object_quota_commit_%s();", owner, owner))
					if err != nil {
						t.Error(err)
					}
				})
			}
			ran := false
			var other Object
			reader := &currentQuotaChangeReader{reader: bytes.NewReader(make([]byte, 20)), before: func() {
				ran = true
				switch mode {
				case "lower":
					if _, err := quotas.Upsert(ctx, QuotaRecord{UserID: owner, QuotaBytes: 10}); err != nil {
						t.Fatal(err)
					}
				case "unavailable":
					adapter.quotaRepository = nil
				case "cancel":
					cancel()
				case "other_object_capacity":
					var err error
					other, err = objects.Put(ctx, owner, PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "other.txt", MimeType: "text/plain", SizeHint: 20, Reader: bytes.NewReader(make([]byte, 20))})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := quotas.Upsert(ctx, QuotaRecord{UserID: owner, QuotaBytes: 20}); err != nil {
						t.Fatal(err)
					}
				}
			}}
			object, err := objects.Put(ctx, owner, PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "quota.txt", MimeType: "text/plain", SizeHint: 20, Reader: reader})
			if err == nil || object.ID != "" || !ran {
				t.Fatal("I/O failure did not run and close", object, err, ran)
			}
			if (mode == "lower" || mode == "other_object_capacity") && !errors.Is(err, ErrObjectQuota) {
				t.Fatal("changed current quota was not the rejection", err)
			}
			if mode == "unavailable" && !errors.Is(err, ErrObjectUnavailable) {
				t.Fatal("missing current source did not fail closed", err)
			}
			ctx = context.Background()
			wantUsed := int64(0)
			if mode == "other_object_capacity" {
				wantUsed = 20
			}
			account, exists, err := repo.Account(ctx, owner)
			if err != nil || !exists || account.UsedBytes != wantUsed || account.ReservedBytes != 0 {
				t.Fatal("original failed receipt left capacity reserved", account, exists, err)
			}
			usage, err := adapter.Usage(owner)
			if err != nil || usage != wantUsed {
				t.Fatal("failed receipt left provider bytes or deleted another object", usage, err)
			}
			var pending, deleted, released int
			if err := pool.QueryRow(ctx, "SELECT count(*) FILTER(WHERE status='pending'),count(*) FILTER(WHERE status='deleted') FROM storage_objects WHERE owner_user_id=$1", owner).Scan(&pending, &deleted); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_storage_reservations WHERE user_id=$1 AND status='released'", owner).Scan(&released); err != nil || pending != 0 || deleted != 1 || released != 1 {
				t.Fatal("original reservation/object receipt was not released exactly", pending, deleted, released, err)
			}
			if other.ID != "" {
				if got, err := objects.Stat(ctx, owner, other.ID); err != nil || got.ID != other.ID || got.Status != ObjectStatusReady {
					t.Fatal("failure changed the unrelated committed object", got, err)
				}
			}
		})
	}
}
