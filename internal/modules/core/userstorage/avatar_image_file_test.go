package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"strings"
	"testing"
)

func TestSpaceAvatarUploadCoreFixedReceiptNamespaceAndExactCleanup(t *testing.T) {
	ctx := context.Background()
	adapter, err := NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := PrepareInlineImage(ctx, bytes.NewReader(testPNG(t)), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if file.WriteAvatarForCommand(ctx, adapter, "1001", "/spaces/files") == nil {
		t.Fatal("avatar write outside SDK accepted")
	}
	err = transaction.NewMemory().Within(ctx, func(tx context.Context) error {
		if file.Write(tx, adapter, "1001", "avatars", "/spaces/files") == nil {
			t.Fatal("generic namespace accepted avatars")
		}
		return file.WriteAvatarForCommand(tx, adapter, "1001", "/spaces/files")
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata := file.Metadata()
	if !strings.HasPrefix(metadata.FileURL, "/spaces/files/1001/avatars/") || metadata.FileSize < 1 {
		t.Fatal(metadata)
	}
	metadata.FileName = "changed"
	if file.Check(ctx) != nil {
		t.Fatal("metadata alias changed private receipt")
	}
	raw, err := json.Marshal(file)
	if err != nil || string(raw) != "{}" {
		t.Fatal("private receipt serialized", string(raw), err)
	}
	if err := file.Discard(); err != nil {
		t.Fatal(err)
	}
	if err := file.Discard(); err != nil {
		t.Fatal("cleanup not idempotent", err)
	}
	if err := file.Check(ctx); err == nil || errors.Is(err, ErrInlineImageCleanupPending) {
		t.Fatal("removed file accepted", err)
	}
}
