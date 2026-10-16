package richtext

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

// Call with the store write lock held, after validating the entire mutation.
// Reservation fails before touching data or consuming any allocation.
func (s *MemoryStore) nextAssetBindingVersions(count int) ([]int64, error) {
	if count < 0 || s.assetBindingCounter < 0 || int64(count) > math.MaxInt64-s.assetBindingCounter {
		return nil, ErrAssetBindingFactsUnavailable
	}
	versions := make([]int64, count)
	for i := range versions {
		s.assetBindingCounter++
		versions[i] = s.assetBindingCounter
	}
	return versions, nil
}

func assetAuthorityJSON(a *UserAsset, version int64) (json.RawMessage, error) {
	if a == nil || version < 1 {
		return nil, ErrAssetBindingFactsUnavailable
	}
	return json.Marshal(map[string]any{
		"id": a.ID, "owner_user_id": a.OwnerID, "kind": a.Kind, "original_name": a.OriginalName,
		"storage_object_id": a.StorageObjectID, "mime_type": a.MimeType, "size_bytes": a.SizeBytes,
		"status": a.Status, "version": a.Version, "facts_version": version,
		"created_at": a.CreatedAt, "updated_at": a.UpdatedAt, "trashed_at": a.TrashedAt, "deleted_at": a.DeletedAt,
	})
}

func bindingAuthorityJSON(a *ArticleAttachment, version int64) (json.RawMessage, error) {
	if a == nil || version < 1 {
		return nil, ErrAssetBindingFactsUnavailable
	}
	return json.Marshal(map[string]any{
		"id": a.ID, "article_content_id": a.ArticleContentID, "asset_id": a.AssetID,
		"display_name": a.DisplayName, "display_order": a.DisplayOrder,
		"created_at": a.CreatedAt, "updated_at": a.UpdatedAt, "facts_version": version,
	})
}

func memoryAssetBindingID(id string) bool {
	return id != "" && len(id) <= 128 && strings.TrimSpace(id) == id
}

func (s *MemoryStore) CheckAssetBindingAuthority(ctx context.Context) error {
	if s == nil {
		return ErrAssetBindingFactsUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.assetFacts == nil || s.bindingFacts == nil {
		return ErrAssetBindingFactsUnavailable
	}
	return nil
}

func (s *MemoryStore) CheckAssetReferenceAuthority(ctx context.Context) error {
	return s.CheckAssetBindingAuthority(ctx)
}

func (s *MemoryStore) LoadAssetAuthorityFacts(ctx context.Context, id string, lock bool) (json.RawMessage, error) {
	if s == nil || !memoryAssetBindingID(id) || lock && !transaction.Active(ctx) {
		return nil, ErrAssetBindingFactsUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.assetFacts == nil || s.bindingFacts == nil {
		return nil, ErrAssetBindingFactsUnavailable
	}
	a, ok := s.userAssets[id]
	if !ok {
		return nil, ErrAssetNotFound
	}
	return assetAuthorityJSON(a, s.assetFacts[id])
}

func (s *MemoryStore) LoadArticleBindingAuthorityFacts(ctx context.Context, article string, lock bool) ([]AssetBindingAuthorityFact, error) {
	if s == nil || !memoryAssetBindingID(article) || lock && !transaction.Active(ctx) {
		return nil, ErrAssetBindingFactsUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.assetFacts == nil || s.bindingFacts == nil {
		return nil, ErrAssetBindingFactsUnavailable
	}
	bindings := []*ArticleAttachment{}
	for _, a := range s.attachments {
		if a.ArticleContentID == article {
			bindings = append(bindings, a)
		}
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].DisplayOrder == bindings[j].DisplayOrder {
			return bindings[i].ID < bindings[j].ID
		}
		return bindings[i].DisplayOrder < bindings[j].DisplayOrder
	})
	facts := make([]AssetBindingAuthorityFact, 0, len(bindings))
	for _, a := range bindings {
		binding, err := bindingAuthorityJSON(a, s.bindingFacts[a.ID])
		if err != nil {
			return nil, err
		}
		asset, err := assetAuthorityJSON(s.userAssets[a.AssetID], s.assetFacts[a.AssetID])
		if err != nil {
			return nil, err
		}
		facts = append(facts, AssetBindingAuthorityFact{Binding: binding, Asset: asset})
	}
	return facts, nil
}

// Restore current row facts while preserving every allocation ever observed.
// Old snapshots missing the hidden maps receive new allocations.
func (s *MemoryStore) restoreAssetBindingVersions() {
	if s.assetFacts == nil {
		s.assetFacts = map[string]int64{}
	}
	if s.bindingFacts == nil {
		s.bindingFacts = map[string]int64{}
	}
	for _, versions := range []map[string]int64{s.assetFacts, s.bindingFacts} {
		for _, version := range versions {
			if version > s.assetBindingCounter {
				s.assetBindingCounter = version
			}
		}
	}
	missingAssets, missingBindings := []string{}, []string{}
	for id := range s.userAssets {
		if s.assetFacts[id] < 1 {
			missingAssets = append(missingAssets, id)
		}
	}
	for id := range s.attachments {
		if s.bindingFacts[id] < 1 {
			missingBindings = append(missingBindings, id)
		}
	}
	sort.Strings(missingAssets)
	sort.Strings(missingBindings)
	versions, err := s.nextAssetBindingVersions(len(missingAssets) + len(missingBindings))
	if err != nil {
		// The original data remains intact; any missing proof closes readers.
		return
	}
	for i, id := range missingAssets {
		s.assetFacts[id] = versions[i]
	}
	for i, id := range missingBindings {
		s.bindingFacts[id] = versions[len(missingAssets)+i]
	}
}
