package schedule

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	academicterm "github.com/campusos/CampusOS/internal/modules/core/academicterm"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/idgen"
)

const (
	defaultRootDir        = "data/personal-space"
	defaultQuotaBytes     = corestorage.DefaultQuotaBytes
	defaultMaxCourses     = 200
	defaultMaxImportBytes = int64(2 * 1024 * 1024)
	dateLayout            = "2006-01-02"
)

var (
	ErrPluginDisabled    = errors.New("personal-schedule plugin is disabled")
	ErrInvalidInput      = errors.New("invalid schedule input")
	ErrQuotaExceeded     = errors.New("schedule exceeds personal space quota")
	ErrUnsupported       = errors.New("unsupported schedule file")
	ErrObjectUnavailable = errors.New("schedule object is unavailable")
)

type Config struct {
	RootDir        string
	QuotaBytes     int64
	MaxCourses     int
	MaxImportBytes int64
}

type Service struct {
	cfg            Config
	storage        corestorage.Port
	academicTerms  academicterm.Port
	termReferences TermReferenceRepository
	objects        corestorage.ObjectPort
	compatibility  corestorage.CompatibilityLedger
	enabled        func() bool
	now            func() time.Time
}

func NewService(cfg Config) (*Service, error) {
	return newService(cfg, nil)
}

// NewServiceWithStorage binds schedule JSON files to the User Storage Core.
func NewServiceWithStorage(cfg Config, storage corestorage.Port) (*Service, error) {
	return NewServiceWithStorageAndTerms(cfg, storage, nil)
}

// NewServiceWithStorageAndTerms binds schedule persistence to the user
// storage adapter and, when supplied, makes AcademicTerm the sole authority
// for new schedule terms. A nil AcademicTerm port is retained only for
// isolated legacy tests and standalone compatibility tools.
func NewServiceWithStorageAndTerms(cfg Config, storage corestorage.Port, academicTerms academicterm.Port) (*Service, error) {
	if storage == nil {
		return nil, errors.New("user storage port is required")
	}
	cfg.RootDir = storage.Root()
	if quota, ok := storage.(corestorage.Quota); ok {
		cfg.QuotaBytes = quota.QuotaBytes("")
	}
	svc, err := newService(cfg, storage)
	if err != nil {
		return nil, err
	}
	svc.academicTerms = academicTerms
	return svc, nil
}

// SetObjectPort enables dual writing of an immutable schedule Object alongside
// the retained JSON compatibility file. Production module startup always
// supplies this port; the nil case is retained only for isolated legacy tests.
func (s *Service) SetObjectPort(objects corestorage.ObjectPort) {
	s.objects = objects
	if ledger, ok := objects.(corestorage.CompatibilityLedger); ok {
		s.compatibility = ledger
	} else {
		s.compatibility = nil
	}
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
	if s.academicTerms != nil && s.termReferences != nil {
		schedule, err := s.loadManagedTermByBusinessKey(ctx, userID, termYear, semester)
		if err != nil {
			return nil, err
		}
		return s.response(schedule), nil
	}
	schedule, err := s.loadTerm(ctx, userID, termYear, semester, false)
	if err != nil {
		return nil, err
	}
	return s.response(schedule), nil
}

