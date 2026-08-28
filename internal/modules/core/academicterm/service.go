package academicterm

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/apperror"
	"github.com/campusos/CampusOS/pkg/idgen"
)

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("academic term repository is required")
	}
	return &Service{repository: repository, now: time.Now}, nil
}

func (s *Service) ListOpen(ctx context.Context) ([]Term, error) {
	return s.repository.List(ctx, ListFilter{Status: StatusOpen})
}

func (s *Service) ListAll(ctx context.Context) ([]Term, error) {
	return s.repository.List(ctx, ListFilter{})
}

// FindOpen resolves a user-selectable academic term by its stable business
// key. Features use this instead of inferring a term from the server clock.
func (s *Service) FindOpen(ctx context.Context, year int, semester string) (Term, error) {
	semester = normalizeSemester(semester)
	if year < 2000 || year > 2200 || (semester != SemesterSpring && semester != SemesterFall) {
		return Term{}, publicError(ErrInvalid, apperror.AcademicTermInvalid, map[string]any{"field": "year_or_semester"})
	}
	items, err := s.ListAll(ctx)
	if err != nil {
		return Term{}, err
	}
	for _, item := range items {
		if item.Year == year && item.Semester == semester {
			if item.Status != StatusOpen {
				return Term{}, publicError(ErrClosed, apperror.AcademicTermClosed, termDetails(item))
			}
			return item, nil
		}
	}
	return Term{}, publicError(ErrNotFound, apperror.AcademicTermNotAvailable, map[string]any{"year": year, "semester": semester})
}

func (s *Service) GetAvailable(ctx context.Context, id string) (Term, error) {
	item, err := s.repository.Get(ctx, strings.TrimSpace(id))
	if errors.Is(err, ErrNotFound) {
		return Term{}, publicError(ErrNotFound, apperror.AcademicTermNotAvailable, nil)
	}
	if err != nil {
		return Term{}, err
	}
	if item.Status != StatusOpen {
		return Term{}, publicError(ErrClosed, apperror.AcademicTermClosed, termDetails(item))
	}
	return item, nil
}

func (s *Service) DefaultOpen(ctx context.Context) (Term, error) {
	items, err := s.ListOpen(ctx)
	if err != nil {
		return Term{}, err
	}
	for _, item := range items {
		if item.IsDefault {
			return item, nil
		}
	}
	return Term{}, publicError(ErrDefaultUnavailable, apperror.AcademicTermDefaultUnavailable, nil)
}

