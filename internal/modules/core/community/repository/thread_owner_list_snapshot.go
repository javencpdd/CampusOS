package repository

import (
	"context"
	"strconv"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

func ownerThreadListFilter(ownerID string, filter domain.ThreadListFilter) (domain.ThreadListFilter, error) {
	if len(ownerID) < 1 || len(ownerID) > 128 {
		return domain.ThreadListFilter{}, ErrThreadOwnerListInvalid
	}
	for i := range len(ownerID) {
		ch := ownerID[i]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' ||
			(i > 0 && (ch == '.' || ch == '_' || ch == ':' || ch == '-')) {
			continue
		}
		return domain.ThreadListFilter{}, ErrThreadOwnerListInvalid
	}
	filter.AuthorID = ownerID
	filter.Status = ""
	filter.PublicationStatus = ""
	filter.ModerationStatus = ""
	filter.DeletionStatus = string(domain.DeletionStatusActive)
	filter.IncludeTrashed = false
	return filter, nil
}

func (r *MemoryThreadRepository) ListOwnerThreadSnapshot(ctx context.Context, ownerID string, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	filter, err := ownerThreadListFilter(ownerID, filter)
	if err != nil {
		return nil, 0, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listThreadSnapshotLocked(filter, true)
}

func (r *PgThreadRepository) ListOwnerThreadSnapshot(ctx context.Context, ownerID string, filter domain.ThreadListFilter) ([]*domain.Thread, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	filter, err := ownerThreadListFilter(ownerID, filter)
	if err != nil {
		return nil, 0, err
	}
	owner, err := strconv.ParseInt(ownerID, 10, 64)
	if err != nil || owner < 1 {
		return nil, 0, ErrThreadOwnerListInvalid
	}
	if filter.CategoryID != "" {
		if _, err := parseBigIntIDs([]string{filter.CategoryID}); err != nil {
			return nil, 0, ErrThreadOwnerListInvalid
		}
	}
	return r.listThreadSnapshot(ctx, filter, []string{
		"publication_status IN ('draft', 'published', 'private')",
		"moderation_status IN ('clear', 'pending', 'rejected', 'taken_down')",
	})
}
