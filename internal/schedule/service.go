package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/space"
	"github.com/campusos/CampusOS/pkg/idgen"
)

const (
	defaultRootDir        = "data/personal-space"
	defaultQuotaBytes     = int64(10 * 1024 * 1024)
	defaultMaxCourses     = 200
	defaultMaxImportBytes = int64(2 * 1024 * 1024)
	dateLayout            = "2006-01-02"
)

var (
	ErrPluginDisabled = errors.New("personal-schedule plugin is disabled")
	ErrInvalidInput   = errors.New("invalid schedule input")
	ErrQuotaExceeded  = errors.New("schedule exceeds personal space quota")
	ErrUnsupported    = errors.New("unsupported schedule file")
)

type Config struct {
	RootDir        string
	QuotaBytes     int64
	MaxCourses     int
	MaxImportBytes int64
}

type Service struct {
	cfg     Config
	enabled func() bool
	now     func() time.Time
}

func NewService(cfg Config) (*Service, error) {
	cfg = cfg.withDefaults()
	root, err := filepath.Abs(filepath.Clean(cfg.RootDir))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	cfg.RootDir = root
	if err := space.MigrateLegacyPersonalSpaceStorage(root); err != nil {
		return nil, err
	}
	return &Service{
		cfg:     cfg,
		enabled: func() bool { return true },
		now:     time.Now,
	}, nil
}

func NewDisabledService() *Service {
	return &Service{
		cfg:     Config{}.withDefaults(),
		enabled: func() bool { return false },
		now:     time.Now,
	}
}

func ConfigFromPluginConfig(raw, personalSpace map[string]interface{}) Config {
	cfg := Config{
		RootDir:        defaultRootDir,
		QuotaBytes:     defaultQuotaBytes,
		MaxCourses:     defaultMaxCourses,
		MaxImportBytes: defaultMaxImportBytes,
	}
	if value := stringConfig(personalSpace, "file_root"); value != "" {
		cfg.RootDir = space.NormalizePersonalSpaceFileRoot(value)
	}
	if value := int64Config(personalSpace, "default_quota_bytes"); value > 0 {
		cfg.QuotaBytes = value
	}
	if value := int64Config(personalSpace, "default_quota_mb"); value > 0 {
		cfg.QuotaBytes = value * 1024 * 1024
	}
	if value := int64Config(raw, "quota_bytes"); value > 0 {
		cfg.QuotaBytes = value
	}
	if value := int64Config(raw, "quota_mb"); value > 0 {
		cfg.QuotaBytes = value * 1024 * 1024
	}
	if value := intConfig(raw, "max_courses"); value > 0 {
		cfg.MaxCourses = value
	}
	if value := int64Config(raw, "max_import_bytes"); value > 0 {
		cfg.MaxImportBytes = value
	}
	if value := int64Config(raw, "max_import_mb"); value > 0 {
		cfg.MaxImportBytes = value * 1024 * 1024
	}
	return cfg.withDefaults()
}

func (cfg Config) withDefaults() Config {
	if strings.TrimSpace(cfg.RootDir) == "" {
		cfg.RootDir = defaultRootDir
	}
	cfg.RootDir = space.NormalizePersonalSpaceFileRoot(cfg.RootDir)
	if cfg.QuotaBytes <= 0 {
		cfg.QuotaBytes = defaultQuotaBytes
	}
	if cfg.MaxCourses <= 0 {
		cfg.MaxCourses = defaultMaxCourses
	}
	if cfg.MaxImportBytes <= 0 {
		cfg.MaxImportBytes = defaultMaxImportBytes
	}
	return cfg
}

func (s *Service) SetEnabledChecker(checker func() bool) {
	if checker == nil {
		s.enabled = func() bool { return true }
		return
	}
	s.enabled = checker
}

func (s *Service) Status() StatusResult {
	return StatusResult{
		Enabled:    s.enabled == nil || s.enabled(),
		PluginName: PluginName,
		Storage:    "personal-space-local-json",
	}
}

