package space

import (
	"context"
	"sort"
	"time"
)

// Private aggregate facts omit titles, manifests, reasons and raw sync errors.
// Space23's full metadata nonce also binds changes outside these predicates.
type spaceAdminSummaryRow struct {
	ID, OwnerID                                 string
	FactsVersion                                int64
	Visibility                                  Visibility
	Disabled, Styled, SyncEnabled, HasSyncError bool
	LastSyncAt                                  *time.Time
}
type spaceAdminInventoryReader interface {
	LoadSpaceAdminInventory(context.Context) ([]spaceAdminSummaryRow, error)
}

func (r *MemoryRepository) LoadSpaceAdminInventory(ctx context.Context) ([]spaceAdminSummaryRow, error) {
	if err := r.CheckSpaceAuthority(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]spaceAdminSummaryRow, 0, len(r.spaces))
	for owner, space := range r.spaces {
		row := spaceAdminSummaryRow{ID: space.ID, OwnerID: owner, FactsVersion: space.FactsVersion, Visibility: space.Visibility, Disabled: space.DisabledAt != nil, Styled: space.StyleName != "", SyncEnabled: space.SyncEnabled, HasSyncError: space.LastSyncError != ""}
		if space.LastSyncAt != nil {
			at := space.LastSyncAt.UTC()
			row.LastSyncAt = &at
		}
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].OwnerID != result[j].OwnerID {
			return result[i].OwnerID < result[j].OwnerID
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}
func (r *PgRepository) LoadSpaceAdminInventory(ctx context.Context) ([]spaceAdminSummaryRow, error) {
	if err := r.CheckSpaceAuthority(ctx); err != nil {
		return nil, err
	}
	rows, err := r.db(ctx).Query(ctx, `SELECT id::text,user_id::text,facts_version,visibility,disabled_at IS NOT NULL,style_name<>'',sync_enabled,last_sync_error<>'',last_sync_at FROM user_spaces WHERE deleted_at IS NULL ORDER BY user_id::text,id::text`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []spaceAdminSummaryRow{}
	for rows.Next() {
		var row spaceAdminSummaryRow
		if err := rows.Scan(&row.ID, &row.OwnerID, &row.FactsVersion, &row.Visibility, &row.Disabled, &row.Styled, &row.SyncEnabled, &row.HasSyncError, &row.LastSyncAt); err != nil {
			return nil, err
		}
		if row.LastSyncAt != nil {
			at := row.LastSyncAt.UTC()
			row.LastSyncAt = &at
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.CheckSpaceAuthority(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
