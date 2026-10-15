package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"reflect"
	"sync"
	"testing"
	"time"
)

type objectDeleteFinalizeFailure struct {
	ObjectRepository
	fail bool
}

func (r *objectDeleteFinalizeFailure) FinalizeDelete(ctx context.Context, receipt storedObject) error {
	if r.fail {
		return errors.New("injected actual delete ledger failure")
	}
	return r.ObjectRepository.FinalizeDelete(ctx, receipt)
}
func TestObjectDeleteReceiptReportsPendingAndRetainsExactTombstone(t *testing.T) {
	ctx := context.Background()
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	base := NewMemoryObjectRepository()
	repo := &objectDeleteFinalizeFailure{ObjectRepository: base, fail: true}
	s, err := NewObjectService(adapter, adapter, repo, 100)
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.Put(ctx, "19101", PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "source.pdf", MimeType: "application/pdf", SizeHint: 3, Reader: bytes.NewBufferString("abc")})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Delete(ctx, o.OwnerID, o.ID, o.Version)
	base.mu.Lock()
	pending := base.objects[o.ID]
	base.mu.Unlock()
	account, exists, accountErr := base.Account(ctx, o.OwnerID)
	usage, usageErr := adapter.Usage(o.OwnerID)
	if pending.ID != o.ID || pending.OwnerID != o.OwnerID || pending.Status != ObjectStatusDeleting || pending.Version != o.Version+1 || pending.SHA256 != o.SHA256 || pending.SizeBytes != o.SizeBytes || !exists || accountErr != nil || account.UsedBytes != 3 || usageErr != nil || usage != 0 {
		t.Fatal("delete did not retain actual receipt/capacity/byte state", pending, account, usage, accountErr, usageErr)
	}
	if !errors.Is(err, ErrObjectCompensationPending) {
		t.Fatal("irreversible byte delete ledger failure was not reported pending", err, pending.Status, account.UsedBytes, usage)
	}
	receipt, err := s.GetOwnedObjectLifecycle(ctx, o.OwnerID, o.ID)
	if err != nil || receipt.Object.Status != ObjectStatusDeleting {
		t.Fatal(receipt, err)
	}
	repo.fail = false
	if err := s.DeleteOwnedObjectForCommand(ctx, receipt); err != nil {
		t.Fatal("pending delete cannot be retried", err)
	}
	terminal, err := s.GetOwnedObjectLifecycle(ctx, o.OwnerID, o.ID)
	if err != nil || terminal.Object.Status != ObjectStatusDeleted || terminal.Object.Version != o.Version+2 {
		t.Fatal(terminal, err)
	}
	account, _, err = base.Account(ctx, o.OwnerID)
	if err != nil || account.UsedBytes != 0 {
		t.Fatal(account, err)
	}
	if err := s.DeleteOwnedObjectForCommand(ctx, terminal); err != nil {
		t.Fatal("terminal retry not idempotent", err)
	}
	again, err := s.GetOwnedObjectLifecycle(ctx, o.OwnerID, o.ID)
	if err != nil || !reflect.DeepEqual(terminal.Object, again.Object) {
		t.Fatal("terminal retry changed receipt", again, err)
	}
}