func (s *Service) Get(ctx context.Context, userID string) (*ScheduleResponse, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	schedule, err := s.load(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.response(schedule), nil
}

func (s *Service) Save(ctx context.Context, userID string, req UpsertRequest) (*ScheduleResponse, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	schedule := &Schedule{
		UserID:         userID,
		FirstWeekStart: strings.TrimSpace(req.FirstWeekStart),
		Settings:       req.Settings,
		Courses:        req.Courses,
		Metadata:       req.Metadata,
		UpdatedAt:      s.now().UTC(),
	}
	if err := s.normalize(schedule); err != nil {
		return nil, err
	}
	if err := s.write(ctx, schedule); err != nil {
		return nil, err
	}
	return s.response(schedule), nil
}

func (s *Service) Import(ctx context.Context, userID, filename string, size int64, data []byte, replace bool) (*ImportResult, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if size > s.cfg.MaxImportBytes || int64(len(data)) > s.cfg.MaxImportBytes {
		return nil, fmt.Errorf("%w: file is too large", ErrInvalidInput)
	}
	courses, warnings, err := ParseImport(filename, data)
	if err != nil {
		return nil, err
	}
	current, err := s.load(ctx, userID)
	if err != nil {
		return nil, err
	}
	if replace {
		current.Courses = courses
	} else {
		current.Courses = append(current.Courses, courses...)
	}
	current.UpdatedAt = s.now().UTC()
	if err := s.normalize(current); err != nil {
		return nil, err
	}
	if err := s.write(ctx, current); err != nil {
		return nil, err
	}
	return &ImportResult{
		Imported: len(courses),
		Replaced: replace,
		Warnings: warnings,
		Schedule: s.response(current),
	}, nil
}

func (s *Service) ensureEnabled() error {
	if s.enabled != nil && !s.enabled() {
		return ErrPluginDisabled
	}
	return nil
}

func (s *Service) load(_ context.Context, userID string) (*Schedule, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	path, err := s.schedulePath(userID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.defaultSchedule(userID), nil
		}
		return nil, err
	}
	var schedule Schedule
	if err := json.Unmarshal(raw, &schedule); err != nil {
		return nil, fmt.Errorf("%w: schedule json is invalid", ErrInvalidInput)
	}
	if schedule.UserID == "" {
		schedule.UserID = userID
	}
	if err := s.normalize(&schedule); err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (s *Service) write(_ context.Context, schedule *Schedule) error {
	path, err := s.schedulePath(schedule.UserID)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(schedule, "", "  ")
	if err != nil {
		return err
	}
	if err := s.checkQuota(schedule.UserID, int64(len(raw)), path); err != nil {
		return err
	}
	if _, err := space.EnsurePersonalSpaceLayout(s.cfg.RootDir, schedule.UserID); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func (s *Service) normalize(schedule *Schedule) error {
	if schedule == nil {
		return fmt.Errorf("%w: schedule is required", ErrInvalidInput)
	}
	if err := validateUserID(schedule.UserID); err != nil {
		return err
	}
	if strings.TrimSpace(schedule.FirstWeekStart) == "" {
		schedule.FirstWeekStart = mondayOf(s.now()).Format(dateLayout)
	}
	if _, err := time.ParseInLocation(dateLayout, schedule.FirstWeekStart, time.Local); err != nil {
		return fmt.Errorf("%w: first_week_start must be YYYY-MM-DD", ErrInvalidInput)
	}
	if schedule.Settings.PeriodsPerDay <= 0 {
		schedule.Settings.PeriodsPerDay = 12
	}
	if schedule.Settings.PeriodsPerDay > 24 {
		return fmt.Errorf("%w: periods_per_day cannot exceed 24", ErrInvalidInput)
	}
	if len(schedule.Settings.PeriodLabels) > 24 {
		return fmt.Errorf("%w: period_labels cannot exceed 24", ErrInvalidInput)
	}
	if len(schedule.Courses) > s.cfg.MaxCourses {
		return fmt.Errorf("%w: too many courses", ErrInvalidInput)
	}
	seen := map[string]struct{}{}
	for i := range schedule.Courses {
		course := &schedule.Courses[i]
		course.Name = strings.TrimSpace(course.Name)
		course.Teacher = strings.TrimSpace(course.Teacher)
		course.Location = strings.TrimSpace(course.Location)
		course.Note = strings.TrimSpace(course.Note)
		course.Color = normalizeColor(course.Color, i)
		if course.ID == "" {
			course.ID = strconv.FormatInt(idgen.New(), 10)
		}
		if _, ok := seen[course.ID]; ok {
			course.ID = strconv.FormatInt(idgen.New(), 10)
		}
		seen[course.ID] = struct{}{}
		if course.Name == "" {
			return fmt.Errorf("%w: course name is required", ErrInvalidInput)
		}
		if course.Weekday < 1 || course.Weekday > 7 {
			return fmt.Errorf("%w: course weekday must be 1-7", ErrInvalidInput)
		}
		if course.StartPeriod < 1 || course.EndPeriod < course.StartPeriod || course.EndPeriod > 24 {
			return fmt.Errorf("%w: invalid course period range", ErrInvalidInput)
		}
		course.Weeks = normalizeWeeks(course.Weeks)
	}
	sort.SliceStable(schedule.Courses, func(i, j int) bool {
		if schedule.Courses[i].Weekday != schedule.Courses[j].Weekday {
			return schedule.Courses[i].Weekday < schedule.Courses[j].Weekday
		}
		if schedule.Courses[i].StartPeriod != schedule.Courses[j].StartPeriod {
			return schedule.Courses[i].StartPeriod < schedule.Courses[j].StartPeriod
		}
		return schedule.Courses[i].Name < schedule.Courses[j].Name
	})
	return nil
}

func (s *Service) defaultSchedule(userID string) *Schedule {
	return &Schedule{
		UserID:         userID,
		FirstWeekStart: mondayOf(s.now()).Format(dateLayout),
		Settings: Settings{
			PeriodsPerDay: 12,
			ShowWeekend:   true,
		},
		Courses:   []Course{},
		UpdatedAt: s.now().UTC(),
	}
}

func (s *Service) response(schedule *Schedule) *ScheduleResponse {
	return &ScheduleResponse{
		Schedule: schedule,
		Week:     weekInfo(schedule.FirstWeekStart, s.now()),
		Enabled:  s.enabled == nil || s.enabled(),
	}
}

func (s *Service) schedulePath(userID string) (string, error) {
	return space.PersonalSpacePath(s.cfg.RootDir, userID, space.PersonalSpaceFileDir, "schedule", "schedule.json")
}

func (s *Service) userDir(userID string) (string, error) {
	if err := validateUserID(userID); err != nil {
		return "", err
	}
	target, err := space.PersonalSpaceUserDir(s.cfg.RootDir, userID)
	if err != nil {
		return "", fmt.Errorf("%w: invalid storage path", ErrInvalidInput)
	}
	return target, nil
}

func (s *Service) checkQuota(userID string, newSize int64, schedulePath string) error {
	userDir, err := s.userDir(userID)
	if err != nil {
		return err
	}
	var usage int64
	if err := filepath.WalkDir(userDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry == nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if samePath(path, schedulePath) {
			return nil
		}
		usage += info.Size()
		return nil
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if usage+newSize > s.cfg.QuotaBytes {
		return ErrQuotaExceeded
	}
	return nil
}

func samePath(a, b string) bool {
	aAbs, aErr := filepath.Abs(filepath.Clean(a))
	bAbs, bErr := filepath.Abs(filepath.Clean(b))
	return aErr == nil && bErr == nil && aAbs == bAbs
}

func validateUserID(userID string) error {
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("%w: user_id is required", ErrInvalidInput)
	}
	if _, err := strconv.ParseInt(userID, 10, 64); err != nil {
		return fmt.Errorf("%w: invalid user_id", ErrInvalidInput)
	}
	return nil
}

func weekInfo(firstWeekStart string, now time.Time) WeekInfo {
	start, err := time.ParseInLocation(dateLayout, firstWeekStart, time.Local)
	if err != nil {
		start = mondayOf(now)
	}
	today := dateOnly(now)
	diff := int(today.Sub(start).Hours() / 24)
	current := diff/7 + 1
	if current < 1 {
		current = 1
	}
	weekStart := start.AddDate(0, 0, (current-1)*7)
	weekEnd := weekStart.AddDate(0, 0, 6)
	return WeekInfo{
		CurrentWeek: current,
		WeekStart:   weekStart.Format(dateLayout),
		WeekEnd:     weekEnd.Format(dateLayout),
		Today:       today.Format(dateLayout),
	}
}

func mondayOf(value time.Time) time.Time {
	day := dateOnly(value)
	weekday := int(day.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return day.AddDate(0, 0, -(weekday - 1))
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.In(time.Local).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

func normalizeWeeks(weeks []int) []int {
	seen := map[int]struct{}{}
	out := make([]int, 0, len(weeks))
	for _, week := range weeks {
		if week < 1 || week > 60 {
			continue
		}
		if _, ok := seen[week]; ok {
			continue
		}
		seen[week] = struct{}{}
		out = append(out, week)
	}
	sort.Ints(out)
	return out
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func normalizeColor(value string, index int) string {
	value = strings.TrimSpace(value)
	if colorPattern.MatchString(value) {
		return value
	}
	palette := []string{"#2563eb", "#16a34a", "#d97706", "#dc2626", "#7c3aed", "#0891b2", "#be123c", "#4d7c0f"}
	return palette[index%len(palette)]
}

func stringConfig(raw map[string]interface{}, key string) string {
	if raw == nil {
		return ""
	}
	value, ok := raw[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func intConfig(raw map[string]interface{}, key string) int {
	if raw == nil {
		return 0
	}
	switch v := raw[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(v))
		return parsed
	default:
		return 0
	}
}

func int64Config(raw map[string]interface{}, key string) int64 {
	if raw == nil {
		return 0
	}
	switch v := raw[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return parsed
	default:
		return 0
	}
}
