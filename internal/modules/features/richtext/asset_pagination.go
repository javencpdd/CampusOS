package richtext

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// AssetPageQuery uses the same stable (updated_at, bigint id) ordering in both
// stores. The cursor is a position, never an authorization credential.
type AssetPageQuery struct {
	BeforeTime time.Time
	BeforeID   string
	Limit      int
}

type UserAssetPage struct {
	Items      []*UserAsset `json:"items"`
	NextCursor string       `json:"next_cursor"`
	Limit      int          `json:"limit"`
}

type assetCursor struct {
	Owner  string    `json:"owner"`
	Status string    `json:"status"`
	Time   time.Time `json:"time"`
	ID     string    `json:"id"`
}

func (s *Service) ListMyUserAssetPage(ctx context.Context, userID, status, cursor string, limit int) (UserAssetPage, error) {
	if strings.TrimSpace(userID) == "" {
		return UserAssetPage{}, ErrPermissionDenied
	}
	if status == "" {
		status = AssetStatusActive
	}
	if status != AssetStatusActive && status != AssetStatusTrashed {
		return UserAssetPage{}, ErrAssetInvalid
	}
	if limit == 0 {
		limit = 200
	}
	if limit < 1 || limit > 200 {
		return UserAssetPage{}, ErrAssetInvalid
	}
	query := AssetPageQuery{Limit: limit + 1}
	if cursor != "" {
		if len(cursor) > 1024 {
			return UserAssetPage{}, ErrAssetInvalid
		}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		var position assetCursor
		if err != nil || json.Unmarshal(raw, &position) != nil {
			return UserAssetPage{}, ErrAssetInvalid
		}
		id, err := strconv.ParseInt(position.ID, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != position.ID || position.Time.IsZero() || position.Owner != userID || position.Status != status {
			return UserAssetPage{}, ErrAssetInvalid
		}
		query.BeforeTime, query.BeforeID = position.Time, position.ID
	}
	items, err := s.store.ListUserAssetsByOwner(ctx, userID, []string{status}, query)
	if err != nil {
		return UserAssetPage{}, err
	}
	page := UserAssetPage{Items: items, Limit: limit}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		raw, _ := json.Marshal(assetCursor{Owner: userID, Status: status, Time: last.UpdatedAt, ID: last.ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

func assetPageQuery(queries []AssetPageQuery) AssetPageQuery {
	if len(queries) == 0 {
		return AssetPageQuery{Limit: 200}
	}
	return queries[0]
}

func assetIDGreater(a, b string) bool {
	// Domain IDs are positive bigint decimal strings, not JavaScript numbers.
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	return a > b
}
