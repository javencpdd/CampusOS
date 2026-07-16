package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/features/personalspace"
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

func TestTermsUseIndependentSemesterJSONFiles(t *testing.T) {
	svc, err := NewService(Config{RootDir: t.TempDir(), QuotaBytes: 1024 * 1024})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	_, err = svc.Save(ctx, "1001", UpsertRequest{
		TermYear:       2026,
		Semester:       SemesterSpring,
		FirstWeekStart: "2026-02-23",
		Settings:       Settings{PeriodsPerDay: 12},
		Courses: []Course{{
			Name: "春季课程", Weekday: 1, StartPeriod: 1, EndPeriod: 2, Weeks: []int{1},
		}},
	})
	if err != nil {
		t.Fatalf("save spring term: %v", err)
	}
	_, err = svc.Save(ctx, "1001", UpsertRequest{
		TermYear:       2026,
		Semester:       SemesterFall,
		FirstWeekStart: "2026-09-07",
		Settings:       Settings{PeriodsPerDay: 12},
		Courses: []Course{{
			Name: "秋季课程", Weekday: 2, StartPeriod: 3, EndPeriod: 4, Weeks: []int{1},
		}},
	})
	if err != nil {
		t.Fatalf("save fall term: %v", err)
	}

	spring, err := svc.GetTerm(ctx, "1001", 2026, SemesterSpring)
	if err != nil {
		t.Fatalf("get spring term: %v", err)
	}
	if len(spring.Schedule.Courses) != 1 || spring.Schedule.Courses[0].Name != "春季课程" {
		t.Fatalf("spring JSON was overwritten: %#v", spring.Schedule.Courses)
	}
	active, err := svc.Get(ctx, "1001")
	if err != nil {
		t.Fatalf("get active term: %v", err)
	}
	if active.Schedule.Semester != SemesterFall || active.Schedule.Courses[0].Name != "秋季课程" {
		t.Fatalf("expected fall term to become active: %#v", active.Schedule)
	}
	terms, err := svc.ListTerms(ctx, "1001")
	if err != nil {
		t.Fatalf("list terms: %v", err)
	}
	if len(terms.Items) != 2 || !terms.Items[0].Active || terms.Items[0].Semester != SemesterFall {
		t.Fatalf("unexpected term index: %#v", terms.Items)
	}
	for _, semester := range []string{SemesterSpring, SemesterFall} {
		path, err := svc.termSchedulePath("1001", 2026, semester)
		if err != nil {
			t.Fatalf("term path: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("semester JSON missing at %s: %v", path, err)
		}
	}
}

func TestImportWritesIntoSelectedTermJSON(t *testing.T) {
	svc, err := NewService(Config{RootDir: t.TempDir(), QuotaBytes: 1024 * 1024})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	csvData := []byte("课程名称,时间,开始周,结束周\n离散数学,星期三 上午3-上午4,1,16\n")
	result, err := svc.Import(context.Background(), "1001", "spring.csv", int64(len(csvData)), csvData, true, 2028, SemesterSpring)
	if err != nil {
		t.Fatalf("import selected term: %v", err)
	}
	if result.Schedule.Schedule.TermYear != 2028 || result.Schedule.Schedule.Semester != SemesterSpring {
		t.Fatalf("unexpected imported term: %#v", result.Schedule.Schedule)
	}
	if len(result.Schedule.Schedule.Courses) != 1 || result.Schedule.Schedule.Courses[0].Name != "离散数学" {
		t.Fatalf("unexpected imported courses: %#v", result.Schedule.Schedule.Courses)
	}
	path, err := svc.termSchedulePath("1001", 2028, SemesterSpring)
	if err != nil {
		t.Fatalf("term path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("selected term JSON missing: %v", err)
	}
}

func TestLegacyScheduleMigratesToTermJSON(t *testing.T) {
	svc, err := NewService(Config{RootDir: t.TempDir(), QuotaBytes: 1024 * 1024})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	legacy := &Schedule{
		UserID:         "1001",
		TermYear:       2025,
		Semester:       SemesterFall,
		FirstWeekStart: "2025-09-01",
		Settings:       Settings{PeriodsPerDay: 12},
		Courses:        []Course{},
		UpdatedAt:      time.Now().UTC(),
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy schedule: %v", err)
	}
	legacyPath, err := svc.legacySchedulePath("1001")
	if err != nil {
		t.Fatalf("legacy path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("create legacy directory: %v", err)
	}
	if err := os.WriteFile(legacyPath, raw, 0o644); err != nil {
		t.Fatalf("write legacy schedule: %v", err)
	}

	got, err := svc.Get(context.Background(), "1001")
	if err != nil {
		t.Fatalf("get migrated schedule: %v", err)
	}
	if got.Schedule.TermYear != 2025 || got.Schedule.Semester != SemesterFall {
		t.Fatalf("unexpected migrated term: %#v", got.Schedule)
	}
	termPath, err := svc.termSchedulePath("1001", 2025, SemesterFall)
	if err != nil {
		t.Fatalf("term path: %v", err)
	}
	if _, err := os.Stat(termPath); err != nil {
		t.Fatalf("migrated term JSON missing: %v", err)
	}
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy JSON should be removed after migration, got %v", err)
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