// GetTermByID reads a term selected from the governed term list. Closed terms
// are readable only after this user already has a durable term reference.
func (s *Service) GetTermByID(ctx context.Context, userID, academicTermID string) (*ScheduleResponse, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if s.academicTerms == nil || s.termReferences == nil {
		return nil, fmt.Errorf("%w: governed term lookup is unavailable", ErrInvalidInput)
	}
	term, err := s.academicTerms.Get(ctx, strings.TrimSpace(academicTermID))
	if err != nil {
		return nil, err
	}
	ref, err := s.termReferences.Get(ctx, userID, term.ID)
	if err != nil {
		if errors.Is(err, ErrTermReferenceNotFound) {
			return nil, academicterm.ErrNotFound
		}
		return nil, err
	}
	schedule, err := s.loadReferencedTerm(ctx, userID, term, ref)
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
	if s.academicTerms != nil && s.termReferences != nil {
		return s.listManagedTerms(ctx, userID)
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
	termYear, semester, term, err := s.resolveWritableTerm(ctx, termYear, semester)
	if err != nil {
		return nil, err
	}
	if term != nil {
		return s.activateOpenTerm(ctx, userID, *term)
	}
	return s.activateLegacyTerm(ctx, userID, termYear, semester)
}

// ActivateTermByID supports the governed picker. An archived term is not a
// creation target, but the owner may select it as their current view when a
// previously committed reference exists.
func (s *Service) ActivateTermByID(ctx context.Context, userID, academicTermID string) (*ScheduleResponse, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	if s.academicTerms == nil || s.termReferences == nil {
		return nil, fmt.Errorf("%w: governed term selection is unavailable", ErrInvalidInput)
	}
	term, err := s.academicTerms.Get(ctx, strings.TrimSpace(academicTermID))
	if err != nil {
		return nil, err
	}
	if term.Status == academicterm.StatusOpen {
		return s.activateOpenTerm(ctx, userID, term)
	}
	ref, err := s.termReferences.Get(ctx, userID, term.ID)
	if err != nil {
		if errors.Is(err, ErrTermReferenceNotFound) {
			return nil, academicterm.ErrClosed
		}
		return nil, err
	}
	if err := s.termReferences.SetPreference(ctx, userID, term.ID); err != nil {
		return nil, err
	}
	schedule, err := s.loadReferencedTerm(ctx, userID, term, ref)
	if err != nil {
		return nil, err
	}
	if err := s.writeActiveTerm(ctx, schedule); err != nil {
		return nil, err
	}
	return s.response(schedule), nil
}

func (s *Service) activateOpenTerm(ctx context.Context, userID string, term academicterm.Term) (*ScheduleResponse, error) {
	if s.termReferences != nil {
		if ref, err := s.termReferences.Get(ctx, userID, term.ID); err == nil {
			if err := s.termReferences.SetPreference(ctx, userID, term.ID); err != nil {
				return nil, err
			}
			schedule, err := s.loadReferencedTerm(ctx, userID, term, ref)
			if err != nil {
				return nil, err
			}
			if err := s.writeActiveTerm(ctx, schedule); err != nil {
				return nil, err
			}
			return s.response(schedule), nil
		} else if !errors.Is(err, ErrTermReferenceNotFound) {
			return nil, err
		}
	}
	schedule, err := s.loadTerm(ctx, userID, term.Year, term.Semester, true)
	if err != nil {
		return nil, err
	}
	if schedule.FirstWeekStart == "" {
		schedule.FirstWeekStart = term.FirstWeekStart
	}
	schedule.AcademicTermID, schedule.TermStatus, schedule.TermVersion = term.ID, term.Status, 0
	if err := s.normalize(schedule); err != nil {
		return nil, err
	}
	if err := s.writeTerm(ctx, schedule, 0); err != nil {
		return nil, err
	}
	if err := s.writeActiveTerm(ctx, schedule); err != nil {
		return nil, err
	}
	return s.response(schedule), nil
}

func (s *Service) activateLegacyTerm(ctx context.Context, userID string, termYear int, semester string) (*ScheduleResponse, error) {
	schedule, err := s.loadTerm(ctx, userID, termYear, semester, true)
	if err != nil {
		return nil, err
	}
	if schedule.FirstWeekStart == "" {
		schedule.FirstWeekStart = mondayOf(s.now()).Format(dateLayout)
	}
	if err := s.normalize(schedule); err != nil {
		return nil, err
	}
	if err := s.writeTerm(ctx, schedule, 0); err != nil {
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
	termYear, semester, term, err := s.resolveWritableTerm(ctx, req.TermYear, req.Semester)
	if err != nil {
		return nil, err
	}
	firstWeekStart := strings.TrimSpace(req.FirstWeekStart)
	var existing *Schedule
	var loadErr error
	if term != nil && s.termReferences != nil {
		if ref, refErr := s.termReferences.Get(ctx, userID, term.ID); refErr == nil {
			existing, loadErr = s.loadReferencedTerm(ctx, userID, *term, ref)
		} else if !errors.Is(refErr, ErrTermReferenceNotFound) {
			return nil, refErr
		}
	} else {
		existing, loadErr = s.loadTerm(ctx, userID, termYear, semester, false)
	}
	if loadErr == nil && existing != nil {
		// The platform term establishes the date for new schedules. A user may
		// not silently rewrite an already-created term's calendar through Save
		// or the raw JSON editor.
		firstWeekStart = existing.FirstWeekStart
	} else if firstWeekStart == "" && term != nil {
		firstWeekStart = term.FirstWeekStart
	}
	schedule := &Schedule{
		UserID:         userID,
		AcademicTermID: termID(term),
		TermStatus:     termStatus(term),
		TermYear:       termYear,
		Semester:       semester,
		FirstWeekStart: firstWeekStart,
		Settings:       req.Settings,
		Courses:        req.Courses,
		Metadata:       req.Metadata,
		UpdatedAt:      s.now().UTC(),
	}
	if err := s.normalize(schedule); err != nil {
		return nil, err
	}
	if err := s.writeTerm(ctx, schedule, req.ExpectedVersion); err != nil {
		return nil, err
	}
	if err := s.writeActiveTerm(ctx, schedule); err != nil {
		return nil, err
	}
	return s.response(schedule), nil
}

func (s *Service) Import(ctx context.Context, userID, filename string, size int64, data []byte, replace bool, termYear int, semester string, expectedVersion int64) (*ImportResult, error) {
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
	termYear, semester, term, err := s.resolveWritableTerm(ctx, termYear, semester)
	if err != nil {
		return nil, err
	}
	var current *Schedule
	if term != nil && s.termReferences != nil {
		if ref, refErr := s.termReferences.Get(ctx, userID, term.ID); refErr == nil {
			current, err = s.loadReferencedTerm(ctx, userID, *term, ref)
		} else if errors.Is(refErr, ErrTermReferenceNotFound) {
			current = s.emptyScheduleForTerm(userID, termYear, semester)
		} else {
			return nil, refErr
		}
	} else {
		current, err = s.loadTerm(ctx, userID, termYear, semester, true)
	}
	if err != nil {
		return nil, err
	}
	if current.FirstWeekStart == "" {
		if term != nil {
			current.FirstWeekStart = term.FirstWeekStart
		} else {
			current.FirstWeekStart = mondayOf(s.now()).Format(dateLayout)
		}
	}
	if term != nil {
		current.AcademicTermID, current.TermStatus = term.ID, term.Status
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
	if err := s.writeTerm(ctx, current, expectedVersion); err != nil {
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
	if s.academicTerms != nil && s.termReferences != nil {
		termID, preferenceErr := s.termReferences.Preference(ctx, userID)
		if preferenceErr == nil {
			term, err := s.academicTerms.Get(ctx, termID)
			if err != nil {
				return nil, err
			}
			ref, err := s.termReferences.Get(ctx, userID, term.ID)
			if err != nil {
				return nil, err
			}
			return s.loadReferencedTerm(ctx, userID, term, ref)
		}
		if !errors.Is(preferenceErr, ErrTermReferenceNotFound) {
			return nil, preferenceErr
		}
		refs, err := s.termReferences.List(ctx, userID)
		if err != nil {
			return nil, err
		}
		if len(refs) == 0 {
			return s.defaultSchedule(ctx, userID)
		}
		term, err := s.academicTerms.Get(ctx, refs[0].AcademicTermID)
		if err != nil {
			return nil, err
		}
		return s.loadReferencedTerm(ctx, userID, term, refs[0])
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
			return s.defaultSchedule(ctx, userID)
		}
		selected := terms.Items[0]
		return s.loadTerm(ctx, userID, selected.TermYear, selected.Semester, false)
	}
	return s.loadTerm(ctx, userID, index.TermYear, index.Semester, false)
}

func (s *Service) loadManagedTermByBusinessKey(ctx context.Context, userID string, termYear int, semester string) (*Schedule, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	termYear, semester, err := s.normalizeTerm(termYear, semester)
	if err != nil {
		return nil, err
	}
	refs, err := s.termReferences.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		term, termErr := s.academicTerms.Get(ctx, ref.AcademicTermID)
		if termErr != nil {
			return nil, termErr
		}
		if term.Year == termYear && term.Semester == semester {
			return s.loadReferencedTerm(ctx, userID, term, ref)
		}
	}
	// Keep the old year/semester query compatible for open terms, but never
	// let it create a missing binding or expose an unconfigured JSON file.
	if _, err := s.academicTerms.FindOpen(ctx, termYear, semester); err != nil {
		return nil, err
	}
	return nil, academicterm.ErrNotFound
}

func (s *Service) loadReferencedTerm(ctx context.Context, userID string, term academicterm.Term, ref TermReference) (*Schedule, error) {
	schedule, err := s.loadTerm(ctx, userID, term.Year, term.Semester, false)
	if err != nil && ref.CurrentObjectID != "" && s.objects != nil {
		schedule, err = s.loadTermObject(ctx, userID, ref.CurrentObjectID)
	}
	if err != nil {
		if ref.CurrentObjectID != "" {
			return nil, fmt.Errorf("%w: the last committed schedule cannot be read", ErrObjectUnavailable)
		}
		return nil, err
	}
	if schedule.UserID != userID || schedule.TermYear != term.Year || schedule.Semester != term.Semester {
		return nil, fmt.Errorf("%w: schedule object does not match its governed term", ErrObjectUnavailable)
	}
	schedule.AcademicTermID = term.ID
	schedule.TermStatus = term.Status
	schedule.TermVersion = ref.Version
	if ref.FirstWeekStart != "" {
		schedule.FirstWeekStart = ref.FirstWeekStart
	}
	return schedule, nil
}

func (s *Service) loadTermObject(ctx context.Context, userID, objectID string) (*Schedule, error) {
	reader, err := s.objects.Open(ctx, userID, objectID)
	if err != nil {
		return nil, err
	}
	defer reader.Reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader.Reader, s.cfg.MaxImportBytes+1))
	if err != nil || int64(len(raw)) > s.cfg.MaxImportBytes {
		return nil, ErrObjectUnavailable
	}
	var schedule Schedule
	if err := json.Unmarshal(raw, &schedule); err != nil {
		return nil, ErrObjectUnavailable
	}
	if schedule.UserID == "" {
		schedule.UserID = userID
	}
	if err := s.normalize(&schedule); err != nil {
		return nil, ErrObjectUnavailable
	}
	return &schedule, nil
}

