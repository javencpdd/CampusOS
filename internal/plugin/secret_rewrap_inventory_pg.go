package plugin

import "context"

func (s *PgAuthorizationStore) CountActiveSecretRewrapCandidates(ctx context.Context, pluginID int64, activeKeyID string) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM plugin_secret_values
		WHERE plugin_id=$1 AND status='active' AND revoked_at IS NULL AND key_version<>$2`, pluginID, activeKeyID).Scan(&count)
	return count, err
}

func (s *PgAuthorizationStore) ListActiveSecretRewrapCandidates(ctx context.Context, pluginID int64, activeKeyID string, limit int) ([]SecretRewrapCandidate, error) {
	if limit < 1 || limit > 500 {
		return nil, ErrSecretRewrapInventory
	}
	rows, err := s.pool.Query(ctx, `SELECT id,plugin_id,owner_user_id,secret_name FROM plugin_secret_values
		WHERE plugin_id=$1 AND status='active' AND revoked_at IS NULL AND key_version<>$2
		ORDER BY id LIMIT $3`, pluginID, activeKeyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]SecretRewrapCandidate, 0)
	for rows.Next() {
		var candidate SecretRewrapCandidate
		if err := rows.Scan(&candidate.ID, &candidate.PluginID, &candidate.OwnerUserID, &candidate.SecretName); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

var _ SecretRewrapInventory = (*PgAuthorizationStore)(nil)
