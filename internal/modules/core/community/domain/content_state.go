package domain

import "time"

// PublicationStatus describes the author's intended visibility. It is kept
// separate from moderation and deletion so an action in one dimension cannot
// accidentally restore another one.
type PublicationStatus string

const (
	PublicationStatusDraft     PublicationStatus = "draft"
	PublicationStatusPublished PublicationStatus = "published"
	PublicationStatusPrivate   PublicationStatus = "private"
)

// ModerationStatus describes the current governance decision for content.
type ModerationStatus string

const (
	ModerationStatusClear     ModerationStatus = "clear"
	ModerationStatusPending   ModerationStatus = "pending"
	ModerationStatusRejected  ModerationStatus = "rejected"
	ModerationStatusTakenDown ModerationStatus = "taken_down"
)

// DeletionStatus provides a recoverable trash state before a privileged
// permanent purge. Existing deleted_at data is treated as purged by readers.
type DeletionStatus string

const (
	DeletionStatusActive  DeletionStatus = "active"
	DeletionStatusTrashed DeletionStatus = "trashed"
	DeletionStatusPurged  DeletionStatus = "purged"
)

func (t *Thread) publication() PublicationStatus {
	if t.PublicationStatus != "" {
		return t.PublicationStatus
	}
	switch t.Status {
	case ThreadStatusDraft:
		return PublicationStatusDraft
	case ThreadStatusPrivate:
		return PublicationStatusPrivate
	default:
		return PublicationStatusPublished
	}
}

func (t *Thread) moderation() ModerationStatus {
	if t.ModerationStatus != "" {
		return t.ModerationStatus
	}
	switch t.Status {
	case ThreadStatusPendingReview:
		return ModerationStatusPending
	case ThreadStatusArchived:
		return ModerationStatusTakenDown
	default:
		return ModerationStatusClear
	}
}

func (t *Thread) deletion() DeletionStatus {
	if t.DeletionStatus != "" {
		return t.DeletionStatus
	}
	return DeletionStatusActive
}

// NormalizeContentState materializes compatibility defaults before a thread
// is persisted or returned by a legacy adapter.
func (t *Thread) NormalizeContentState() {
	if t == nil {
		return
	}
	t.ThreadType = NormalizeThreadType(t.ThreadType)
	t.PublicationStatus = t.publication()
	t.ModerationStatus = t.moderation()
	t.DeletionStatus = t.deletion()
	t.SyncLegacyStatus()
}

// SyncLegacyStatus keeps the v0.9 status field useful to existing clients
// during the v10 migration. New reads must use IsPublic and state fields.
func (t *Thread) SyncLegacyStatus() {
	if t == nil {
		return
	}
	if t.deletion() != DeletionStatusActive {
		t.Status = ThreadStatusArchived
		return
	}
	switch t.moderation() {
	case ModerationStatusPending:
		t.Status = ThreadStatusPendingReview
		return
	case ModerationStatusRejected, ModerationStatusTakenDown:
		t.Status = ThreadStatusArchived
		return
	}
	switch t.publication() {
	case PublicationStatusDraft:
		t.Status = ThreadStatusDraft
	case PublicationStatusPrivate:
		t.Status = ThreadStatusPrivate
	default:
		t.Status = ThreadStatusPublished
	}
}

func (t *Thread) IsPublic() bool {
	return t != nil && t.publication() == PublicationStatusPublished &&
		t.moderation() == ModerationStatusClear && t.deletion() == DeletionStatusActive
}

func (t *Thread) IsAuthorVisible(viewerID string) bool {
	if t == nil || viewerID == "" || t.AuthorID != viewerID {
		return false
	}
	return t.deletion() != DeletionStatusPurged
}

func (t *Thread) IsActive() bool {
	return t != nil && t.deletion() == DeletionStatusActive
}

type ContentRevision struct {
	ID            string    `json:"id"`
	ThreadID      string    `json:"thread_id"`
	Version       int       `json:"version"`
	Title         string    `json:"title"`
	Content       string    `json:"content"`
	ContentFormat string    `json:"content_format"`
	Tags          []string  `json:"tags,omitempty"`
	Action        string    `json:"action"`
	Reason        string    `json:"reason,omitempty"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
}

type ModerationCase struct {
	ID         string     `json:"id"`
	ThreadID   string     `json:"thread_id"`
	Status     string     `json:"status"`
	Reason     string     `json:"reason,omitempty"`
	OpenedBy   string     `json:"opened_by"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	OpenedAt   time.Time  `json:"opened_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

type ModerationAction struct {
	ID          string    `json:"id"`
	CaseID      string    `json:"case_id,omitempty"`
	ThreadID    string    `json:"thread_id"`
	Action      string    `json:"action"`
	Reason      string    `json:"reason,omitempty"`
	ActorID     string    `json:"actor_id"`
	BeforeState string    `json:"before_state"`
	AfterState  string    `json:"after_state"`
	CreatedAt   time.Time `json:"created_at"`
}

func ContentStateSummary(t *Thread) string {
	if t == nil {
		return ""
	}
	return string(t.publication()) + "/" + string(t.moderation()) + "/" + string(t.deletion())
}
