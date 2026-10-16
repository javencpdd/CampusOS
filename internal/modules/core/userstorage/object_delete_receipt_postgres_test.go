package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
	"reflect"
	"testing"
)

func TestObjectDeleteReceiptPostgreSQLPendingExactRetryAndCapacity(t *testing.T) {
	ctx := context.Background()
	s, adapter, _, repo, pool := objectQuotaPostgres(t, "169201")
	put := func(name string) Object {
		o, err := s.Put(ctx, "169201", PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: name, MimeType: "application/pdf", SizeHint: 3, Reader: bytes.NewBufferString("abc")})
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	o, other := put("target.pdf"), put("other.pdf")
	receipt, err := s.GetOwnedObjectLifecycle(ctx, o.OwnerID, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "CREATE FUNCTION object_delete_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='deleted' THEN RAISE EXCEPTION 'actual delete ledger commit rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER object_delete_reject BEFORE UPDATE ON storage_objects FOR EACH ROW EXECUTE FUNCTION object_delete_reject()"); err != nil {
		t.Fatal(err)
	}
	dropped := false
	defer func() {
		if !dropped {
			if _, err := pool.Exec(context.Background(), "DROP TRIGGER object_delete_reject ON storage_objects; DROP FUNCTION object_delete_reject()"); err != nil {
				t.Error(err)
			}
		}
	}()
	err = s.DeleteOwnedObjectForCommand(ctx, receipt)
	var pe *pgconn.PgError
	if !errors.Is(err, ErrObjectCompensationPending) || !errors.As(err, &pe) || pe.Code != "P0001" {
		t.Fatal("actual pending SQL failure not reached", err)
	}
	pending, err := s.GetOwnedObjectLifecycle(ctx, o.OwnerID, o.ID)
	if err != nil || pending.Object.Status != ObjectStatusDeleting || pending.Object.Version != o.Version+1 || pending.Object.SHA256 != o.SHA256 || pending.Object.SizeBytes != o.SizeBytes {
		t.Fatal(pending, err)
	}
	account, _, err := repo.Account(ctx, o.OwnerID)
	if err != nil || account.UsedBytes != 6 {
		t.Fatal("failed finalization partially released ledger", account, err)
	}
	if usage, err := adapter.Usage(o.OwnerID); err != nil || usage != 3 {
		t.Fatal("failed receipt removed other object", usage, err)
	}
	if value, err := s.Stat(ctx, other.OwnerID, other.ID); err != nil || !reflect.DeepEqual(value, other) {
		t.Fatal("other metadata changed", value, err)
	}
	if _, err := pool.Exec(ctx, "DROP TRIGGER object_delete_reject ON storage_objects; DROP FUNCTION object_delete_reject()"); err != nil {
		t.Fatal(err)
	}
	dropped = true
	if err := s.DeleteOwnedObjectForCommand(ctx, pending); err != nil {
		t.Fatal("pending receipt retry failed", err)
	}
	terminal, err := s.GetOwnedObjectLifecycle(ctx, o.OwnerID, o.ID)
	if err != nil || terminal.Object.Status != ObjectStatusDeleted || terminal.Object.Version != o.Version+2 {
		t.Fatal(terminal, err)
	}
	account, _, err = repo.Account(ctx, o.OwnerID)
	if err != nil || account.UsedBytes != 3 {
		t.Fatal(account, err)
	}
	if err := s.DeleteOwnedObjectForCommand(ctx, terminal); err != nil {
		t.Fatal(err)
	}
	again, err := s.GetOwnedObjectLifecycle(ctx, o.OwnerID, o.ID)
	if err != nil || !reflect.DeepEqual(terminal.Object, again.Object) {
		t.Fatal("terminal retry changed receipt", again, err)
	}
	if err := repo.RestoreReady(ctx, *pending.binding); err == nil {
		t.Fatal("terminal object resurrected")
	}
}
func TestObjectDeleteReceiptPostgreSQLWholePrivateProofAndActualLocks(t *testing.T) {
	ctx := context.Background()
	for i, mode := range []string{"current", "metadata", "private_key", "public_alias", "outer_tx", "changed_after_prepare", "restore_fail"} {
		t.Run(mode, func(t *testing.T) {
			owner := fmt.Sprint(169301 + i*10)
			s, adapter, _, repo, pool := objectQuotaPostgres(t, owner)
			o, err := s.Put(ctx, owner, PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "target.pdf", MimeType: "application/pdf", SizeHint: 3, Reader: bytes.NewBufferString("abc")})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := s.GetOwnedObjectLifecycle(ctx, owner, o.ID)
			if err != nil {
				t.Fatal(err)
			}
			if raw, err := json.Marshal(receipt); err != nil || string(raw) != "{}" {
				t.Fatal("private SQL receipt exposed", string(raw), err)
			}
			switch mode {
			case "metadata":
				if _, err := pool.Exec(ctx, "UPDATE storage_objects SET namespace='changed' WHERE id=$1", o.ID); err != nil {
					t.Fatal(err)
				}
			case "private_key":
				if _, err := pool.Exec(ctx, "UPDATE storage_objects SET storage_key='changed-key' WHERE id=$1", o.ID); err != nil {
					t.Fatal(err)
				}
			case "public_alias":
				receipt.Object.SHA256 = "forged"
			case "restore_fail":
				if _, err := pool.Exec(ctx, "UPDATE storage_objects SET storage_key='unsafe/key' WHERE id=$1", o.ID); err != nil {
					t.Fatal(err)
				}
				receipt, err = s.GetOwnedObjectLifecycle(ctx, owner, o.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, "CREATE FUNCTION object_restore_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.status='deleting' AND NEW.status='ready' THEN RAISE EXCEPTION 'actual restore rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER object_restore_reject BEFORE UPDATE ON storage_objects FOR EACH ROW EXECUTE FUNCTION object_restore_reject()"); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := pool.Exec(context.Background(), "DROP TRIGGER object_restore_reject ON storage_objects; DROP FUNCTION object_restore_reject()"); err != nil {
						t.Error(err)
					}
				}()
			}
			if mode == "changed_after_prepare" {
				ran := false
				s.repository = &objectDeleteAfterPrepare{ObjectRepository: repo, after: func(x storedObject) {
					ran = true
					if _, err := pool.Exec(ctx, "UPDATE storage_objects SET purpose='changed after admission' WHERE id=$1", x.ID); err != nil {
						t.Fatal(err)
					}
				}}
				err = s.DeleteOwnedObjectForCommand(ctx, receipt)
				if !ran || !errors.Is(err, ErrObjectCompensationPending) {
					t.Fatal("after-admission receipt change was not pending", err, ran)
				}
			} else if mode == "outer_tx" {
				err = transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error { return s.DeleteOwnedObjectForCommand(tx, receipt) })
			} else {
				err = s.DeleteOwnedObjectForCommand(ctx, receipt)
			}
			if (err == nil) != (mode == "current") {
				t.Fatal(mode, err)
			}
			if mode == "restore_fail" && !errors.Is(err, ErrObjectCompensationPending) {
				t.Fatal("failed restore hidden", err)
			}
			want := int64(3)
			if mode == "current" || mode == "changed_after_prepare" {
				want = 0
			}
			if usage, e := adapter.Usage(owner); e != nil || usage != want {
				t.Fatal("wrong provider receipt removed", mode, usage, e)
			}
			if mode == "current" {
				// Exercise the repository's actual row lock during preparation/finalize.
				other, err := s.Put(ctx, owner, PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "locked.pdf", SizeHint: 3, Reader: bytes.NewBufferString("def")})
				if err != nil {
					t.Fatal(err)
				}
				if err := transaction.NewPostgreSQL(pool).Within(ctx, func(tx context.Context) error {
					pending, err := repo.PrepareDelete(tx, owner, other.ID, other.Version)
					if err != nil {
						return err
					}
					_, err = pool.Exec(ctx, "SELECT id FROM storage_objects WHERE id=$1 FOR UPDATE NOWAIT", other.ID)
					var pe *pgconn.PgError
					if !errors.As(err, &pe) || pe.Code != "55P03" {
						t.Fatal("actual object lock missing", err)
					}
					return repo.RestoreReady(tx, pending)
				}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

type objectDeleteAfterPrepare struct {
	ObjectRepository
	after func(storedObject)
}

func (r *objectDeleteAfterPrepare) PrepareDelete(ctx context.Context, owner, id string, version int64) (storedObject, error) {
	x, err := r.ObjectRepository.PrepareDelete(ctx, owner, id, version)
	if err == nil {
		r.after(x)
	}
	return x, err
}
