package plugin

import (
	"context"
	"sort"
)

func (s *MemorySecretStore) CountActiveSecretRewrapCandidates(ctx context.Context, pluginID int64, activeKeyID string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int64
	for _, value := range s.items {
		if value.PluginID == pluginID && value.Status == "active" && value.RevokedAt == nil && value.KeyVersion != activeKeyID {
			count++
		}
	}
	return count, nil
}

func (s *MemorySecretStore) ListActiveSecretRewrapCandidates(ctx context.Context, pluginID int64, activeKeyID string, limit int) ([]SecretRewrapCandidate, error) {
	if limit < 1 || limit > 500 {
		return nil, ErrSecretRewrapInventory
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	candidates := make([]SecretRewrapCandidate, 0)
	for _, value := range s.items {
		if value.PluginID != pluginID || value.Status != "active" || value.RevokedAt != nil || value.KeyVersion == activeKeyID {
			continue
		}
		candidate := SecretRewrapCandidate{ID: value.ID, PluginID: value.PluginID, SecretName: value.SecretName}
		if value.OwnerUserID != nil {
			owner := *value.OwnerUserID
			candidate.OwnerUserID = &owner
		}
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

var _ SecretRewrapInventory = (*MemorySecretStore)(nil)
