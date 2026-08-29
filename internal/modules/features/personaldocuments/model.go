// Package personaldocuments owns private, immutable user document versions.
package personaldocuments

import (
	"context"
	"time"
)

const (
	ModuleID        = "feature.personal-documents"
	FeatureID       = "personal-documents"
	StatusActive    = "active"
	StatusTrashed   = "trashed"
	FormatText      = "text"
	FormatMarkdown  = "markdown"
	FormatCampusDoc = "campusdoc"
	FormatPDF       = "pdf"
	FormatDOCX      = "docx"
)

type Document struct {
	ID               string     `json:"id"`
	OwnerID          string     `json:"owner_id"`
	Name             string     `json:"name"`
	Format           string     `json:"format"`
	Status           string     `json:"status"`
	CurrentVersionID string     `json:"current_version_id"`
	Version          int64      `json:"version"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

type DocumentVersion struct {
	ID                    string    `json:"id"`
	DocumentID            string    `json:"document_id"`
	VersionNumber         int       `json:"version_number"`
	SourceObjectID        string    `json:"source_object_id"`
	Format                string    `json:"format"`
	SizeBytes             int64     `json:"size_bytes"`
	SHA256                string    `json:"sha256"`
	RestoredFromVersionID string    `json:"restored_from_version_id,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
}

type DocumentDetail struct {
	Document
	CurrentVersion *DocumentVersion `json:"current_version,omitempty"`
}

type CreateRequest struct {
	Name    string `json:"name"`
	Format  string `json:"format"`
	Content string `json:"content"`
}
type SaveRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Name            string `json:"name"`
	Content         string `json:"content"`
}
type VersionRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}
type ListFilter struct{ Status string }

// PreviewStatus is intentionally a bounded capability result. v0.14 never
// invokes a document converter inside the API process: binary formats report
// converter_unavailable until an independently deployed, constrained runner
// is proven available.
type PreviewStatus struct {
	Document          DocumentDetail `json:"document"`
	Status            string         `json:"status"`
	DownloadAvailable bool           `json:"download_available"`
	Message           string         `json:"message"`
	RenderedHTML      string         `json:"rendered_html,omitempty"`
	Warnings          []string       `json:"warnings,omitempty"`
}

// PreviewMetricKey is an aggregate-only lifecycle bucket for the optional
// converter queue. It intentionally contains no document, owner, or filename.
type PreviewMetricKey struct {
	Status string
	Format string
}

type Repository interface {
	Create(context.Context, Document, DocumentVersion) (DocumentDetail, error)
	List(context.Context, string, ListFilter) ([]DocumentDetail, error)
	Get(context.Context, string, string) (DocumentDetail, error)
	Versions(context.Context, string, string) ([]DocumentVersion, error)
	AppendVersion(context.Context, string, string, int64, DocumentVersion, string) (DocumentDetail, error)
	SetStatus(context.Context, string, string, int64, string) (DocumentDetail, error)
	Version(context.Context, string, string, string) (DocumentVersion, error)
}