func (s *Service) Create(ctx context.Context, actorID string, req CreateRequest) (Term, error) {
	actorID = strings.TrimSpace(actorID)
	req.Semester = normalizeSemester(req.Semester)
	req.Status = normalizeStatus(req.Status)
	if actorID == "" || req.Year < 2000 || req.Year > 2200 ||
		req.Semester == "" ||
		(req.Semester != SemesterSpring && req.Semester != SemesterFall) ||
		req.Status == "" ||
		(req.Status != StatusOpen && req.Status != StatusClosed) ||
		validateReason(req.Reason) != nil {
		return Term{}, publicError(ErrInvalid, apperror.AcademicTermInvalid, map[string]any{"field": "request"})
	}
	if req.Status == StatusClosed && req.IsDefault {
		return Term{}, publicError(ErrInvalid, apperror.AcademicTermInvalid, map[string]any{"field": "is_default", "reason": "关闭学期不能设为默认"})
	}
	firstWeek, err := parseMonday(req.FirstWeekStart)
	if err != nil {
		return Term{}, publicError(err, apperror.AcademicTermInvalid, map[string]any{"field": "first_week_start", "reason": "第一周开始日期必须是星期一（YYYY-MM-DD）"})
	}
	now := s.now().UTC()
	item := Term{
		ID:             strconv.FormatInt(idgen.New(), 10),
		Year:           req.Year,
		Semester:       req.Semester,
		FirstWeekStart: firstWeek.Format("2006-01-02"),
		Status:         req.Status,
		IsDefault:      req.IsDefault,
		Version:        1,
		CreatedBy:      actorID,
		UpdatedBy:      actorID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if req.Status == StatusClosed {
		item.ClosedAt = &now
	}
	result, err := s.repository.Create(ctx, item)
	return result, translate(err, nil)
}

func (s *Service) UpdateFirstWeek(ctx context.Context, actorID, id string, req UpdateRequest) (Term, error) {
	if err := validateTransition(actorID, req.ExpectedVersion, req.Reason); err != nil {
		return Term{}, err
	}
	firstWeek, err := parseMonday(req.FirstWeekStart)
	if err != nil {
		return Term{}, publicError(err, apperror.AcademicTermInvalid, map[string]any{"field": "first_week_start", "reason": "第一周开始日期必须是星期一（YYYY-MM-DD）"})
	}
	item, err := s.repository.UpdateFirstWeek(ctx, strings.TrimSpace(id), req.ExpectedVersion, firstWeek, strings.TrimSpace(actorID))
	return item, translate(err, nil)
}

func (s *Service) Close(ctx context.Context, actorID, id string, req TransitionRequest) (Term, error) {
	if err := validateTransition(actorID, req.ExpectedVersion, req.Reason); err != nil {
		return Term{}, err
	}
	item, err := s.repository.Close(ctx, strings.TrimSpace(id), req.ExpectedVersion, strings.TrimSpace(actorID))
	return item, translate(err, nil)
}

func (s *Service) Open(ctx context.Context, actorID, id string, req TransitionRequest) (Term, error) {
	if err := validateTransition(actorID, req.ExpectedVersion, req.Reason); err != nil {
		return Term{}, err
	}
	item, err := s.repository.Open(ctx, strings.TrimSpace(id), req.ExpectedVersion, strings.TrimSpace(actorID))
	return item, translate(err, nil)
}

func (s *Service) SetDefault(ctx context.Context, actorID, id string, req TransitionRequest) (Term, error) {
	if err := validateTransition(actorID, req.ExpectedVersion, req.Reason); err != nil {
		return Term{}, err
	}
	item, err := s.repository.SetDefault(ctx, strings.TrimSpace(id), req.ExpectedVersion, strings.TrimSpace(actorID))
	return item, translate(err, nil)
}

func (s *Service) Delete(ctx context.Context, actorID, id string, req TransitionRequest) error {
	if err := validateTransition(actorID, req.ExpectedVersion, req.Reason); err != nil {
		return err
	}
	return translate(s.repository.Delete(ctx, strings.TrimSpace(id), req.ExpectedVersion), nil)
}

func validateTransition(actorID string, expectedVersion int64, reason string) error {
	if strings.TrimSpace(actorID) == "" || expectedVersion < 1 || validateReason(reason) != nil {
		return publicError(ErrInvalid, apperror.AcademicTermInvalid, map[string]any{"field": "expected_version_or_reason"})
	}
	return nil
}

func translate(err error, details map[string]any) error {
	if err == nil {
		return nil
	}
	var public *apperror.AppError
	if errors.As(err, &public) {
		return err
	}
	switch {
	case errors.Is(err, ErrInvalid):
		return publicError(err, apperror.AcademicTermInvalid, details)
	case errors.Is(err, ErrNotFound):
		return publicError(err, apperror.AcademicTermNotAvailable, details)
	case errors.Is(err, ErrAlreadyExists):
		return publicError(err, apperror.AcademicTermAlreadyExists, details)
	case errors.Is(err, ErrClosed):
		return publicError(err, apperror.AcademicTermClosed, details)
	case errors.Is(err, ErrDefaultUnavailable):
		return publicError(err, apperror.AcademicTermDefaultUnavailable, details)
	case errors.Is(err, ErrVersionConflict):
		return publicError(err, apperror.AcademicTermVersionConflict, details)
	case errors.Is(err, ErrHasSchedules):
		return publicError(err, apperror.AcademicTermHasSchedules, details)
	default:
		return err
	}
}

func publicError(cause error, descriptor apperror.Descriptor, details map[string]any) error {
	return apperror.Wrap(cause, descriptor, details)
}

func termDetails(item Term) map[string]any {
	return map[string]any{"academic_term_id": item.ID, "display_name": item.DisplayName, "status": item.Status}
}
