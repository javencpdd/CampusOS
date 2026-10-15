package storage

import (
	"context"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"testing"
)

func TestSpaceAvatarRetentionAccountingActivePreparedAndDeltaOnlyUndo(t *testing.T) {
	ctx := context.Background()
	adapter, err := NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := NewMemoryObjectRepository()
	objects, err := NewObjectService(adapter, adapter, repo, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if objects.PrepareAvatarRetentionAccounting(ctx, "1001") == nil {
		t.Fatal("prepare outside command accepted")
	}
	if _, err := objects.ReleaseAvatarRetentionBytes(ctx, "1001", 4); err == nil {
		t.Fatal("release outside command accepted")
	}
	manager := transaction.NewMemory()
	err = manager.Within(ctx, func(tx context.Context) error {
		if _, err := objects.ReleaseAvatarRetentionBytes(tx, "1001", 4); err == nil {
			t.Fatal("unprepared missing ledger rebootstrap")
		}
		if err := objects.PrepareAvatarRetentionAccounting(tx, "1001"); err != nil {
			return err
		}
		if _, err := objects.AccountInlineImageForCommand(tx, "1001", 10); err != nil {
			return err
		}
		undo, err := objects.ReleaseAvatarRetentionBytes(tx, "1001", 4)
		if err != nil {
			return err
		}
		usage, err := objects.Usage(tx, "1001")
		if err != nil || usage.UsedBytes != 6 {
			t.Fatal(usage, err)
		}
		if err := undo(); err != nil {
			return err
		}
		if err := undo(); err != nil {
			return err
		}
		usage, err = objects.Usage(tx, "1001")
		if err != nil || usage.UsedBytes != 10 {
			t.Fatal("delta undo repeated", usage, err)
		}
		if _, err := objects.ReleaseAvatarRetentionBytes(tx, "1001", 11); err == nil {
			t.Fatal("underflow accepted")
		}
		cancelled, cancel := context.WithCancel(tx)
		cancel()
		if _, err := objects.ReleaseAvatarRetentionBytes(cancelled, "1001", 1); err == nil {
			t.Fatal("cancelled release accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
