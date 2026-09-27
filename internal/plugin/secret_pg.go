package plugin

import (
	"context"
	"encoding/json"
	"errors"
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

func lockSecretIdentity(ctx context.Context, tx pgx.Tx, pluginID int64, owner *int64, name string) error {
	ownerKey := "system"
	if owner != nil {
		ownerKey = fmt.Sprintf("%d", *owner)
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("secret:%d:%s:%s", pluginID, ownerKey, name))
	return err
}

func (s *PgAuthorizationStore) PutSecret(ctx context.Context, value SecretMetadata) (SecretMetadata, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SecretMetadata{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockSecretIdentity(ctx, tx, value.PluginID, value.OwnerUserID, value.SecretName); err != nil {
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

// ReplaceSecretIfCurrent changes only the encrypted envelope of the same
// active row. The identity lock serializes it with PutSecret and RevokeSecret;
// the row and old-envelope predicates make every stale read fail closed.
func (s *PgAuthorizationStore) ReplaceSecretIfCurrent(ctx context.Context, expected, replacement SecretMetadata) (SecretMetadata, error) {
	if !sameActiveSecretIdentity(expected, replacement) {
		return SecretMetadata{}, ErrSecretChanged
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SecretMetadata{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockSecretIdentity(ctx, tx, expected.PluginID, expected.OwnerUserID, expected.SecretName); err != nil {
		return SecretMetadata{}, err
	}
	saved, err := scanSecret(tx.QueryRow(ctx, `UPDATE plugin_secret_values
		SET key_version=$1,algorithm=$2,ciphertext=$3,nonce=$4
		WHERE id=$5 AND plugin_id=$6 AND owner_user_id IS NOT DISTINCT FROM $7
			AND secret_name=$8 AND status='active' AND revoked_at IS NULL
			AND key_version=$9 AND algorithm=$10 AND nonce=$11 AND ciphertext=$12
		RETURNING id,plugin_id,owner_user_id,secret_name,key_version,algorithm,ciphertext,nonce,status,metadata,created_at,rotated_at,revoked_at`,
		replacement.KeyVersion, replacement.Algorithm, replacement.Ciphertext, replacement.Nonce,
		expected.ID, expected.PluginID, expected.OwnerUserID, expected.SecretName,
		expected.KeyVersion, expected.Algorithm, expected.Nonce, expected.Ciphertext))
	if errors.Is(err, ErrMarketNotFound) {
		return SecretMetadata{}, ErrSecretChanged
	}
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
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockSecretIdentity(ctx, tx, pluginID, owner, name); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE plugin_secret_values SET status='revoked',revoked_at=NOW() WHERE plugin_id=$1 AND owner_user_id IS NOT DISTINCT FROM $2 AND secret_name=$3 AND status='active' AND revoked_at IS NULL`, pluginID, owner, name)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrMarketNotFound
	}
	return tx.Commit(ctx)
}

var _ SecretStore = (*PgAuthorizationStore)(nil)
