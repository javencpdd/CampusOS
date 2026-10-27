package knowledge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

const (
	OfficialKind    = "knowledge.official"
	MaxMembers      = 1000
	MaxSelections   = 64
	MaxInventory    = 1000
	RefreshPageSize = 50
)

var ErrMFARequired = errors.New("knowledge management requires current MFA")

type Collection struct {
	ID               string `json:"id"`
	Version          int64  `json:"version,string"`
	Title            string `json:"title"`
	Status           string `json:"status"`
	CreatedByAdminID string `json:"created_by_admin_id"`
}
type Official struct {
	ID               string `json:"id"`
	Version          int64  `json:"version,string"`
	Title            string `json:"title"`
	Content          string `json:"content"`
	Format           string `json:"format"`
	Status           string `json:"status"`
	CreatedByAdminID string `json:"created_by_admin_id"`
}
type OfficialRevision struct {
	ID               string   `json:"id"`
	Version          int64    `json:"version,string"`
	SourceID         string   `json:"source_id"`
	OfficialVersion  int64    `json:"official_version,string"`
	Snapshot         Official `json:"snapshot"`
	ChangedByAdminID string   `json:"changed_by_admin_id"`
	ChangedAtMS      int64    `json:"changed_at_ms"`
}
type Selection struct {
	ID           string `json:"id"`
	Version      int64  `json:"version,string"`
	CollectionID string `json:"collection_id"`
	Kind         string `json:"kind"`
	SourceID     string `json:"source_id"`
	Active       bool   `json:"active"`
}
type Member struct {
	ID               string `json:"id"`
	Version          int64  `json:"version,string"`
	CollectionID     string `json:"collection_id"`
	SelectionID      string `json:"selection_id"`
	SelectionVersion int64  `json:"selection_version,string"`
	Kind             string `json:"kind"`
	SourceID         string `json:"source_id"`
	SourceRevision   string `json:"source_revision"`
}
type CollectionState struct {
	Collection Collection  `json:"collection"`
	Selections []Selection `json:"selections"`
	Members    []Member    `json:"members"`
}
type RefreshResult struct {
	State              CollectionState `json:"state"`
	Page               int             `json:"page"`
	NextPage           int             `json:"next_page"`
	Examined           int             `json:"examined"`
	Materialized       int             `json:"materialized"`
	SkippedUnsupported int             `json:"skipped_unsupported"`
	// This is a current bounded page, never a completed durable backfill receipt.
	HasMore bool `json:"has_more"`
}
type VisibleSource struct {
	MemberID     string                `json:"member_id"`
	CollectionID string                `json:"collection_id"`
	Revision     string                `json:"revision"`
	Source       PublicThreadCandidate `json:"source"`
}
type VisiblePage struct {
	Items    []VisibleSource `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

type CollectionService struct {
	repo       Repository
	admins     identityport.AdminExecutionAuthorityReader
	reliable   *reliability.Service
	query      communityport.ContentQuery
	categories communityport.SourceCategoryCatalog
	plain      PublicSourceCandidates
	articles   PublicArticleCandidates
	enabled    func() bool
}

func NewCollectionService(repo Repository, admins identityport.AdminExecutionAuthorityReader, reliable *reliability.Service, query communityport.ContentQuery, categories communityport.SourceCategoryCatalog, plain PublicSourceCandidates, articles PublicArticleCandidates, enabled func() bool) (*CollectionService, error) {
	for _, v := range []any{repo, admins, reliable, query, categories, plain, articles, enabled} {
		if nilSourceAdapter(v) {
			return nil, ErrSourceUnavailable
		}
	}
	s := &CollectionService{repo, admins, reliable, query, categories, plain, articles, enabled}
	if snap, ok := repo.(transaction.Snapshotter); ok {
		reliable.RegisterMemorySnapshotters(snap)
	}
	return s, nil
}
func opaque(prefix, id string) bool {
	if !strings.HasPrefix(id, prefix) || len(id) != len(prefix)+32 {
		return false
	}
	b, e := hex.DecodeString(id[len(prefix):])
	return e == nil && hex.EncodeToString(b) == id[len(prefix):]
}
func newID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", ErrSourceUnavailable
	}
	return prefix + hex.EncodeToString(b), nil
}
func textValid(s string, max int) bool {
	return len(s) > 0 && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0) && strings.TrimSpace(s) != ""
}
func titleValid(s string) bool {
	return textValid(s, MaxSourceTitleBytes) && !strings.ContainsAny(s, "\r\n")
}
func (s *CollectionService) check(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s == nil || s.enabled == nil {
		return ErrSourceUnavailable
	}
	if !s.enabled() {
		return ErrDisabled
	}
	return nil
}
func adminQuery(code string) identityport.AdminExecutionQuery {
	return identityport.AdminExecutionQuery{PermissionCode: code, Scope: identityport.PolicyResourceRef{Kind: "knowledge.collection_inventory", ID: "all"}}
}
func (s *CollectionService) admin(ctx context.Context, code string, resolve identityport.SessionPrincipalResolver, lock bool) (identityport.AdminExecutionAuthority, error) {
	var zero identityport.AdminExecutionAuthority
	if e := s.check(ctx); e != nil {
		return zero, e
	}
	if resolve == nil {
		return zero, ErrDenied
	}
	a, e := s.admins.LoadAdminExecutionAuthority(ctx, adminQuery(code), resolve, lock)
	if e != nil {
		return zero, e
	}
	if identityport.CheckAdminExecutionAuthority(a, a) != nil || a.Query != adminQuery(code) {
		return zero, ErrDenied
	}
	if !a.Allowed || a.ExpiresAtMS <= time.Now().UnixMilli() {
		if code == "knowledge.collection.manage" && (!a.Principal.SatisfiesAdminMFARequirement() || a.ExpiresAtMS <= time.Now().UnixMilli()) {
			return zero, ErrMFARequired
		}
		return zero, ErrDenied
	}
	return a, nil
}
func (s *CollectionService) managed(ctx context.Context, operation, id string, resolve identityport.SessionPrincipalResolver, action func(context.Context, string) error) error {
	a, e := s.admin(ctx, "knowledge.collection.manage", resolve, false)
	if e != nil {
		return e
	}
	return s.reliable.Execute(ctx, reliability.Command{Code: "knowledge." + operation, ActorType: "admin", ActorID: a.Principal.Actor.ID, ResourceType: "knowledge", ResourceID: id, PermissionCode: "knowledge.collection.manage", OperationCode: "knowledge." + operation, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent(selectionChangedEvent, "knowledge", id, map[string]string{"resource_id": id, "operation": operation})
	}}, func(tx context.Context) error {
		current, e := s.admin(tx, "knowledge.collection.manage", resolve, true)
		if e != nil {
			return e
		}
		if e = identityport.CheckAdminExecutionAuthority(a, current); e != nil {
			return e
		}
		if e = action(tx, a.Principal.Actor.ID); e != nil {
			return e
		}
		current, e = s.admin(tx, "knowledge.collection.manage", resolve, true)
		if e != nil {
			return e
		}
		if e = identityport.CheckAdminExecutionAuthority(a, current); e != nil {
			return e
		}
		return s.check(tx)
	})
}
func load[T any](ctx context.Context, repo Repository, k recordKind, id string, write bool) (T, error) {
	r, e := repo.get(ctx, k, id, write)
	if e != nil {
		var zero T
		return zero, e
	}
	return decode[T](r)
}
func save[T any](ctx context.Context, repo Repository, k recordKind, id string, expected int64, v T) (T, error) {
	r, e := repo.save(ctx, k, id, expected, v)
	if e != nil {
		var zero T
		return zero, e
	}
	return decode[T](r)
}
func list[T any](ctx context.Context, repo Repository, k recordKind, scope string, limit int) ([]T, error) {
	rows, e := repo.list(ctx, k, scope, limit+1)
	if e != nil {
		return nil, e
	}
	if len(rows) > limit {
		return nil, ErrCapacity
	}
	out := []T{}
	for _, r := range rows {
		v, e := decode[T](r)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *CollectionService) SaveCollection(ctx context.Context, in Collection, resolve identityport.SessionPrincipalResolver) (Collection, error) {
	var out Collection
	if !titleValid(in.Title) || (in.Status != "active" && in.Status != "disabled") || in.Version < 0 {
		return out, ErrInvalid
	}
	creating := in.ID == ""
	if creating {
		if in.Version != 0 {
			return out, ErrInvalid
		}
		id, e := newID("kc_")
		if e != nil {
			return out, e
		}
		in.ID = id
	} else if !opaque("kc_", in.ID) || in.Version < 1 {
		return out, ErrInvalid
	}
	e := s.managed(ctx, "collection.save", in.ID, resolve, func(tx context.Context, admin string) error {
		if creating {
			if e := s.repo.lockInventory(tx, collections); e != nil {
				return e
			}
			rows, e := list[Collection](tx, s.repo, collections, "", MaxInventory)
			if e != nil {
				return e
			}
			if len(rows) >= MaxInventory {
				return ErrCapacity
			}
			in.CreatedByAdminID = admin
		} else {
			old, e := load[Collection](tx, s.repo, collections, in.ID, true)
			if e != nil {
				return e
			}
			if old.Version != in.Version {
				return ErrConflict
			}
			in.CreatedByAdminID = old.CreatedByAdminID
		}
		var e error
		out, e = save(tx, s.repo, collections, in.ID, in.Version, in)
		return e
	})
	if e != nil {
		return Collection{}, e
	}
	return out, nil
}
func (s *CollectionService) SaveOfficial(ctx context.Context, in Official, resolve identityport.SessionPrincipalResolver) (Official, error) {
	var out Official
	if !titleValid(in.Title) || !textValid(in.Content, MaxSourceBodyBytes) || (in.Format != "text" && in.Format != "markdown") || (in.Status != "draft" && in.Status != "published" && in.Status != "withdrawn") || in.Version < 0 {
		return out, ErrInvalid
	}
	creating := in.ID == ""
	if creating {
		if in.Version != 0 {
			return out, ErrInvalid
		}
		id, e := newID("ko_")
		if e != nil {
			return out, e
		}
		in.ID = id
	} else if !opaque("ko_", in.ID) || in.Version < 1 {
		return out, ErrInvalid
	}
	e := s.managed(ctx, "official.save", in.ID, resolve, func(tx context.Context, admin string) error {
		if creating {
			if e := s.repo.lockInventory(tx, officials); e != nil {
				return e
			}
			rows, e := list[Official](tx, s.repo, officials, "", MaxInventory)
			if e != nil {
				return e
			}
			if len(rows) >= MaxInventory {
				return ErrCapacity
			}
			in.CreatedByAdminID = admin
		} else {
			old, e := load[Official](tx, s.repo, officials, in.ID, true)
			if e != nil {
				return e
			}
			if old.Version != in.Version {
				return ErrConflict
			}
			in.CreatedByAdminID = old.CreatedByAdminID
		}
		var e error
		out, e = save(tx, s.repo, officials, in.ID, in.Version, in)
		if e != nil {
			return e
		}
		rid, e := newID("kr_")
		if e != nil {
			return e
		}
		_, e = save(tx, s.repo, revisions, rid, 0, OfficialRevision{ID: rid, SourceID: out.ID, OfficialVersion: out.Version, Snapshot: out, ChangedByAdminID: admin, ChangedAtMS: time.Now().UnixMilli()})
		return e
	})
	if e != nil {
		return Official{}, e
	}
	return out, nil
}
func (s *CollectionService) state(ctx context.Context, id string, write bool) (CollectionState, error) {
	c, e := load[Collection](ctx, s.repo, collections, id, write)
	if e != nil {
		return CollectionState{}, e
	}
	rules, e := list[Selection](ctx, s.repo, selections, id, MaxSelections)
	if e != nil {
		return CollectionState{}, e
	}
	ms, e := list[Member](ctx, s.repo, members, id, MaxMembers)
	if e != nil {
		return CollectionState{}, e
	}
	return CollectionState{c, rules, ms}, nil
}
func (s *CollectionService) CollectionState(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver) (CollectionState, error) {
	var out CollectionState
	if !opaque("kc_", id) {
		return out, ErrInvalid
	}
	e := s.adminRead(ctx, resolve, func(tx context.Context) error { var e error; out, e = s.state(tx, id, false); return e })
	if e != nil {
		return CollectionState{}, e
	}
	return out, nil
}
func (s *CollectionService) adminRead(ctx context.Context, resolve identityport.SessionPrincipalResolver, read func(context.Context) error) error {
	a, e := s.admin(ctx, "knowledge.collection.read", resolve, false)
	if e != nil {
		return e
	}
	return s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		current, e := s.admin(tx, "knowledge.collection.read", resolve, true)
		if e != nil {
			return e
		}
		if e = identityport.CheckAdminExecutionAuthority(a, current); e != nil {
			return e
		}
		if e = read(tx); e != nil {
			return e
		}
		current, e = s.admin(tx, "knowledge.collection.read", resolve, true)
		if e != nil {
			return e
		}
		if e = identityport.CheckAdminExecutionAuthority(a, current); e != nil {
			return e
		}
		return s.check(tx)
	})
}
func (s *CollectionService) Inventory(ctx context.Context, kind string, resolve identityport.SessionPrincipalResolver) (any, error) {
	var out any
	e := s.adminRead(ctx, resolve, func(tx context.Context) error {
		if kind == "collections" {
			rows, e := list[Collection](tx, s.repo, collections, "", MaxInventory)
			out = rows
			return e
		}
		if kind == "officials" {
			rows, e := list[Official](tx, s.repo, officials, "", MaxInventory)
			if e != nil {
				return e
			}
			for i := range rows {
				rows[i].Content = ""
			}
			out = rows
			return nil
		}
		return ErrInvalid
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

type HistoryPage struct {
	Items      []OfficialRevision `json:"items"`
	NextBefore int64              `json:"next_before,string"`
}

func (s *CollectionService) OfficialHistory(ctx context.Context, id string, before int64, resolve identityport.SessionPrincipalResolver) (HistoryPage, error) {
	var out HistoryPage
	if !opaque("ko_", id) || before < 0 {
		return out, ErrInvalid
	}
	e := s.adminRead(ctx, resolve, func(tx context.Context) error {
		if _, e := load[Official](tx, s.repo, officials, id, false); e != nil {
			return e
		}
		rows, e := s.repo.history(tx, id, before)
		if e != nil {
			return e
		}
		out.Items = []OfficialRevision{}
		more := len(rows) > 50
		if more {
			rows = rows[:50]
		}
		for _, r := range rows {
			v, e := decode[OfficialRevision](r)
			if e != nil {
				return e
			}
			out.Items = append(out.Items, v)
		}
		if more {
			out.NextBefore = out.Items[len(out.Items)-1].OfficialVersion
		}
		return nil
	})
	if e != nil {
		return HistoryPage{}, e
	}
	return out, nil
}

// Board eligibility uses Community's current catalogue; a closed board can
// still be read, whereas archived boards/parents cannot supply new or old bytes.
func (s *CollectionService) board(ctx context.Context, id string) (string, error) {
	if reader, ok := s.categories.(communityport.SourceBoardFactsReader); ok {
		board, parent, err := reader.ReadSourceBoardFacts(ctx, id)
		if err != nil {
			return "", ErrSourceUnavailable
		}
		if board.ID != id || board.NodeKind != domain.CategoryNodeBoard || board.LifecycleStatus != domain.CategoryLifecycleActive || board.Version < 1 {
			return "", ErrNotFound
		}
		stamp := fmt.Sprintf("%s:%d", id, board.Version)
		if board.ParentID != "" {
			if parent.ID != board.ParentID || parent.NodeKind != domain.CategoryNodeGroup || parent.LifecycleStatus != domain.CategoryLifecycleActive || parent.Version < 1 {
				return "", ErrNotFound
			}
			stamp += fmt.Sprintf(":%s:%d", parent.ID, parent.Version)
		}
		return stamp, nil
	}
	rows, e := s.categories.ListSourceCategories(ctx)
	if e != nil {
		return "", ErrSourceUnavailable
	}
	var board *domain.Category
	byID := map[string]*domain.Category{}
	for _, c := range rows {
		if c != nil {
			byID[c.ID] = c
		}
	}
	board = byID[id]
	if board == nil || board.NodeKind != domain.CategoryNodeBoard || board.LifecycleStatus != domain.CategoryLifecycleActive || board.Version < 1 {
		return "", ErrNotFound
	}
	stamp := fmt.Sprintf("%s:%d", id, board.Version)
	if board.ParentID != nil {
		parent := byID[*board.ParentID]
		if parent == nil || parent.NodeKind != domain.CategoryNodeGroup || parent.LifecycleStatus != domain.CategoryLifecycleActive || parent.Version < 1 {
			return "", ErrNotFound
		}
		stamp += fmt.Sprintf(":%s:%d", parent.ID, parent.Version)
	}
	return stamp, nil
}
func (s *CollectionService) origin(ctx context.Context, kind, id string) (PublicThreadCandidate, error) {
	switch kind {
	case OfficialKind:
		row, e := load[Official](ctx, s.repo, officials, id, false)
		if e != nil {
			return PublicThreadCandidate{}, e
		}
		if row.Status != "published" || row.Version < 1 || !titleValid(row.Title) || !textValid(row.Content, MaxSourceBodyBytes) || (row.Format != "text" && row.Format != "markdown") {
			return PublicThreadCandidate{}, ErrNotFound
		}
		revision, e := memoRevision(ctx, revisionMemoSlot{"official", id}, row, len(row.Title)+len(row.Content), func() (string, error) {
			raw, e := json.Marshal(row)
			if e != nil {
				return "", e
			}
			h := sha256.Sum256(append([]byte("campusos.knowledge.official/v1\x00"), raw...))
			return hex.EncodeToString(h[:]), nil
		})
		if e != nil {
			return PublicThreadCandidate{}, ErrSourceUnavailable
		}
		return PublicThreadCandidate{Reference: SourceReference{Kind: OfficialKind, ID: id, Revision: revision}, Title: row.Title, Content: row.Content, Format: row.Format, URL: "/knowledge/officials/" + id}, nil
	case PublicThreadKind:
		return s.plain.PreviewPublicThread(ctx, id)
	case PublicArticleKind:
		return s.articles.PreviewPublicArticle(ctx, id)
	default:
		return PublicThreadCandidate{}, ErrUnsupportedSource
	}
}
func (s *CollectionService) threadOrigin(ctx context.Context, id string) (PublicThreadCandidate, error) {
	t, e := s.query.GetPublicThread(ctx, id)
	if e != nil || t == nil {
		return PublicThreadCandidate{}, ErrNotFound
	}
	switch t.ThreadType {
	case domain.ThreadTypeDiscussion:
		return s.origin(ctx, PublicThreadKind, id)
	case domain.ThreadTypeArticle:
		return s.origin(ctx, PublicArticleKind, id)
	default:
		return PublicThreadCandidate{}, ErrUnsupportedSource
	}
}
func (s *CollectionService) SaveSelection(ctx context.Context, collectionID string, collectionVersion int64, in Selection, resolve identityport.SessionPrincipalResolver) (CollectionState, error) {
	var out CollectionState
	if !opaque("kc_", collectionID) || collectionVersion < 1 || (in.Kind != "thread" && in.Kind != "board" && in.Kind != "official") || in.Version < 0 {
		return out, ErrInvalid
	}
	if (in.Kind == "official" && !opaque("ko_", in.SourceID)) || (in.Kind != "official" && !sourceID(in.SourceID)) {
		return out, ErrInvalid
	}
	creating := in.ID == ""
	if creating {
		if in.Version != 0 {
			return out, ErrInvalid
		}
		id, e := newID("ks_")
		if e != nil {
			return out, e
		}
		in.ID = id
	} else if !opaque("ks_", in.ID) || in.Version < 1 {
		return out, ErrInvalid
	}
	in.CollectionID = collectionID
	e := s.managed(ctx, "selection.save", collectionID, resolve, func(tx context.Context, _ string) error {
		state, e := s.state(tx, collectionID, true)
		if e != nil {
			return e
		}
		if state.Collection.Version != collectionVersion {
			return ErrConflict
		}
		if creating {
			if len(state.Selections) >= MaxSelections {
				return ErrCapacity
			}
			for _, r := range state.Selections {
				if r.Kind == in.Kind && r.SourceID == in.SourceID {
					return ErrConflict
				}
			}
		} else {
			old, e := load[Selection](tx, s.repo, selections, in.ID, true)
			if e != nil {
				return e
			}
			if old.Version != in.Version || old.CollectionID != collectionID || old.Kind != in.Kind || old.SourceID != in.SourceID {
				return ErrConflict
			}
		}
		var source PublicThreadCandidate
		if in.Active {
			switch in.Kind {
			case "board":
				_, e = s.board(tx, in.SourceID)
			case "official":
				source, e = s.origin(tx, OfficialKind, in.SourceID)
			case "thread":
				source, e = s.threadOrigin(tx, in.SourceID)
			}
			if e != nil {
				return e
			}
		}
		rule, e := save(tx, s.repo, selections, in.ID, in.Version, in)
		if e != nil {
			return e
		}
		// Rule changes invalidate its old generation immediately. Projections are
		// independent rows and can be rebuilt; withdrawal never copies origin ACLs.
		for _, m := range state.Members {
			if m.SelectionID == rule.ID {
				if e = s.repo.deleteMember(tx, m.ID); e != nil {
					return e
				}
			}
		}
		if in.Active && in.Kind != "board" {
			if len(state.Members) >= MaxMembers && creating {
				return ErrCapacity
			}
			if e = s.materialize(tx, rule, source); e != nil {
				return e
			}
		}
		if _, e = save(tx, s.repo, collections, collectionID, collectionVersion, state.Collection); e != nil {
			return e
		}
		out, e = s.state(tx, collectionID, true)
		return e
	})
	if e != nil {
		return CollectionState{}, e
	}
	return out, nil
}
func (s *CollectionService) materialize(ctx context.Context, rule Selection, source PublicThreadCandidate) error {
	id, e := newID("km_")
	if e != nil {
		return e
	}
	m := Member{ID: id, CollectionID: rule.CollectionID, SelectionID: rule.ID, SelectionVersion: rule.Version, Kind: source.Reference.Kind, SourceID: source.Reference.ID, SourceRevision: source.Reference.Revision}
	_, e = save(ctx, s.repo, members, id, 0, m)
	return e
}
func (s *CollectionService) Refresh(ctx context.Context, collectionID, selectionID string, collectionVersion, selectionVersion int64, page int, resolve identityport.SessionPrincipalResolver) (RefreshResult, error) {
	var out RefreshResult
	if !opaque("kc_", collectionID) || !opaque("ks_", selectionID) || collectionVersion < 1 || selectionVersion < 1 || page < 1 || page > 100000 {
		return out, ErrInvalid
	}
	e := s.managed(ctx, "selection.refresh", collectionID, resolve, func(tx context.Context, _ string) error {
		state, e := s.state(tx, collectionID, true)
		if e != nil {
			return e
		}
		if state.Collection.Version != collectionVersion {
			return ErrConflict
		}
		rule, e := load[Selection](tx, s.repo, selections, selectionID, true)
		if e != nil {
			return e
		}
		if rule.CollectionID != collectionID || rule.Version != selectionVersion || !rule.Active {
			return ErrConflict
		}
		var candidates []PublicThreadCandidate
		if rule.Kind == "board" {
			stamp, e := s.board(tx, rule.SourceID)
			if e != nil {
				return e
			}
			rows, total, e := s.query.ListPublicThreads(tx, domain.ThreadListFilter{CategoryID: rule.SourceID, Page: page, PageSize: RefreshPageSize})
			if e != nil {
				return e
			}
			out.Examined = len(rows)
			out.HasMore = int64(page*RefreshPageSize) < total
			for _, row := range rows {
				if row == nil || row.CategoryID != rule.SourceID {
					return ErrConflict
				}
				candidate, e := s.threadOrigin(tx, row.ID)
				if errors.Is(e, ErrUnsupportedSource) || errors.Is(e, ErrSourceTooLarge) {
					out.SkippedUnsupported++
					continue
				}
				if e != nil {
					return e
				}
				if candidate.CategoryID != rule.SourceID {
					return ErrConflict
				}
				candidates = append(candidates, candidate)
			}
			current, e := s.board(tx, rule.SourceID)
			if e != nil {
				return e
			}
			if current != stamp {
				return ErrConflict
			}
		} else {
			if page != 1 {
				return ErrInvalid
			}
			var source PublicThreadCandidate
			if rule.Kind == "official" {
				source, e = s.origin(tx, OfficialKind, rule.SourceID)
			} else {
				source, e = s.threadOrigin(tx, rule.SourceID)
			}
			if e != nil {
				return e
			}
			candidates = []PublicThreadCandidate{source}
			out.Examined = 1
		}
		for _, candidate := range candidates {
			found := -1
			for i, m := range state.Members {
				if m.SelectionID == rule.ID && m.Kind == candidate.Reference.Kind && m.SourceID == candidate.Reference.ID {
					found = i
					break
				}
			}
			if found >= 0 {
				old := state.Members[found]
				if old.SelectionVersion != rule.Version || old.SourceRevision != candidate.Reference.Revision {
					old.SelectionVersion = rule.Version
					old.SourceRevision = candidate.Reference.Revision
					if _, e = save(tx, s.repo, members, old.ID, old.Version, old); e != nil {
						return e
					}
				}
			} else {
				if len(state.Members) >= MaxMembers {
					return ErrCapacity
				}
				if e = s.materialize(tx, rule, candidate); e != nil {
					return e
				}
				state.Members = append(state.Members, Member{})
			}
			out.Materialized++
		}
		if _, e = save(tx, s.repo, collections, collectionID, collectionVersion, state.Collection); e != nil {
			return e
		}
		out.State, e = s.state(tx, collectionID, true)
		out.Page = page
		if out.HasMore {
			out.NextPage = page + 1
		}
		return e
	})
	if e != nil {
		return RefreshResult{}, e
	}
	return out, nil
}
func (s *CollectionService) RemoveMember(ctx context.Context, collectionID, memberID string, version int64, resolve identityport.SessionPrincipalResolver) (CollectionState, error) {
	var out CollectionState
	if !opaque("kc_", collectionID) || !opaque("km_", memberID) || version < 1 {
		return out, ErrInvalid
	}
	e := s.managed(ctx, "member.remove", collectionID, resolve, func(tx context.Context, _ string) error {
		state, e := s.state(tx, collectionID, true)
		if e != nil {
			return e
		}
		if state.Collection.Version != version {
			return ErrConflict
		}
		m, e := load[Member](tx, s.repo, members, memberID, true)
		if e != nil {
			return e
		}
		if m.CollectionID != collectionID {
			return ErrNotFound
		}
		if e = s.repo.deleteMember(tx, memberID); e != nil {
			return e
		}
		if _, e = save(tx, s.repo, collections, collectionID, version, state.Collection); e != nil {
			return e
		}
		out, e = s.state(tx, collectionID, true)
		return e
	})
	if e != nil {
		return CollectionState{}, e
	}
	return out, nil
}
func collectionPolicy(c Collection, m Member, revision string) identityport.ResourcePolicyRequest {
	visible := true
	return identityport.ResourcePolicyRequest{Contract: identityport.ResourcePolicyContract, RequestID: m.ID, Principal: identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "anonymous"}, Audience: "public", AuthenticationStrength: "none"}, Action: "knowledge.source.read", Facts: identityport.ResourcePolicyFacts{Kind: "knowledge_source", ID: m.ID, CollectionID: c.ID, Status: "published", PublicationStatus: "published", OriginVisible: &visible, Version: revision}, Grants: []identityport.ResourcePolicyGrant{}, EvaluatedAtMS: time.Now().UnixMilli(), PolicyVersion: "knowledge.public-source/v1"}
}
func (s *CollectionService) visible(ctx context.Context, state CollectionState, m Member) (VisibleSource, error) {
	var empty VisibleSource
	if state.Collection.Status != "active" || m.CollectionID != state.Collection.ID {
		return empty, ErrNotFound
	}
	var rule Selection
	for _, r := range state.Selections {
		if r.ID == m.SelectionID {
			rule = r
			break
		}
	}
	if !rule.Active || rule.Version != m.SelectionVersion || rule.CollectionID != state.Collection.ID {
		return empty, ErrNotFound
	}
	if (rule.Kind == "official" && (m.Kind != OfficialKind || m.SourceID != rule.SourceID)) || (rule.Kind == "thread" && (m.Kind != PublicThreadKind && m.Kind != PublicArticleKind || m.SourceID != rule.SourceID)) || (rule.Kind == "board" && (m.Kind != PublicThreadKind && m.Kind != PublicArticleKind)) {
		return empty, ErrNotFound
	}
	stamp := ""
	var e error
	if rule.Kind == "board" {
		stamp, e = s.board(ctx, rule.SourceID)
		if e != nil {
			return empty, e
		}
	}
	source, e := s.origin(ctx, m.Kind, m.SourceID)
	if e != nil {
		return empty, e
	}
	if rule.Kind == "board" && source.CategoryID != rule.SourceID {
		return empty, ErrNotFound
	}
	// A member's observed revision is diagnostic only. Current origin facts,
	// collection/rule generation and board lifecycle authorize every returned byte.
	bound := struct {
		Collection Collection
		Rule       Selection
		Member     Member
		Source     SourceReference
		Board      string
	}{state.Collection, rule, m, source.Reference, stamp}
	revision, e := memoRevision(ctx, revisionMemoSlot{"visibility", m.ID}, bound, len(state.Collection.Title)+len(stamp)+len(m.SourceRevision), func() (string, error) {
		// Preserve the exact persisted visibility-revision wire contract.
		raw, err := json.Marshal([]any{state.Collection, rule, m, source.Reference, stamp})
		if err != nil {
			return "", err
		}
		digest := sha256.Sum256(raw)
		return hex.EncodeToString(digest[:]), nil
	})
	if e != nil {
		return empty, e
	}
	request := collectionPolicy(state.Collection, m, revision)
	decision, e := (policy.ResourcePolicy{}).DecideResource(request)
	if e != nil || decision.Effect != "allow" {
		return empty, ErrNotFound
	}
	current := source
	pinned := false
	// Only the concrete plain-source adapter can prove that its own G1 row
	// is immutable in this callback. The retrieval return phase still opens
	// current G1 again; memory/custom adapters keep the two-origin comparison.
	if plain, ok := s.plain.(*PublicSourceService); ok && m.Kind == PublicThreadKind {
		if pin, ok := plain.query.(communityport.SourceReadPin); ok {
			pinned = pin.SourceReadPinned(ctx, m.SourceID)
		}
		if pinned {
			if e = plain.check(ctx); e != nil {
				return empty, e
			}
		}
	}
	if !pinned {
		current, e = s.origin(ctx, m.Kind, m.SourceID)
		if e != nil {
			return empty, e
		}
	}
	if source != current {
		return empty, ErrSourceChanged
	}
	if rule.Kind == "board" {
		now, e := s.board(ctx, rule.SourceID)
		if e != nil {
			return empty, e
		}
		if now != stamp {
			return empty, ErrSourceChanged
		}
	}
	if e = policy.CheckResourceDecision(request, decision, collectionPolicy(state.Collection, m, revision)); e != nil {
		return empty, e
	}
	if current.Reference.Kind == OfficialKind {
		current.URL = "/api/v1/knowledge/collections/" + state.Collection.ID + "/sources/" + m.ID
	}
	return VisibleSource{m.ID, state.Collection.ID, revision, current}, s.check(ctx)
}
func hiddenOrigin(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrSourceUnavailable) || errors.Is(err, ErrUnsupportedSource) || errors.Is(err, ErrSourceTooLarge) || errors.Is(err, ErrSourceChanged) || errors.Is(err, ErrDisabled)
}
func (s *CollectionService) Sources(ctx context.Context, id string, page, pageSize int) (VisiblePage, error) {
	var out VisiblePage
	if !opaque("kc_", id) || page < 1 || page > 100000 || pageSize < 1 || pageSize > 50 {
		return out, ErrInvalid
	}
	if e := s.check(ctx); e != nil {
		return out, e
	}
	e := s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		state, e := s.state(tx, id, false)
		if e != nil {
			return e
		}
		if state.Collection.Status != "active" {
			return ErrNotFound
		}
		all := []VisibleSource{}
		seen := map[string]bool{}
		for _, m := range state.Members {
			v, e := s.visible(tx, state, m)
			if e != nil {
				if hiddenOrigin(e) {
					continue
				}
				return e
			}
			key := v.Source.Reference.Kind + ":" + v.Source.Reference.ID
			if !seen[key] {
				seen[key] = true
				all = append(all, v)
			}
		}
		out = VisiblePage{Items: []VisibleSource{}, Total: len(all), Page: page, PageSize: pageSize}
		start := (page - 1) * pageSize
		if start < len(all) {
			end := start + pageSize
			if end > len(all) {
				end = len(all)
			}
			out.Items = all[start:end]
		}
		return s.check(tx)
	})
	if e != nil {
		return VisiblePage{}, e
	}
	return out, nil
}
func (s *CollectionService) Source(ctx context.Context, id, memberID, revision string) (VisibleSource, error) {
	var out VisibleSource
	if !opaque("kc_", id) || !opaque("km_", memberID) || (revision != "" && (len(revision) != 64 || strings.Trim(revision, "0123456789abcdef") != "")) {
		return out, ErrInvalid
	}
	if e := s.check(ctx); e != nil {
		return out, e
	}
	e := s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		state, e := s.state(tx, id, false)
		if e != nil {
			return e
		}
		for _, m := range state.Members {
			if m.ID == memberID {
				out, e = s.visible(tx, state, m)
				if e == nil && revision != "" && out.Revision != revision {
					return ErrConflict
				}
				return e
			}
		}
		return ErrNotFound
	})
	if e != nil {
		return VisibleSource{}, e
	}
	return out, nil
}
func (s *CollectionService) PublicCollections(ctx context.Context) ([]Collection, error) {
	if e := s.check(ctx); e != nil {
		return nil, e
	}
	out := []Collection{}
	e := s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		rows, e := list[Collection](tx, s.repo, collections, "", MaxInventory)
		if e != nil {
			return e
		}
		for _, c := range rows {
			if c.Status == "active" {
				c.CreatedByAdminID = ""
				out = append(out, c)
			}
		}
		return s.check(tx)
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}
