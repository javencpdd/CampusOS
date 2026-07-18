package secondhand

import (
	"errors"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/contentbody"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

const (
	ModuleID              = "feature.secondhand"
	FeatureID             = "secondhand"
	ContentFormat         = contentbody.FormatPlainText
	ContentFormatSafeHTML = contentbody.FormatSafeHTML
	maxLocationLen        = 160
	currencyCNY           = "CNY"
)

type ItemCondition string

const (
	ItemConditionNew     ItemCondition = "new"
	ItemConditionLikeNew ItemCondition = "like_new"
	ItemConditionGood    ItemCondition = "good"
	ItemConditionFair    ItemCondition = "fair"
)

type TradeMethod string

const (
	TradeMethodInPerson      TradeMethod = "in_person"
	TradeMethodCampusDropoff TradeMethod = "campus_dropoff"
	TradeMethodOther         TradeMethod = "other"
)

type TradeStatus string

const (
	TradeStatusAvailable TradeStatus = "available"
	TradeStatusReserved  TradeStatus = "reserved"
	TradeStatusSold      TradeStatus = "sold"
	TradeStatusClosed    TradeStatus = "closed"
)

var (
	ErrFeatureDisabled   = errors.New("secondhand feature is disabled")
	ErrNotFound          = errors.New("secondhand thread not found")
	ErrForbidden         = errors.New("secondhand thread belongs to another user")
	ErrInvalidInput      = errors.New("secondhand request is invalid")
	ErrVersionConflict   = errors.New("secondhand detail version conflict")
	ErrInvalidTransition = errors.New("secondhand status transition is invalid")
	ErrThreadNotEditable = errors.New("secondhand thread is no longer editable")
)

type Detail struct {
	ThreadID      string        `json:"thread_id"`
	PriceMinor    int64         `json:"price_minor"`
	Currency      string        `json:"currency"`
	ItemCondition ItemCondition `json:"item_condition"`
	TradeMethod   TradeMethod   `json:"trade_method"`
	TradeStatus   TradeStatus   `json:"trade_status"`
	LocationScope string        `json:"location_scope,omitempty"`
	Version       int64         `json:"version"`
	CreatedBy     string        `json:"created_by"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type CreateRequest struct {
	Title         string        `json:"title" binding:"required,min=1,max=255"`
	Content       string        `json:"content" binding:"required,min=1"`
	ContentFormat string        `json:"content_format,omitempty"`
	CategoryID    string        `json:"category_id" binding:"required"`
	Tags          []string      `json:"tags,omitempty"`
	PriceMinor    int64         `json:"price_minor" binding:"min=0"`
	Currency      string        `json:"currency,omitempty"`
	ItemCondition ItemCondition `json:"item_condition" binding:"required"`
	TradeMethod   TradeMethod   `json:"trade_method" binding:"required"`
	LocationScope string        `json:"location_scope,omitempty" binding:"max=160"`
}

// UpdateRequest is a complete editable projection. Trade state transitions
// have their own endpoint and cannot be smuggled through an edit request.
type UpdateRequest struct {
	Title         string        `json:"title" binding:"required,min=1,max=255"`
	Content       string        `json:"content" binding:"required,min=1"`
	ContentFormat string        `json:"content_format,omitempty"`
	Tags          []string      `json:"tags,omitempty"`
	PriceMinor    int64         `json:"price_minor" binding:"min=0"`
	Currency      string        `json:"currency,omitempty"`
	ItemCondition ItemCondition `json:"item_condition" binding:"required"`
	TradeMethod   TradeMethod   `json:"trade_method" binding:"required"`
	LocationScope string        `json:"location_scope,omitempty" binding:"max=160"`
	Version       int64         `json:"version" binding:"required,min=1"`
}

type UpdateStatusRequest struct {
	TradeStatus TradeStatus `json:"trade_status" binding:"required"`
	Version     int64       `json:"version" binding:"required,min=1"`
}

type Result struct {
	Thread *domain.Thread `json:"thread"`
	Detail *Detail        `json:"detail"`
}

type StatusResult struct {
	Enabled bool `json:"enabled"`
}

func normalizeCondition(value ItemCondition) ItemCondition {
	return ItemCondition(strings.TrimSpace(strings.ToLower(string(value))))
}

func normalizeMethod(value TradeMethod) TradeMethod {
	return TradeMethod(strings.TrimSpace(strings.ToLower(string(value))))
}

func normalizeStatus(value TradeStatus) TradeStatus {
	return TradeStatus(strings.TrimSpace(strings.ToLower(string(value))))
}

func normalizeCurrency(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return currencyCNY
	}
	return value
}

func validCondition(value ItemCondition) bool {
	switch normalizeCondition(value) {
	case ItemConditionNew, ItemConditionLikeNew, ItemConditionGood, ItemConditionFair:
		return true
	default:
		return false
	}
}

func validMethod(value TradeMethod) bool {
	switch normalizeMethod(value) {
	case TradeMethodInPerson, TradeMethodCampusDropoff, TradeMethodOther:
		return true
	default:
		return false
	}
}

func validStatus(value TradeStatus) bool {
	switch normalizeStatus(value) {
	case TradeStatusAvailable, TradeStatusReserved, TradeStatusSold, TradeStatusClosed:
		return true
	default:
		return false
	}
}
