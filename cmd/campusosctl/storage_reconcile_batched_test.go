package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

func TestStorageReconcileCheckpointKeepsBoundedSamplesAndAggregateCounts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "personal-space")
	accumulator := newStorageReconcileAccumulator(root, time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC), 1)
	accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcilePhysicalOrphan, OwnerID: "1001", RelativePath: "1001/file/objects/a.bin"})
	accumulator.add(storage.ReconcileDifference{Kind: storage.ReconcilePhysicalOrphan, OwnerID: "1002", RelativePath: "1002/file/objects/b.bin"})
	if !accumulator.truncated || accumulator.report.Counts[storage.ReconcilePhysicalOrphan] != 2 || len(accumulator.report.Differences) != 1 {
		t.Fatalf("bounded report = %#v, truncated=%v", accumulator.report, accumulator.truncated)
	}

	checkpoint := filepath.Join(t.TempDir(), "reconcile-checkpoint.json")
	state := storageReconcileCheckpoint{Schema: storageReconcileCheckpointSchema, RootDigest: "digest", Phase: "objects", ObjectCursor: "123"}
	accumulator.checkpoint(&state)
	if err := writeStorageReconcileCheckpoint(checkpoint, state); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	loaded, err := readStorageReconcileCheckpoint(checkpoint)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if loaded.ObjectCursor != "123" || !loaded.DifferencesTruncated || loaded.Report.Counts[storage.ReconcilePhysicalOrphan] != 2 || len(loaded.Report.Differences) != 1 {
		t.Fatalf("checkpoint round trip = %#v", loaded)
	}
	if strings.Contains(string(mustReadFile(t, checkpoint)), root) {
		t.Fatal("checkpoint must not persist the provider absolute root")
	}
}

func TestReconcileObjectPathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if _, _, ok := reconcileObjectPath(root, "1001", "../outside"); ok {
		t.Fatal("unsafe storage key was accepted")
	}
	path, relative, ok := reconcileObjectPath(root, "1001", "safe.bin")
	if !ok || !strings.HasSuffix(filepath.ToSlash(path), "/1001/file/objects/safe.bin") || relative != "1001/file/objects/safe.bin" {
		t.Fatalf("safe object path = %q %q %v", path, relative, ok)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
