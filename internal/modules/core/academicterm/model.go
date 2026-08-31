// Package academicterm owns the school-level, administrator-governed term
// catalog. It intentionally does not depend on the Schedule feature so other
// built-in features can consume the same directory fact later.
package academicterm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const ModuleID = "core.academic-term"

const (
	SemesterSpring = "spring"
	SemesterFall   = "fall"
	StatusOpen     = "open"
	StatusClosed   = "closed"
)

var (
	ErrInvalid            = errors.New("academic term is invalid")
	ErrNotFound           = errors.New("academic term not found")
	ErrAlreadyExists      = errors.New("academic term already exists")
	ErrClosed             = errors.New("academic term is closed")
	ErrDefaultUnavailable = errors.New("academic term default is unavailable")
	ErrVersionConflict    = errors.New("academic term version conflict")
	ErrHasSchedules       = errors.New("academic term has schedules")
)

type Term struct {
	ID             string     `json:"id"`
	Year           int        `json:"year"`
	Semester       string     `json:"semester"`
	DisplayName    string     `json:"display_name"`
	FirstWeekStart string     `json:"first_week_start"`
	Status         string     `json:"status"`
	IsDefault      bool       `json:"is_default"`
	Version        int64      `json:"version"`
	CreatedBy      string     `json:"created_by,omitempty"`
	UpdatedBy      string     `json:"updated_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
	// ScheduleReferenceCount is an administrator-only operational projection.
	// It is never used for authorization: PostgreSQL foreign keys remain the
	// source of truth that prevents deletion of an in-use term.
	ScheduleReferenceCount int64 `json:"schedule_reference_count,omitempty"`
}

type CreateRequest struct {
	Year           int    `json:"year"`
	Semester       string `json:"semester"`
	FirstWeekStart string `json:"first_week_start"`
	Status         string `json:"status"`
	IsDefault      bool   `json:"is_default"`
	Reason         string `json:"reason"`
}

type UpdateRequest struct {
	FirstWeekStart  string `json:"first_week_start"`
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

type TransitionRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

type ListFilter struct {
	Status string
}

// Port is the stable read boundary for built-in features. Management commands
// stay on Service so a feature cannot bypass administrator lifecycle rules.
type Port interface {
	// Get returns a directory fact regardless of lifecycle state.  It is a
	// read-only operation used by owner-scoped historical consumers (for
	// example, a user reading their already-created closed-term schedule).
	// Write paths must continue to use one of the Open methods below.
	Get(context.Context, string) (Term, error)
	ListOpen(context.Context) ([]Term, error)
	FindOpen(context.Context, int, string) (Term, error)
	GetAvailable(context.Context, string) (Term, error)
	DefaultOpen(context.Context) (Term, error)
}

func (t Term) withDerivedFields() Term {
	t.Semester = normalizeSemester(t.Semester)
	t.Status = strings.ToLower(strings.TrimSpace(t.Status))
	t.DisplayName = displayName(t.Year, t.Semester)
	return t
}

func displayName(year int, semester string) string {
	if semester == SemesterSpring {
		return fmt.Sprintf("%d 春季学期", year)
	}
	return fmt.Sprintf("%d 秋季学期", year)
}

func normalizeSemester(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeStatus(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func parseMonday(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil || parsed.Weekday() != time.Monday {
		return time.Time{}, ErrInvalid
	}
	return parsed, nil
}

func validateReason(value string) error {
	if len([]rune(strings.TrimSpace(value))) < 2 || len([]rune(value)) > 500 {
		return ErrInvalid
	}
	return nil
}
