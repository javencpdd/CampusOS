package schedule

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/space"
)

func TestSaveAndGetSchedule(t *testing.T) {
	svc, err := NewService(Config{RootDir: t.TempDir(), QuotaBytes: 1024 * 1024})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.now = func() time.Time {
		return time.Date(2026, 7, 9, 12, 0, 0, 0, time.Local)
	}

	saved, err := svc.Save(context.Background(), "1001", UpsertRequest{
		TermYear:       2026,
		Semester:       SemesterSpring,
		FirstWeekStart: "2026-07-06",
		Settings:       Settings{PeriodsPerDay: 12, ShowWeekend: true},
		Courses: []Course{{
			Name:        "机器学习",
			Teacher:     "张宁",
			Location:    "6E-206",
			Weekday:     1,
			StartPeriod: 5,
			EndPeriod:   6,
			Weeks:       []int{4, 5, 6},
		}},
	})
	if err != nil {
		t.Fatalf("save schedule: %v", err)
	}
	if saved.Week.CurrentWeek != 1 {
		t.Fatalf("unexpected current week: %#v", saved.Week)
	}

	got, err := svc.Get(context.Background(), "1001")
	if err != nil {
		t.Fatalf("get schedule: %v", err)
	}
	if len(got.Schedule.Courses) != 1 || got.Schedule.Courses[0].Name != "机器学习" {
		t.Fatalf("unexpected courses: %#v", got.Schedule.Courses)
	}
	if got.Schedule.TermYear != 2026 || got.Schedule.Semester != SemesterSpring {
		t.Fatalf("unexpected term: %#v", got.Schedule)
	}
	path, err := svc.schedulePath("1001")
	if err != nil {
		t.Fatalf("schedule path: %v", err)
	}
	if want := filepath.Join("1001", space.PersonalSpaceFileDir, "schedule", "schedule.json"); !strings.HasSuffix(path, want) {
		t.Fatalf("unexpected schedule path: %q", path)
	}
}

func TestScheduleDefaultsAndValidatesTerm(t *testing.T) {
	svc, err := NewService(Config{RootDir: t.TempDir(), QuotaBytes: 1024 * 1024})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.now = func() time.Time {
		return time.Date(2027, time.November, 10, 12, 0, 0, 0, time.Local)
	}
	result, err := svc.Save(context.Background(), "1001", UpsertRequest{
		FirstWeekStart: "2027-09-06",
		Settings:       Settings{PeriodsPerDay: 12},
	})
	if err != nil {
		t.Fatalf("save default term: %v", err)
	}
	if result.Schedule.TermYear != 2027 || result.Schedule.Semester != SemesterFall {
		t.Fatalf("unexpected default term: %#v", result.Schedule)
	}
	_, err = svc.Save(context.Background(), "1001", UpsertRequest{
		Semester:       "winter",
		FirstWeekStart: "2027-09-06",
		Settings:       Settings{PeriodsPerDay: 12},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid semester error, got %v", err)
	}
}

func TestConfigUsesPersonalSpaceRoot(t *testing.T) {
	cfg := ConfigFromPluginConfig(
		map[string]interface{}{"data_root": "data/separate-schedule-root"},
		map[string]interface{}{"file_root": "data/personal-space"},
	)
	if cfg.RootDir != "data/personal-space" {
		t.Fatalf("schedule should use the personal-space root, got %q", cfg.RootDir)
	}
	defaultOnly := ConfigFromPluginConfig(map[string]interface{}{"data_root": "data/separate-schedule-root"}, nil)
	if defaultOnly.RootDir != "data/personal-space" {
		t.Fatalf("legacy schedule data_root must not override the unified root, got %q", defaultOnly.RootDir)
	}
}

func TestImportXLSFixture(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "Todo", "tmp", "class.xls")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("xls fixture not available: %v", err)
	}

	courses, warnings, err := ParseImport("class.xls", data)
	if err != nil {
		t.Fatalf("parse xls fixture: %v warnings=%v", err, warnings)
	}
	if len(courses) != 8 {
		t.Fatalf("expected 8 courses, got %d", len(courses))
	}
	first := courses[0]
	if first.Name != "自然辩证法概论" || first.Weekday != 3 || first.StartPeriod != 3 || first.EndPeriod != 4 {
		t.Fatalf("unexpected first course: %#v", first)
	}
	if len(first.Weeks) != 9 || first.Weeks[0] != 1 || first.Weeks[8] != 9 {
		t.Fatalf("unexpected first course weeks: %#v", first.Weeks)
	}
}
