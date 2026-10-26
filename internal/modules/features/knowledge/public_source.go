// Package knowledge owns knowledge-source selection and projections. Candidate
// reads are a preparation seam for trusted Built-in Features, not collection
// membership, indexing consent, or authority for plugins/models/integrations.
package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"unicode/utf8"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
)

const (
	MaxSourceBodyBytes  = 64 * 1024
	MaxSourceTitleBytes = 1024
	PublicThreadKind    = "community.thread"
)

var (
	ErrDisabled          = errors.New("knowledge feature disabled")
	ErrInvalidReference  = errors.New("invalid knowledge source reference")
	ErrSourceUnavailable = errors.New("knowledge source unavailable")
	ErrSourceChanged     = errors.New("knowledge source changed")
	ErrUnsupportedSource = errors.New("knowledge source format unsupported")
	ErrSourceTooLarge    = errors.New("knowledge source exceeds preview limit")
)

// PublicThreadQuery deliberately accepts no actor/owner/visibility override.
// Production binds Community's current checked public reader. It never reads a
// repository, cache, private document, or a privileged viewer path directly.
type PublicThreadQuery interface {
	GetPublicThread(context.Context, string) (*domain.Thread, error)
}

type SourceReference struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

// PublicThreadCandidate contains only the selected public text and its origin.
// Content is untrusted text/Markdown data; no HTML rendering, URL fetching,
// attachment expansion, storage, parser, or model execution happens here.
type PublicThreadCandidate struct {
	Reference  SourceReference `json:"reference"`
	CategoryID string          `json:"category_id"`
	Title      string          `json:"title"`
	Content    string          `json:"content"`
	Format     string          `json:"format"`
	URL        string          `json:"url"`
}

type PublicSourceCandidates interface {
	PreviewPublicThread(context.Context, string) (PublicThreadCandidate, error)
	RecheckPublicThread(context.Context, SourceReference) (PublicThreadCandidate, error)
}

type PublicSourceService struct {
	query   PublicThreadQuery
	enabled func() bool
}

func NewPublicSourceService(query PublicThreadQuery, enabled func() bool) (*PublicSourceService, error) {
	if nilQuery(query) || enabled == nil {
		return nil, ErrSourceUnavailable
	}
	return &PublicSourceService{query: query, enabled: enabled}, nil
}

func nilQuery(query PublicThreadQuery) bool {
	return nilSourceAdapter(query)
}

func nilSourceAdapter(query any) bool {
	if query == nil {
		return true
	}
	v := reflect.ValueOf(query)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}

func sourceID(id string) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == id
}

func validReference(ref SourceReference) bool {
	return validReferenceFor(ref, PublicThreadKind)
}

func validReferenceFor(ref SourceReference, kind string) bool {
	if ref.Kind != kind || !sourceID(ref.ID) || len(ref.Revision) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(ref.Revision)
	return err == nil && hex.EncodeToString(decoded) == ref.Revision
}

func (s *PublicSourceService) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilQuery(s.query) || s.enabled == nil {
		return ErrSourceUnavailable
	}
	if !s.enabled() {
		return ErrDisabled
	}
	return nil
}

func (s *PublicSourceService) read(ctx context.Context, id string) (PublicThreadCandidate, error) {
	var empty PublicThreadCandidate
	if err := s.check(ctx); err != nil {
		return empty, err
	}
	row, err := s.query.GetPublicThread(ctx, id)
	if err != nil {
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		// Existence, private metadata, SQL, and adapter errors stay private.
		return empty, ErrSourceUnavailable
	}
	if err = s.check(ctx); err != nil {
		return empty, err
	}
	if row == nil || row.ID != id || row.FactsVersion < 1 || !sourceID(row.CategoryID) || row.DeletedAt != nil ||
		row.PublicationStatus != domain.PublicationStatusPublished || row.ModerationStatus != domain.ModerationStatusClear || row.DeletionStatus != domain.DeletionStatusActive {
		return empty, ErrSourceUnavailable
	}
	// RichText, Mutual Aid, and Secondhand have owning Feature lifecycles and
	// body semantics. They need explicit source adapters in later09a slices.
	if row.ThreadType != domain.ThreadTypeDiscussion || (row.ContentFormat != "text" && row.ContentFormat != "markdown") {
		return empty, ErrUnsupportedSource
	}
	if len(row.Content) > MaxSourceBodyBytes || len(row.Title) > MaxSourceTitleBytes {
		return empty, ErrSourceTooLarge
	}
	if row.Title == "" || row.Content == "" || !utf8.ValidString(row.Title) || !utf8.ValidString(row.Content) {
		return empty, ErrSourceUnavailable
	}
	// Repository-owned facts_version prevents A->private->A from reviving an
	// old reference, even if content/current_revision/updated_at are unchanged.
	// Counters and wall-clock timestamps are intentionally not revision facts.
	bound := struct {
		Contract, ID, CategoryID, ThreadType, Format, Title, Content string
		FactsVersion                                                 int64
		ContentRevision                                              int
	}{"campusos.knowledge.public-thread-candidate/v1", id, row.CategoryID, string(row.ThreadType), row.ContentFormat, row.Title, row.Content, row.FactsVersion, row.CurrentRevision}
	revision, err := memoRevision(ctx, revisionMemoSlot{"thread", id}, bound, len(row.Title)+len(row.Content), func() (string, error) {
		wire, e := json.Marshal(bound)
		if e != nil {
			return "", e
		}
		digest := sha256.Sum256(wire)
		return hex.EncodeToString(digest[:]), nil
	})
	if err != nil {
		return empty, ErrSourceUnavailable
	}
	return PublicThreadCandidate{Reference: SourceReference{Kind: PublicThreadKind, ID: id, Revision: revision}, CategoryID: row.CategoryID, Title: row.Title, Content: row.Content, Format: row.ContentFormat, URL: "/threads/" + id}, nil
}

func (s *PublicSourceService) PreviewPublicThread(ctx context.Context, id string) (PublicThreadCandidate, error) {
	var empty PublicThreadCandidate
	if !sourceID(id) {
		return empty, ErrInvalidReference
	}
	initial, err := s.read(ctx, id)
	if err != nil {
		return empty, err
	}
	// G1 above already checked the current row. A domain-owned pin makes the
	// duplicate read within this preview redundant; final callers still reopen
	// G1. Unlocked/memory adapters keep the original two-read comparison.
	if pin, ok := s.query.(communityport.SourceReadPin); ok && pin.SourceReadPinned(ctx, id) {
		if err := s.check(ctx); err != nil {
			return empty, err
		}
		return initial, nil
	}
	current, err := s.read(ctx, id)
	if err != nil {
		return empty, err
	}
	if initial != current {
		return empty, ErrSourceChanged
	}
	return current, nil
}

// Recheck reacquires current public facts. A saved reference is a version
// condition, never a bearer capability. Consumers must recheck at their own
// return/commit boundary and separately enforce collection and egress policy.
func (s *PublicSourceService) RecheckPublicThread(ctx context.Context, ref SourceReference) (PublicThreadCandidate, error) {
	var empty PublicThreadCandidate
	if !validReference(ref) {
		return empty, ErrInvalidReference
	}
	current, err := s.PreviewPublicThread(ctx, ref.ID)
	if err != nil {
		return empty, err
	}
	if current.Reference != ref {
		return empty, ErrSourceChanged
	}
	return current, nil
}