func deleteReceiptFixture(t *testing.T) (*ObjectService, *LocalAdapter, *MemoryObjectRepository, Object) {
	t.Helper()
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewMemoryObjectRepository()
	s, err := NewObjectService(adapter, adapter, repo, 100)
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.Put(context.Background(), "19102", PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "original.pdf", MimeType: "application/pdf", SizeHint: 3, Reader: bytes.NewBufferString("abc")})
	if err != nil {
		t.Fatal(err)
	}
	return s, adapter, repo, o
}
func TestObjectDeleteReceiptWholePrivateBindingAndNoAliasing(t *testing.T) {
	for _, mode := range []string{"current", "public_alias", "public_metadata", "private_key", "forged_binding", "canceled", "outer_transaction"} {
		t.Run(mode, func(t *testing.T) {
			s, adapter, repo, o := deleteReceiptFixture(t)
			ctx := context.Background()
			receipt, err := s.GetOwnedObjectLifecycle(ctx, o.OwnerID, o.ID)
			if err != nil {
				t.Fatal(err)
			}
			if raw, err := json.Marshal(receipt); err != nil || string(raw) != "{}" {
				t.Fatal("private receipt leaked", string(raw), err)
			}
			if _, err := s.GetOwnedObjectLifecycle(ctx, "other", o.ID); !errors.Is(err, ErrObjectNotFound) {
				t.Fatal(err)
			}
			switch mode {
			case "public_alias":
				receipt.Object.OriginalName = "forged.pdf"
			case "public_metadata", "private_key":
				repo.mu.Lock()
				row := repo.objects[o.ID]
				if mode == "public_metadata" {
					row.MimeType = "changed"
				} else {
					row.storageKey = "another-key"
				}
				repo.objects[o.ID] = row
				repo.mu.Unlock()
			case "forged_binding":
				receipt.binding = nil
			case "canceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if mode == "outer_transaction" {
				err = transaction.NewMemory().Within(ctx, func(tx context.Context) error { return s.DeleteOwnedObjectForCommand(tx, receipt) })
			} else {
				err = s.DeleteOwnedObjectForCommand(ctx, receipt)
			}
			if (err == nil) != (mode == "current") {
				t.Fatal(mode, err)
			}
			usage, e := adapter.Usage(o.OwnerID)
			want := int64(3)
			if mode == "current" {
				want = 0
			}
			if e != nil || usage != want {
				t.Fatal("receipt rejection removed bytes", mode, usage, e)
			}
		})
	}
}

type objectDeleteCancelAfterPrepare struct {
	ObjectRepository
	cancel func()
	valid  bool
}

func (r *objectDeleteCancelAfterPrepare) PrepareDelete(ctx context.Context, owner, id string, version int64) (storedObject, error) {
	x, err := r.ObjectRepository.PrepareDelete(ctx, owner, id, version)
	if err == nil {
		r.cancel()
	}
	return x, err
}
func (r *objectDeleteCancelAfterPrepare) FinalizeDelete(ctx context.Context, x storedObject) error {
	deadline, ok := ctx.Deadline()
	r.valid = ctx.Err() == nil && ok && time.Until(deadline) <= 5*time.Second
	return r.ObjectRepository.FinalizeDelete(ctx, x)
}
func TestObjectDeleteReceiptCanceledAfterPreparationCompletesBoundedLedger(t *testing.T) {
	s, adapter, base, o := deleteReceiptFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &objectDeleteCancelAfterPrepare{ObjectRepository: base, cancel: cancel}
	s.repository = repo
	if err := s.Delete(ctx, o.OwnerID, o.ID, o.Version); err != nil || !repo.valid {
		t.Fatal(err, repo.valid)
	}
	x, err := s.GetOwnedObjectLifecycle(context.Background(), o.OwnerID, o.ID)
	if err != nil || x.Object.Status != ObjectStatusDeleted {
		t.Fatal(x, err)
	}
	account, _, err := base.Account(context.Background(), o.OwnerID)
	if err != nil || account.UsedBytes != 0 {
		t.Fatal(account, err)
	}
	if usage, err := adapter.Usage(o.OwnerID); err != nil || usage != 0 {
		t.Fatal(usage, err)
	}
}
func TestObjectDeleteReceiptTerminalCannotBeRestoredAndConcurrentRetry(t *testing.T) {
	s, adapter, base, o := deleteReceiptFixture(t)
	ctx := context.Background()
	pending, err := base.PrepareDelete(ctx, o.OwnerID, o.ID, o.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := base.FinalizeDelete(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := base.RestoreReady(ctx, pending); err == nil {
		t.Fatal("terminal object resurrected")
	}
	s, adapter, base, o = deleteReceiptFixture(t)
	receipt, err := s.GetOwnedObjectLifecycle(ctx, o.OwnerID, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errorsCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errorsCh <- s.DeleteOwnedObjectForCommand(ctx, receipt) }()
	}
	wg.Wait()
	close(errorsCh)
	success := 0
	for err := range errorsCh {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("ready receipt had multiple successful deletions", success)
	}
	account, _, err := base.Account(ctx, o.OwnerID)
	if err != nil || account.UsedBytes != 0 {
		t.Fatal(account, err)
	}
	if usage, err := adapter.Usage(o.OwnerID); err != nil || usage != 0 {
		t.Fatal(usage, err)
	}
}
