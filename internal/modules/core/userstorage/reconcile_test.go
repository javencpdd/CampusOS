package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/pkg/observability"
)

func TestReconcileLocalReportsOnlySafeDifferences(t *testing.T) {
	root := t.TempDir()
	objectDir := filepath.Join(root, "1001", FileDir, "objects")
	if err := os.MkdirAll(objectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("expected")
	if err := os.WriteFile(filepath.Join(objectDir, "ok.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(objectDir, "orphan.bin"), []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "1001", FileDir, "schedule"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "1001", FileDir, "schedule", "legacy.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitkeep"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	now := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	report, err := ReconcileLocal(root, ReconcileSnapshot{
		Objects: []ReconcileObject{
			{ID: "1", OwnerID: "1001", StorageKey: "ok.bin", SizeBytes: int64(len(payload)), SHA256: hex.EncodeToString(hash[:]), Status: ObjectStatusReady},
			{ID: "2", OwnerID: "1001", StorageKey: "missing.bin", SizeBytes: 1, SHA256: "01", Status: ObjectStatusReady},
			{ID: "3", OwnerID: "1001", StorageKey: "pending.bin", Status: ObjectStatusPending, UpdatedAt: now.Add(-16 * time.Minute)},
		},
		Reservations: []ReconcileReservation{{ID: "r1", ObjectID: "3", OwnerID: "1001", Status: ObjectStatusPending, ExpiresAt: now.Add(-time.Minute)}},
		Accounts:     []ReconcileAccount{{OwnerID: "1001", UsedBytes: int64(len(payload) + len("orphan") + len("{}"))}},
	}, now)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if filepath.IsAbs(report.Root) || report.Root != filepath.Base(root) {
		t.Fatalf("report must expose only a root label, got %q", report.Root)
	}
	if report.Counts[ReconcileMetadataMissingFile] != 1 || report.Counts[ReconcilePhysicalOrphan] != 1 || report.Counts[ReconcileLegacyUnclassified] != 1 {
		t.Fatalf("unexpected report counts: %#v", report.Counts)
	}
	if report.Counts[ReconcilePendingObjectExpired] != 1 || report.Counts[ReconcileReservationExpired] != 1 {
		t.Fatalf("missing expired state: %#v", report.Counts)
	}
	if report.Counts[ReconcileLedgerMismatch] != 0 || report.Counts[ReconcileInvalidOwnerDirectory] != 1 {
		t.Fatalf("ledger should match physical usage: %#v", report.Counts)
	}
}

func TestReconcileLocalDoesNotFollowSymbolicLinks(t *testing.T) {
	if testing.Short() {
		t.Skip("symlink test requires local filesystem support")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "1001")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	report, err := ReconcileLocal(root, ReconcileSnapshot{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts[ReconcileUnsafePath] != 1 || report.Counts[ReconcileLegacyUnclassified] != 0 {
		t.Fatalf("symlink must be reported but not traversed: %#v", report.Counts)
	}
}

func TestRecordReconcileMetricsUsesFixedKindsOnly(t *testing.T) {
	collector := observability.NewCollector()
	RecordReconcileMetrics(collector, ReconcileReport{Counts: map[string]int{ReconcileUnsafePath: 2, ReconcilePhysicalOrphan: 1}})
	metrics := collector.PrometheusText()
	for _, expected := range []string{
		`campusos_storage_reconcile_differences{kind="physical_without_metadata"} 1`,
		`campusos_storage_reconcile_differences{kind="unsafe_path"} 2`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("reconcile metric missing %q: %s", expected, metrics)
		}
	}
	if strings.Contains(metrics, "1001") || strings.Contains(metrics, "C:\\") {
		t.Fatalf("reconcile metrics must not expose owner or paths: %s", metrics)
	}
}
