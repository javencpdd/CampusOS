package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	corestorage "github.com/campusos/CampusOS/internal/core/storage"
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
	storage corestorage.Port
	enabled func() bool
	now     func() time.Time
}

func NewService(cfg Config) (*Service, error) {
	return newService(cfg, nil)
}

// NewServiceWithStorage binds schedule JSON files to the User Storage Core.
func NewServiceWithStorage(cfg Config, storage corestorage.Port) (*Service, error) {
	if storage == nil {
		return nil, errors.New("user storage port is required")
	}
	cfg.RootDir = storage.Root()
	if quota, ok := storage.(corestorage.Quota); ok {
		cfg.QuotaBytes = quota.QuotaBytes("")
	}
	return newService(cfg, storage)
}

func newService(cfg Config, storage corestorage.Port) (*Service, error) {
	cfg = cfg.withDefaults()
	root, err := filepath.Abs(filepath.Clean(cfg.RootDir))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	cfg.RootDir = root
	if storage == nil {
		storage, err = corestorage.NewLocalAdapterWithQuota(root, cfg.QuotaBytes)
		if err != nil {
			return nil, err
		}
	}
	return &Service{
		cfg:     cfg,
		storage: storage,
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
		cfg.RootDir = corestorage.NormalizeRoot(value)
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
	cfg.RootDir = corestorage.NormalizeRoot(cfg.RootDir)
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

// GetTerm returns one semester-specific schedule JSON without changing the
// user's active schedule selection.
func (s *Service) GetTerm(ctx context.Context, userID string, termYear int, semester string) (*ScheduleResponse, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	schedule, err := s.loadTerm(ctx, userID, termYear, semester, false)
	if err != nil {
		return nil, err
	}
	return s.response(schedule), nil
}

// ListTerms lists the independent JSON schedules available to one user.
func (s *Service) ListTerms(ctx context.Context, userID string) (*TermsResponse, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	if err := s.migrateLegacySchedule(ctx, userID); err != nil {
		return nil, err
	}
	return s.listTerms(ctx, userID)
}

// ActivateTerm selects an existing term or creates an empty term JSON. The
// first-week date remains user-configurable after the selection is made.
func (s *Service) ActivateTerm(ctx context.Context, userID string, termYear int, semester string) (*ScheduleResponse, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	schedule, err := s.loadTerm(ctx, userID, termYear, semester, true)
	if err != nil {
		return nil, err
	}
	if err := s.writeTerm(ctx, schedule); err != nil {
		return nil, err
	}
	if err := s.writeActiveTerm(ctx, schedule); err != nil {
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
		TermYear:       req.TermYear,
		Semester:       req.Semester,
		FirstWeekStart: strings.TrimSpace(req.FirstWeekStart),
		Settings:       req.Settings,
		Courses:        req.Courses,
		Metadata:       req.Metadata,
		UpdatedAt:      s.now().UTC(),
	}
	if err := s.normalize(schedule); err != nil {
		return nil, err
	}
	if err := s.writeTerm(ctx, schedule); err != nil {
		return nil, err
	}
	if err := s.writeActiveTerm(ctx, schedule); err != nil {
		return nil, err
	}
	return s.response(schedule), nil
}

func (s *Service) Import(ctx context.Context, userID, filename string, size int64, data []byte, replace bool, termYear int, semester string) (*ImportResult, error) {
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
	current, err := s.loadTerm(ctx, userID, termYear, semester, true)
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
	if err := s.writeTerm(ctx, current); err != nil {
		return nil, err
	}
	if err := s.writeActiveTerm(ctx, current); err != nil {
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

func (s *Service) load(ctx context.Context, userID string) (*Schedule, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	if err := s.migrateLegacySchedule(ctx, userID); err != nil {
		return nil, err
	}
	index, err := s.readActiveTerm(userID)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		terms, listErr := s.listTerms(ctx, userID)
		if listErr != nil {
			return nil, listErr
		}
		if len(terms.Items) == 0 {
			return s.defaultSchedule(userID), nil
		}
		selected := terms.Items[0]
		return s.loadTerm(ctx, userID, selected.TermYear, selected.Semester, false)
	}
	return s.loadTerm(ctx, userID, index.TermYear, index.Semester, false)
}

func (s *Service) loadTerm(_ context.Context, userID string, termYear int, semester string, create bool) (*Schedule, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	termYear, semester, err := s.normalizeTerm(termYear, semester)
	if err != nil {
		return nil, err
	}
	path, err := s.termSchedulePath(userID, termYear, semester)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if create {
				return s.defaultScheduleForTerm(userID, termYear, semester), nil
			}
			return nil, fmt.Errorf("%w: schedule term not found", ErrInvalidInput)
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
	if schedule.TermYear != termYear || schedule.Semester != semester {
		return nil, fmt.Errorf("%w: schedule term does not match file path", ErrInvalidInput)
	}
	return &schedule, nil
}

func (s *Service) writeTerm(_ context.Context, schedule *Schedule) error {
	path, err := s.termSchedulePath(schedule.UserID, schedule.TermYear, schedule.Semester)
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
	if _, err := s.storage.EnsureLayout(schedule.UserID); err != nil {
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
	if schedule.TermYear == 0 {
		schedule.TermYear = defaultTermYear(s.now())
	}
	if schedule.TermYear < 2000 || schedule.TermYear > 2200 {
		return fmt.Errorf("%w: term_year must be between 2000 and 2200", ErrInvalidInput)
	}
	if strings.TrimSpace(schedule.Semester) == "" {
		schedule.Semester = defaultTermSemester(s.now())
	} else {
		semester, err := normalizeSemester(schedule.Semester)
		if err != nil {
			return err
		}
		schedule.Semester = semester
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
	return s.defaultScheduleForTerm(userID, defaultTermYear(s.now()), defaultTermSemester(s.now()))
}

func (s *Service) defaultScheduleForTerm(userID string, termYear int, semester string) *Schedule {
	return &Schedule{
		UserID:         userID,
		TermYear:       termYear,
		Semester:       semester,
		FirstWeekStart: mondayOf(s.now()).Format(dateLayout),
		Settings: Settings{
			PeriodsPerDay: 12,
			ShowWeekend:   true,
		},
		Courses:   []Course{},
		UpdatedAt: s.now().UTC(),
	}
}

func defaultTermYear(now time.Time) int {
	return now.In(time.Local).Year()
}

func defaultTermSemester(now time.Time) string {
	if now.In(time.Local).Month() >= time.August {
		return SemesterFall
	}
	return SemesterSpring
}

func normalizeSemester(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case SemesterSpring, "春", "春季", "春季学期":
		return SemesterSpring, nil
	case SemesterFall, "autumn", "秋", "秋季", "秋季学期":
		return SemesterFall, nil
	default:
		return "", fmt.Errorf("%w: semester must be spring or fall", ErrInvalidInput)
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
	return s.legacySchedulePath(userID)
}

func (s *Service) legacySchedulePath(userID string) (string, error) {
	return s.storage.Path(userID, corestorage.FileDir, "schedule", "schedule.json")
}

func (s *Service) termsDir(userID string) (string, error) {
	return s.storage.Path(userID, corestorage.FileDir, "schedule", "terms")
}

func (s *Service) indexPath(userID string) (string, error) {
	return s.storage.Path(userID, corestorage.FileDir, "schedule", "index.json")
}

func (s *Service) termSchedulePath(userID string, termYear int, semester string) (string, error) {
	termYear, semester, err := s.normalizeTerm(termYear, semester)
	if err != nil {
		return "", err
	}
	return s.storage.Path(userID, corestorage.FileDir, "schedule", "terms", termKey(termYear, semester)+".json")
}

func (s *Service) normalizeTerm(termYear int, semester string) (int, string, error) {
	if termYear == 0 {
		termYear = defaultTermYear(s.now())
	}
	if termYear < 2000 || termYear > 2200 {
		return 0, "", fmt.Errorf("%w: term_year must be between 2000 and 2200", ErrInvalidInput)
	}
	if strings.TrimSpace(semester) == "" {
		semester = defaultTermSemester(s.now())
	}
	normalized, err := normalizeSemester(semester)
	if err != nil {
		return 0, "", err
	}
	return termYear, normalized, nil
}

func termKey(termYear int, semester string) string {
	return fmt.Sprintf("%d-%s", termYear, semester)
}

func (s *Service) readActiveTerm(userID string) (*scheduleIndex, error) {
	path, err := s.indexPath(userID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var index scheduleIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return nil, fmt.Errorf("%w: schedule index json is invalid", ErrInvalidInput)
	}
	if index.UserID == "" {
		index.UserID = userID
	}
	if index.UserID != userID {
		return nil, fmt.Errorf("%w: schedule index user does not match", ErrInvalidInput)
	}
	termYear, semester, err := s.normalizeTerm(index.TermYear, index.Semester)
	if err != nil {
		return nil, err
	}
	index.TermYear = termYear
	index.Semester = semester
	return &index, nil
}

func (s *Service) writeActiveTerm(_ context.Context, schedule *Schedule) error {
	path, err := s.indexPath(schedule.UserID)
	if err != nil {
		return err
	}
	index := scheduleIndex{
		UserID:    schedule.UserID,
		TermYear:  schedule.TermYear,
		Semester:  schedule.Semester,
		UpdatedAt: s.now().UTC(),
	}
	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	if err := s.checkQuota(schedule.UserID, int64(len(raw)), path); err != nil {
		return err
	}
	if _, err := s.storage.EnsureLayout(schedule.UserID); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func (s *Service) listTerms(_ context.Context, userID string) (*TermsResponse, error) {
	index, indexErr := s.readActiveTerm(userID)
	if indexErr != nil && !errors.Is(indexErr, os.ErrNotExist) {
		return nil, indexErr
	}
	dir, err := s.termsDir(userID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &TermsResponse{Items: []TermSummary{}}, nil
		}
		return nil, err
	}
	items := make([]TermSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
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
		expectedPath, err := s.termSchedulePath(userID, schedule.TermYear, schedule.Semester)
		if err != nil {
			return nil, err
		}
		if !samePath(expectedPath, filepath.Join(dir, entry.Name())) {
			return nil, fmt.Errorf("%w: schedule term file name does not match content", ErrInvalidInput)
		}
		items = append(items, TermSummary{
			TermYear:       schedule.TermYear,
			Semester:       schedule.Semester,
			FirstWeekStart: schedule.FirstWeekStart,
			CourseCount:    len(schedule.Courses),
			UpdatedAt:      schedule.UpdatedAt,
			Active:         index != nil && index.TermYear == schedule.TermYear && index.Semester == schedule.Semester,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].TermYear != items[j].TermYear {
			return items[i].TermYear > items[j].TermYear
		}
		return items[i].Semester == SemesterFall && items[j].Semester != SemesterFall
	})
	return &TermsResponse{Items: items}, nil
}

func (s *Service) migrateLegacySchedule(ctx context.Context, userID string) error {
	legacyPath, err := s.legacySchedulePath(userID)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(legacyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var schedule Schedule
	if err := json.Unmarshal(raw, &schedule); err != nil {
		return fmt.Errorf("%w: legacy schedule json is invalid", ErrInvalidInput)
	}
	if schedule.UserID == "" {
		schedule.UserID = userID
	}
	if err := s.normalize(&schedule); err != nil {
		return err
	}
	if schedule.UserID != userID {
		return fmt.Errorf("%w: legacy schedule user does not match", ErrInvalidInput)
	}
	termPath, err := s.termSchedulePath(userID, schedule.TermYear, schedule.Semester)
	if err != nil {
		return err
	}
	if _, err := os.Stat(termPath); errors.Is(err, os.ErrNotExist) {
		migrated, err := json.MarshalIndent(&schedule, "", "  ")
		if err != nil {
			return err
		}
		// The legacy file was already counted in this user's quota. Do not fail
		// a one-time relocation merely because both paths exist briefly.
		if _, err := s.storage.EnsureLayout(userID); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(termPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(termPath, migrated, 0o644); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := os.Remove(legacyPath); err != nil {
		return err
	}
	if _, err := s.readActiveTerm(userID); errors.Is(err, os.ErrNotExist) {
		if err := s.writeActiveTerm(ctx, &schedule); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

func (s *Service) userDir(userID string) (string, error) {
	if err := validateUserID(userID); err != nil {
		return "", err
	}
	target, err := s.storage.Path(userID)
	if err != nil {
		return "", fmt.Errorf("%w: invalid storage path", ErrInvalidInput)
	}
	return target, nil
}

func (s *Service) checkQuota(userID string, newSize int64, schedulePath string) error {
	usage, err := s.storage.Usage(userID)
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(schedulePath); statErr == nil {
		usage -= info.Size()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
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