func (s *Service) listManagedTerms(ctx context.Context, userID string) (*TermsResponse, error) {
	refs, err := s.termReferences.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	preference, preferenceErr := s.termReferences.Preference(ctx, userID)
	if preferenceErr != nil && !errors.Is(preferenceErr, ErrTermReferenceNotFound) {
		return nil, preferenceErr
	}
	items := make([]TermSummary, 0, len(refs))
	for _, ref := range refs {
		term, termErr := s.academicTerms.Get(ctx, ref.AcademicTermID)
		if termErr != nil {
			return nil, termErr
		}
		schedule, scheduleErr := s.loadReferencedTerm(ctx, userID, term, ref)
		if scheduleErr != nil {
			return nil, scheduleErr
		}
		items = append(items, TermSummary{
			AcademicTermID: term.ID, TermStatus: term.Status, Version: ref.Version,
			TermYear: term.Year, Semester: term.Semester, FirstWeekStart: schedule.FirstWeekStart,
			CourseCount: len(schedule.Courses), UpdatedAt: schedule.UpdatedAt, Active: preference == term.ID,
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
				return s.emptyScheduleForTerm(userID, termYear, semester), nil
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

func (s *Service) writeTerm(ctx context.Context, schedule *Schedule, expectedVersion int64) error {
	path, err := s.termSchedulePath(schedule.UserID, schedule.TermYear, schedule.Semester)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(schedule, "", "  ")
	if err != nil {
		return err
	}
	if _, err := s.storage.EnsureLayout(schedule.UserID); err != nil {
		return err
	}
	managed := s.objects != nil && s.termReferences != nil && strings.TrimSpace(schedule.AcademicTermID) != ""
	if !managed {
		return s.writeCompatibilityFile(ctx, schedule.UserID, path, raw)
	}
	if s.compatibility == nil {
		return fmt.Errorf("%w: quota compatibility ledger is unavailable", ErrObjectUnavailable)
	}
	previousSize, err := compatibilityFileSize(path)
	if err != nil {
		return err
	}
	// Reserve the exact delta of the retained JSON copy before reserving the
	// immutable Object. This keeps the ledger equal to physical bytes without a
	// per-save directory walk. The compatibility copy itself is written only
	// after the Object binding switches, so a stale concurrent writer cannot
	// overwrite a successful writer's readable fallback.
	if err := s.compatibility.ReplaceCompatibility(ctx, schedule.UserID, previousSize, int64(len(raw))); err != nil {
		return err
	}
	ledgerAdjusted := true
	defer func() {
		if ledgerAdjusted {
			_ = s.compatibility.ReplaceCompatibility(context.Background(), schedule.UserID, int64(len(raw)), previousSize)
		}
	}()
	object, err := s.objects.Put(ctx, schedule.UserID, corestorage.PutRequest{
		Namespace: "schedule", Purpose: "term-json", OriginalName: termKey(schedule.TermYear, schedule.Semester) + ".json",
		MimeType: "application/json", SizeHint: int64(len(raw)), Reader: bytes.NewReader(raw),
	})
	if err != nil {
		return err
	}
	ref, err := s.termReferences.Switch(ctx, TermReference{
		UserID: schedule.UserID, AcademicTermID: schedule.AcademicTermID,
		CurrentObjectID: object.ID, FirstWeekStart: schedule.FirstWeekStart,
	}, expectedVersion)
	if err != nil {
		_ = s.objects.Delete(context.Background(), schedule.UserID, object.ID, object.Version)
		return err
	}
	schedule.TermVersion = ref.Version
	if err := writeFileAtomically(path, raw, 0o644); err != nil {
		// The immutable Object/reference are already authoritative. Restore only
		// the ledger delta (the old compatibility file remains in place) and let
		// the next successful save or reconciliation refresh the mirror.
		return err
	}
	ledgerAdjusted = false
	return nil
}

// writeCompatibilityFile is limited to v14's retained JSON/index mirrors.
// Managed installations charge only the size delta to the persistent ledger;
// isolated legacy tests retain the earlier adapter quota check.
func (s *Service) writeCompatibilityFile(ctx context.Context, userID, path string, raw []byte) error {
	previousSize, err := compatibilityFileSize(path)
	if err != nil {
		return err
	}
	if s.compatibility == nil {
		if err := s.checkQuota(userID, int64(len(raw)), path); err != nil {
			return err
		}
		return writeFileAtomically(path, raw, 0o644)
	}
	if err := s.compatibility.ReplaceCompatibility(ctx, userID, previousSize, int64(len(raw))); err != nil {
		return err
	}
	if err := writeFileAtomically(path, raw, 0o644); err != nil {
		_ = s.compatibility.ReplaceCompatibility(context.Background(), userID, int64(len(raw)), previousSize)
		return err
	}
	return nil
}

func compatibilityFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("%w: schedule compatibility path is a directory", ErrInvalidInput)
	}
	return info.Size(), nil
}

func writeFileAtomically(path string, raw []byte, permission os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".schedule-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if _, err := temporary.Write(raw); err != nil {
		return err
	}
	if err := temporary.Chmod(permission); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	closed = true
	return os.Rename(temporaryPath, path)
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

func (s *Service) defaultSchedule(ctx context.Context, userID string) (*Schedule, error) {
	if s.academicTerms != nil {
		term, err := s.academicTerms.DefaultOpen(ctx)
		if err != nil {
			return nil, err
		}
		schedule := s.defaultScheduleWithFirstWeek(userID, term.Year, term.Semester, term.FirstWeekStart)
		schedule.AcademicTermID, schedule.TermStatus, schedule.TermVersion = term.ID, term.Status, 0
		return schedule, nil
	}
	return s.defaultScheduleForTerm(userID, defaultTermYear(s.now()), defaultTermSemester(s.now())), nil
}

func (s *Service) defaultScheduleForTerm(userID string, termYear int, semester string) *Schedule {
	return s.defaultScheduleWithFirstWeek(userID, termYear, semester, mondayOf(s.now()).Format(dateLayout))
}

func (s *Service) emptyScheduleForTerm(userID string, termYear int, semester string) *Schedule {
	return s.defaultScheduleWithFirstWeek(userID, termYear, semester, "")
}

func (s *Service) defaultScheduleWithFirstWeek(userID string, termYear int, semester, firstWeekStart string) *Schedule {
	return &Schedule{
		UserID:         userID,
		TermYear:       termYear,
		Semester:       semester,
		FirstWeekStart: firstWeekStart,
		Settings: Settings{
			PeriodsPerDay: 12,
			ShowWeekend:   true,
		},
		Courses:   []Course{},
		UpdatedAt: s.now().UTC(),
	}
}

func termID(term *academicterm.Term) string {
	if term == nil {
		return ""
	}
	return term.ID
}

func termStatus(term *academicterm.Term) string {
	if term == nil {
		return ""
	}
	return term.Status
}

func (s *Service) resolveWritableTerm(ctx context.Context, termYear int, semester string) (int, string, *academicterm.Term, error) {
	if s.academicTerms == nil {
		year, normalized, err := s.normalizeTerm(termYear, semester)
		return year, normalized, nil, err
	}
	if termYear == 0 && strings.TrimSpace(semester) == "" {
		term, err := s.academicTerms.DefaultOpen(ctx)
		if err != nil {
			return 0, "", nil, err
		}
		return term.Year, term.Semester, &term, nil
	}
	if termYear == 0 || strings.TrimSpace(semester) == "" {
		return 0, "", nil, fmt.Errorf("%w: term_year and semester must be provided together", ErrInvalidInput)
	}
	year, normalized, err := s.normalizeTerm(termYear, semester)
	if err != nil {
		return 0, "", nil, err
	}
	term, err := s.academicTerms.FindOpen(ctx, year, normalized)
	if err != nil {
		return 0, "", nil, err
	}
	return year, normalized, &term, nil
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

func (s *Service) writeActiveTerm(ctx context.Context, schedule *Schedule) error {
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
	if _, err := s.storage.EnsureLayout(schedule.UserID); err != nil {
		return err
	}
	return s.writeCompatibilityFile(ctx, schedule.UserID, path, raw)
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
		// Keep the original legacy JSON untouched. v14 adoption is additive: a
		// recovery or later administrator-approved adoption must still be able to
		// verify the original bytes and hash.
		if _, err := s.storage.EnsureLayout(userID); err != nil {
			return err
		}
		if err := s.writeCompatibilityFile(ctx, userID, termPath, migrated); err != nil {
			return err
		}
	} else if err != nil {
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
	quotaBytes := s.cfg.QuotaBytes
	if quota, ok := s.storage.(corestorage.Quota); ok {
		quotaBytes = quota.QuotaBytes(userID)
	}
	if usage+newSize > quotaBytes {
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
