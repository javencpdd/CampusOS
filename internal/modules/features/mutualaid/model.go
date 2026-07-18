package mutualaid

import (
	"errors"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/contentbody"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

const (
	ModuleID              = "feature.mutual-aid"
	FeatureID             = "mutual-aid"
	ContentFormat         = contentbody.FormatPlainText
	ContentFormatSafeHTML = contentbody.FormatSafeHTML
	maxLocationLen        = 160
)

type AidType string

const (
	AidTypeRequest       AidType = "request"
	AidTypeOffer         AidType = "offer"
	AidTypeVolunteer     AidType = "volunteer"
	AidTypeResourceShare AidType = "resource_share"
)

type AidStatus string

const (
	AidStatusOpen       AidStatus = "open"
	AidStatusInProgress AidStatus = "in_progress"
	AidStatusResolved   AidStatus = "resolved"
	AidStatusClosed     AidStatus = "closed"
)

type ContactMode string

const (
	ContactModeComment ContactMode = "comment"
	ContactModeInApp   ContactMode = "in_app"
	ContactModeEmail   ContactMode = "email"
	ContactModeOther   ContactMode = "other"
)

var (
	ErrFeatureDisabled   = errors.New("mutual aid feature is disabled")
	ErrNotFound          = errors.New("mutual aid thread not found")
	ErrForbidden         = errors.New("mutual aid thread belongs to another user")
	ErrInvalidInput      = errors.New("mutual aid request is invalid")
	ErrVersionConflict   = errors.New("mutual aid detail version conflict")
	ErrInvalidTransition = errors.New("mutual aid status transition is invalid")
	ErrThreadNotEditable = errors.New("mutual aid thread is no longer editable")
)

type Detail struct {
	ThreadID      string      `json:"thread_id"`
	AidType       AidType     `json:"aid_type"`
	AidStatus     AidStatus   `json:"aid_status"`
	Deadline      *time.Time  `json:"deadline,omitempty"`
	LocationScope string      `json:"location_scope,omitempty"`
	ContactMode   ContactMode `json:"contact_mode"`
	Version       int64       `json:"version"`
	CreatedBy     string      `json:"created_by"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

type CreateRequest struct {
	Title         string      `json:"title" binding:"required,min=1,max=255"`
	Content       string      `json:"content" binding:"required,min=1"`
	ContentFormat string      `json:"content_format,omitempty"`
	CategoryID    string      `json:"category_id" binding:"required"`
	Tags          []string    `json:"tags,omitempty"`
	AidType       AidType     `json:"aid_type" binding:"required"`
	Deadline      *time.Time  `json:"deadline,omitempty"`
	LocationScope string      `json:"location_scope,omitempty" binding:"max=160"`
	ContactMode   ContactMode `json:"contact_mode" binding:"required"`
}

// UpdateRequest is intentionally a complete editable projection. Business
// state changes have a separate endpoint and cannot be smuggled through it.
type UpdateRequest struct {
	Title         string      `json:"title" binding:"required,min=1,max=255"`
	Content       string      `json:"content" binding:"required,min=1"`
	ContentFormat string      `json:"content_format,omitempty"`
	Tags          []string    `json:"tags,omitempty"`
	AidType       AidType     `json:"aid_type" binding:"required"`
	Deadline      *time.Time  `json:"deadline,omitempty"`
	LocationScope string      `json:"location_scope,omitempty" binding:"max=160"`
	ContactMode   ContactMode `json:"contact_mode" binding:"required"`
	Version       int64       `json:"version" binding:"required,min=1"`
}

type UpdateStatusRequest struct {
	AidStatus AidStatus `json:"aid_status" binding:"required"`
	Version   int64     `json:"version" binding:"required,min=1"`
}

type Result struct {
	Thread *domain.Thread `json:"thread"`
	Detail *Detail        `json:"detail"`
}

type StatusResult struct {
	Enabled bool `json:"enabled"`
}

func normalizeAidType(value AidType) AidType {
	return AidType(strings.TrimSpace(strings.ToLower(string(value))))
}

func normalizeAidStatus(value AidStatus) AidStatus {
	return AidStatus(strings.TrimSpace(strings.ToLower(string(value))))
}

func normalizeContactMode(value ContactMode) ContactMode {
	return ContactMode(strings.TrimSpace(strings.ToLower(string(value))))
}

func validAidType(value AidType) bool {
	switch normalizeAidType(value) {
	case AidTypeRequest, AidTypeOffer, AidTypeVolunteer, AidTypeResourceShare:
		return true
	default:
		return false
	}
}

func validAidStatus(value AidStatus) bool {
	switch normalizeAidStatus(value) {
	case AidStatusOpen, AidStatusInProgress, AidStatusResolved, AidStatusClosed:
		return true
	default:
		return false
	}
}

func validContactMode(value ContactMode) bool {
	switch normalizeContactMode(value) {
	case ContactModeComment, ContactModeInApp, ContactModeEmail, ContactModeOther:
		return true
	default:
		return false
	}
}
