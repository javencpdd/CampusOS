package richtext

import (
	"context"
	"time"
)

// Retain expired contexts for 24 hours to support the authenticated host
// download fallback. Only short-lived context metadata is deleted, never files.
func (s *PgStore) PruneInvocations(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit < 1 || limit > 500 {
		return 0, ErrAssetInvalid
	}
	tag, err := s.db(ctx).Exec(ctx, `WITH candidates AS (
		SELECT id FROM plugin_ui_invocations WHERE expires_at < $1
		ORDER BY expires_at,id LIMIT $2 FOR UPDATE SKIP LOCKED
	) DELETE FROM plugin_ui_invocations WHERE id IN (SELECT id FROM candidates)`, before, limit)
	return tag.RowsAffected(), err
}

func (s *MemoryStore) PruneInvocations(_ context.Context, before time.Time, limit int) (int64, error) {
	if limit < 1 || limit > 500 {
		return 0, ErrAssetInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	for id, v := range s.invocations {
		if count >= int64(limit) {
			break
		}
		if v.ExpiresAt.Before(before) {
			delete(s.invocations, id)
			count++
		}
	}
	return count, nil
}
