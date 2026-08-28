package schedule

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

func TestScanHistoricalSchedulesValidatesOwnerHashAndIndex(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "1001", corestorage.FileDir, "schedule", "terms")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	valid := `{"user_id":"1001","term_year":2025,"semester":"fall","first_week_start":"2025-09-01","settings":{"periods_per_day":12},"courses":[]}`
	if err := os.WriteFile(filepath.Join(dir, "2025-fall.json"), []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-spring.json"), []byte(`{"user_id":"999","term_year":2026,"semester":"spring","first_week_start":"2026-03-02"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(root, "1001", corestorage.FileDir, "schedule", "index.json")
	if err := os.WriteFile(index, []byte(`{"user_id":"1001","term_year":2025,"semester":"fall"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := ScanHistoricalSchedules(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Candidates) != 1 || !report.Candidates[0].ActiveInIndex || report.Candidates[0].SHA256 == "" {
		t.Fatalf("unexpected candidates: %#v", report.Candidates)
	}
	if len(report.Issues) != 1 || report.Issues[0].Kind != "owner_mismatch" {
		t.Fatalf("unexpected issues: %#v", report.Issues)
	}
}
