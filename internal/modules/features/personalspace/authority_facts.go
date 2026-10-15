package space

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"math"
	"reflect"
	"sort"
)

var ErrSpaceFactsUnavailable = errors.New("personal space facts unavailable")

// Private actual row facts are a data boundary, never authentication or an
// article/file capability. The owning command supplies the current actor.
type SpaceAuthorityReader interface {
	CheckSpaceAuthority(context.Context) error
	LoadSpaceAuthority(context.Context, string, bool) (json.RawMessage, error)
	LoadStyleSnapshotAuthority(context.Context, string, bool) ([]json.RawMessage, error)
}

func spaceAuthorityJSON(s *Space) (json.RawMessage, error) {
	if s == nil || s.FactsVersion < 1 {
		return nil, ErrSpaceFactsUnavailable
	}
	return json.Marshal(map[string]any{
		"id": s.ID, "user_id": s.UserID, "title": s.Title, "bio": s.Bio, "avatar": s.Avatar, "cover_image": s.CoverImage,
		"theme": s.Theme, "layout": s.Layout, "style_name": s.StyleName, "style_version": s.StyleVersion, "style_manifest": styleManifestForSave(s.StyleManifest),
		"visibility": s.Visibility, "sync_enabled": s.SyncEnabled, "sync_categories": append([]string{}, s.SyncCategories...), "sync_tags": append([]string{}, s.SyncTags...),
		"disabled_at": s.DisabledAt, "disabled_by": s.DisabledBy, "disabled_reason": s.DisabledReason, "disabled_actor_kind": s.DisabledActorKind, "disabled_actor_id": s.DisabledActorID,
		"last_sync_at": s.LastSyncAt, "last_sync_error": s.LastSyncError, "created_at": s.CreatedAt, "updated_at": s.UpdatedAt, "deleted_at": nil, "facts_version": s.FactsVersion,
	})
}
func snapshotAuthorityJSON(s *StyleSnapshot) (json.RawMessage, error) {
	if s == nil || s.FactsVersion < 1 {
		return nil, ErrSpaceFactsUnavailable
	}
	return json.Marshal(map[string]any{"id": s.ID, "user_id": s.UserID, "snapshot_type": s.SnapshotType, "style_name": s.StyleName, "style_version": s.StyleVersion, "theme": s.Theme, "layout": s.Layout, "style_manifest": styleManifestForSave(s.StyleManifest), "created_at": s.CreatedAt, "facts_version": s.FactsVersion})
}
func sameSpaceMetadata(a, b *Space) bool {
	if a == nil || b == nil {
		return a == b
	}
	x, y := cloneSpace(a), cloneSpace(b)
	x.FactsVersion, y.FactsVersion = 0, 0
	x.UpdatedAt, y.UpdatedAt = x.CreatedAt, y.CreatedAt
	x.LastSyncAt, y.LastSyncAt = nil, nil
	x.LastSyncError, y.LastSyncError = "", ""
	x.IsDefault, y.IsDefault = false, false
	return reflect.DeepEqual(x, y)
}
func (r *MemoryRepository) nextFactsVersion() (int64, error) {
	if r.factsCounter == math.MaxInt64 {
		return 0, ErrSpaceFactsUnavailable
	}
	r.factsCounter++
	return r.factsCounter, nil
}
func normalizeSpaceActor(s *Space) {
	if s.DisabledActorKind == "" {
		s.DisabledActorKind, s.DisabledActorID = "legacy_unknown", s.DisabledBy
	}
}
func (r *MemoryRepository) CheckSpaceAuthority(ctx context.Context) error {
	if r == nil {
		return ErrSpaceFactsUnavailable
	}
	return ctx.Err()
}
func (r *MemoryRepository) LoadSpaceAuthority(ctx context.Context, owner string, lock bool) (json.RawMessage, error) {
	if r == nil || owner == "" || lock && !transaction.Active(ctx) {
		return nil, ErrSpaceFactsUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := r.spaces[owner]
	if s == nil {
		return nil, ErrSpaceNotFound
	}
	return spaceAuthorityJSON(s)
}
func (r *MemoryRepository) LoadStyleSnapshotAuthority(ctx context.Context, owner string, lock bool) ([]json.RawMessage, error) {
	if r == nil || owner == "" || lock && !transaction.Active(ctx) {
		return nil, ErrSpaceFactsUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := append([]*StyleSnapshot(nil), r.snapshots[owner]...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	rows := make([]json.RawMessage, 0, len(items))
	for _, s := range items {
		raw, err := snapshotAuthorityJSON(s)
		if err != nil {
			return nil, err
		}
		rows = append(rows, raw)
	}
	return rows, nil
}

type spaceMemorySnapshot struct {
	spaces    map[string]*Space
	contents  map[string]*SpaceContent
	snapshots map[string][]*StyleSnapshot
}

func (r *MemoryRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := spaceMemorySnapshot{spaces: map[string]*Space{}, contents: map[string]*SpaceContent{}, snapshots: map[string][]*StyleSnapshot{}}
	for id, v := range r.spaces {
		s.spaces[id] = cloneSpace(v)
	}
	for id, v := range r.contents {
		s.contents[id] = cloneContent(v)
	}
	for id, items := range r.snapshots {
		for _, v := range items {
			s.snapshots[id] = append(s.snapshots[id], cloneSnapshot(v))
		}
	}
	return s
}
func (r *MemoryRepository) Restore(value any) {
	s, ok := value.(spaceMemorySnapshot)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spaces = map[string]*Space{}
	r.contents = map[string]*SpaceContent{}
	r.snapshots = map[string][]*StyleSnapshot{}
	for id, v := range s.spaces {
		r.spaces[id] = cloneSpace(v)
	}
	for id, v := range s.contents {
		r.contents[id] = cloneContent(v)
	}
	for id, items := range s.snapshots {
		for _, v := range items {
			r.snapshots[id] = append(r.snapshots[id], cloneSnapshot(v))
		}
	}
	// Never rewind allocation; restored content cannot reuse an abandoned proof.
}
