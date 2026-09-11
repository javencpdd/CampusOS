package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func scanSecret(row pgx.Row) (SecretMetadata, error) {
	var value SecretMetadata
	var metadata []byte
	err := row.Scan(&value.ID, &value.PluginID, &value.OwnerUserID, &value.SecretName, &value.KeyVersion, &value.Algorithm, &value.Ciphertext, &value.Nonce, &value.Status, &metadata, &value.CreatedAt, &value.RotatedAt, &value.RevokedAt)
	if err != nil {
		return SecretMetadata{}, normalizeAuthorizationRowError(err)
	}
	_ = json.Unmarshal(metadata, &value.Metadata)
	value.MaskedValue = "••••••••"
	return value, nil
}

const secretSelect = `SELECT id,plugin_id,owner_user_id,secret_name,key_version,algorithm,ciphertext,nonce,status,metadata,created_at,rotated_at,revoked_at FROM plugin_secret_values `

func (s *PgAuthorizationStore) PutSecret(ctx context.Context, value SecretMetadata) (SecretMetadata, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SecretMetadata{}, err
	}
	defer tx.Rollback(ctx)
	ownerKey := "system"
	if value.OwnerUserID != nil {
		ownerKey = fmt.Sprintf("%d", *value.OwnerUserID)
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("secret:%d:%s:%s", value.PluginID, ownerKey, value.SecretName)); err != nil {
		return SecretMetadata{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE plugin_secret_values SET status='rotated',rotated_at=NOW() WHERE plugin_id=$1 AND owner_user_id IS NOT DISTINCT FROM $2 AND secret_name=$3 AND status='active'`, value.PluginID, value.OwnerUserID, value.SecretName); err != nil {
		return SecretMetadata{}, err
	}
	metadata, _ := json.Marshal(nonNilMap(value.Metadata))
	saved, err := scanSecret(tx.QueryRow(ctx, `INSERT INTO plugin_secret_values (id,plugin_id,owner_user_id,secret_name,key_version,algorithm,ciphertext,nonce,status,metadata,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'active',$9::jsonb,$10) RETURNING id,plugin_id,owner_user_id,secret_name,key_version,algorithm,ciphertext,nonce,status,metadata,created_at,rotated_at,revoked_at`, value.ID, value.PluginID, value.OwnerUserID, value.SecretName, value.KeyVersion, value.Algorithm, value.Ciphertext, value.Nonce, string(metadata), value.CreatedAt))
	if err != nil {
		return SecretMetadata{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return SecretMetadata{}, err
	}
	return maskSecret(saved), nil
}
func (s *PgAuthorizationStore) ActiveSecret(ctx context.Context, pluginID int64, owner *int64, name string) (SecretMetadata, error) {
	return scanSecret(s.pool.QueryRow(ctx, secretSelect+`WHERE plugin_id=$1 AND owner_user_id IS NOT DISTINCT FROM $2 AND secret_name=$3 AND status='active' AND revoked_at IS NULL`, pluginID, owner, name))
}
func (s *PgAuthorizationStore) ListSecrets(ctx context.Context, pluginID int64, owner *int64) ([]SecretMetadata, error) {
	rows, err := s.pool.Query(ctx, secretSelect+`WHERE plugin_id=$1 AND owner_user_id IS NOT DISTINCT FROM $2 AND status='active' AND revoked_at IS NULL ORDER BY secret_name`, pluginID, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SecretMetadata{}
	for rows.Next() {
		value, scanErr := scanSecret(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, maskSecret(value))
	}
	return result, rows.Err()
}
func (s *PgAuthorizationStore) RevokeSecret(ctx context.Context, pluginID int64, owner *int64, name string) error {
	result, err := s.pool.Exec(ctx, `UPDATE plugin_secret_values SET status='revoked',revoked_at=NOW() WHERE plugin_id=$1 AND owner_user_id IS NOT DISTINCT FROM $2 AND secret_name=$3 AND status='active' AND revoked_at IS NULL`, pluginID, owner, name)
	if err == nil && result.RowsAffected() == 0 {
		return ErrMarketNotFound
	}
	return err
}

var _ SecretStore = (*PgAuthorizationStore)(nil)
