package storage

import (
	"context"
	"io"
	"time"
)

const (
	ObjectStatusPending     = "pending"
	ObjectStatusReady       = "ready"
	ObjectStatusDeleting    = "deleting"
	ObjectStatusDeleted     = "deleted"
	ObjectStatusQuarantined = "quarantined"
	ObjectStatusMissing     = "missing"
	ObjectPageDefaultLimit  = 20
	ObjectPageMaximumLimit  = 100
)

// Object is the safe DTO shared with built-in features. Storage keys and
// absolute paths are deliberately absent: only the Local Provider interprets
// those internal implementation details.
type Object struct {
	ID           string     `json:"id"`
	OwnerID      string     `json:"owner_id"`
	Namespace    string     `json:"namespace"`
	Purpose      string     `json:"purpose"`
	OriginalName string     `json:"original_name"`
	MimeType     string     `json:"mime_type"`
	SizeBytes    int64      `json:"size_bytes"`
	SHA256       string     `json:"sha256"`
	Status       string     `json:"status"`
	Version      int64      `json:"version"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

type PutRequest struct {
	Namespace    string
	Purpose      string
	OriginalName string
	MimeType     string
	SizeHint     int64
	Reader       io.Reader
}

type ObjectFilter struct {
	Namespace      string
	Purpose        string
	IncludeDeleted bool
}

type PageRequest struct {
	Limit  int
	Cursor string
}

type ObjectPage struct {
	Items      []Object `json:"items"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

type ObjectReader struct {
	Reader io.ReadCloser
	Object Object
}

// ObjectUsage is a safe, owner-scoped capacity summary. It exposes byte
// counts only; it never turns an Object Port into a directory or metadata
// enumeration API.
type ObjectUsage struct {
	UsedBytes      int64 `json:"used_bytes"`
	QuotaBytes     int64 `json:"quota_bytes"`
	RemainingBytes int64 `json:"remaining_bytes"`
}

// ObjectPort is the only file boundary for new user-owned features. It is
// intentionally streaming and owner-aware; callers never receive host paths.
type ObjectPort interface {
	Put(context.Context, string, PutRequest) (Object, error)
	Open(context.Context, string, string) (ObjectReader, error)
	Stat(context.Context, string, string) (Object, error)
	Delete(context.Context, string, string, int64) error
	List(context.Context, string, ObjectFilter, PageRequest) (ObjectPage, error)
}

// CompatibilityLedger is intentionally narrower than ObjectPort. It exists
// only while built-in features keep a readable compatibility copy beside
// immutable Objects. Callers supply byte counts, never a filesystem path;
// the provider updates the same durable quota ledger used by ObjectPort.
// New feature data must not use this as a general file writer.
type CompatibilityLedger interface {
	ReplaceCompatibility(context.Context, string, int64, int64) error
}
