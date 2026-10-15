package richtext

import (
	"encoding/json"
	"strconv"
	"time"
)

// Decode the actual row and its nonce together. No separate model getter may
// supply fields from a different statement or replace the actual relationship.
func factRowFields(raw json.RawMessage, required []string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, ErrAssetBindingFactsUnavailable
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return nil, ErrAssetBindingFactsUnavailable
		}
	}
	var nonce int64
	if json.Unmarshal(fields["facts_version"], &nonce) != nil || nonce < 1 {
		return nil, ErrAssetBindingFactsUnavailable
	}
	return fields, nil
}

func normalizeFactID(fields map[string]json.RawMessage, name string) error {
	raw := fields[name]
	var id string
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &id) != nil || !memoryAssetBindingID(id) {
			return ErrAssetBindingFactsUnavailable
		}
	} else {
		value, err := strconv.ParseInt(string(raw), 10, 64)
		if err != nil || value <= 0 || strconv.FormatInt(value, 10) != string(raw) {
			return ErrAssetBindingFactsUnavailable
		}
		id = string(raw)
	}
	fields[name], _ = json.Marshal(id)
	return nil
}

func assetFromAuthorityFact(raw json.RawMessage) (*UserAsset, error) {
	fields, err := factRowFields(raw, []string{"id", "owner_user_id", "kind", "original_name", "storage_object_id", "mime_type", "size_bytes", "status", "version", "created_at", "updated_at", "trashed_at", "deleted_at"})
	if err != nil {
		return nil, err
	}
	for _, id := range []string{"id", "owner_user_id", "storage_object_id"} {
		if err := normalizeFactID(fields, id); err != nil {
			return nil, err
		}
	}
	fields["owner_id"] = fields["owner_user_id"]
	normalized, err := json.Marshal(fields)
	if err != nil {
		return nil, ErrAssetBindingFactsUnavailable
	}
	var a UserAsset
	if json.Unmarshal(normalized, &a) != nil || a.Version < 1 || a.SizeBytes < 0 {
		return nil, ErrAssetBindingFactsUnavailable
	}
	return canonicalReadAsset(&a), nil
}

func attachmentFromAuthorityFact(fact AssetBindingAuthorityFact) (*ArticleAttachment, error) {
	fields, err := factRowFields(fact.Binding, []string{"id", "article_content_id", "asset_id", "display_name", "display_order", "created_at", "updated_at"})
	if err != nil {
		return nil, err
	}
	for _, id := range []string{"id", "article_content_id", "asset_id"} {
		if err := normalizeFactID(fields, id); err != nil {
			return nil, err
		}
	}
	normalized, err := json.Marshal(fields)
	if err != nil {
		return nil, ErrAssetBindingFactsUnavailable
	}
	var a ArticleAttachment
	if json.Unmarshal(normalized, &a) != nil || a.DisplayOrder < 0 || a.DisplayName == "" {
		return nil, ErrAssetBindingFactsUnavailable
	}
	asset, err := assetFromAuthorityFact(fact.Asset)
	if err != nil {
		return nil, err
	}
	if a.AssetID != asset.ID {
		return nil, ErrAssetBindingFactsUnavailable
	}
	a.Asset = *asset
	a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
	return &a, nil
}

func canonicalReadAsset(a *UserAsset) *UserAsset {
	clone := cloneUserAsset(a)
	clone.CreatedAt, clone.UpdatedAt = clone.CreatedAt.UTC(), clone.UpdatedAt.UTC()
	for _, value := range []*time.Time{clone.TrashedAt, clone.DeletedAt} {
		if value != nil {
			*value = value.UTC()
		}
	}
	return clone
}
