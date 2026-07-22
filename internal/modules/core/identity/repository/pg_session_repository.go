package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgSessionRepository keeps refresh token material outside the application
// process log path. Queries intentionally select a digest only inside this
// package; HTTP handlers only receive a SessionView from the service layer.
type PgSessionRepository struct{ pool *pgxpool.Pool }

func NewPgSessionRepository(pool *pgxpool.Pool) *PgSessionRepository {
	return &PgSessionRepository{pool: pool}
}

func (r *PgSessionRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgSessionRepository) Create(ctx context.Context, session *domain.Session) error {
	if session == nil || session.ID == "" || session.UserID == "" || session.RefreshTokenDigest == "" || session.TokenFamilyID == "" {
		return errors.New("identity session requires id, user, refresh digest and family")
	}
	_, err := r.db(ctx).Exec(ctx, `INSERT INTO sessions (
		id, user_id, refresh_token, refresh_token_digest, token_family_id,
		rotated_from_id, rotated_to_id, device_id, device_name, device_type,
		ip_address, ip_hash, user_agent, authentication_strength, mfa_authenticated_at, last_active_at, expires_at,
		revoked_at, revoke_reason, created_at, updated_at
	) VALUES (
		$1, $2, NULL, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9,
		'', $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
	)`, session.ID, session.UserID, session.RefreshTokenDigest, session.TokenFamilyID,
		session.RotatedFromID, session.RotatedToID, session.DeviceID, session.DeviceName, session.DeviceType,
		session.IPHash, session.UserAgent, session.AuthenticationStrength, session.MFAAuthenticatedAt, session.LastActiveAt, session.ExpiresAt,
		session.RevokedAt, session.RevokeReason, session.CreatedAt, session.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert identity session: %w", err)
	}
	return nil
}

func (r *PgSessionRepository) GetByID(ctx context.Context, id string) (*domain.Session, error) {
	return r.querySession(ctx, `SELECT id, user_id, refresh_token_digest, token_family_id,
		rotated_from_id, rotated_to_id, device_id, device_name, device_type,
		ip_hash, user_agent, authentication_strength, mfa_authenticated_at, last_active_at, expires_at, revoked_at, revoke_reason, created_at, updated_at
		FROM sessions WHERE id=$1 AND deleted_at IS NULL`, id)
}

func (r *PgSessionRepository) GetByIDForUpdate(ctx context.Context, id string) (*domain.Session, error) {
	return r.querySession(ctx, `SELECT id, user_id, refresh_token_digest, token_family_id,
		rotated_from_id, rotated_to_id, device_id, device_name, device_type,
		ip_hash, user_agent, authentication_strength, mfa_authenticated_at, last_active_at, expires_at, revoked_at, revoke_reason, created_at, updated_at
		FROM sessions WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id)
}

func (r *PgSessionRepository) GetByRefreshDigestForUpdate(ctx context.Context, digest string) (*domain.Session, error) {
	return r.querySession(ctx, `SELECT id, user_id, refresh_token_digest, token_family_id,
		rotated_from_id, rotated_to_id, device_id, device_name, device_type,
		ip_hash, user_agent, authentication_strength, mfa_authenticated_at, last_active_at, expires_at, revoked_at, revoke_reason, created_at, updated_at
		FROM sessions WHERE refresh_token_digest=$1 AND deleted_at IS NULL FOR UPDATE`, digest)
}

func (r *PgSessionRepository) querySession(ctx context.Context, query string, value string) (*domain.Session, error) {
	session := &domain.Session{}
	var rotatedFromID, rotatedToID *string
	var revokedAt *time.Time
	err := r.db(ctx).QueryRow(ctx, query, value).Scan(
		&session.ID, &session.UserID, &session.RefreshTokenDigest, &session.TokenFamilyID,
		&rotatedFromID, &rotatedToID, &session.DeviceID, &session.DeviceName, &session.DeviceType,
		&session.IPHash, &session.UserAgent, &session.AuthenticationStrength, &session.MFAAuthenticatedAt, &session.LastActiveAt, &session.ExpiresAt, &revokedAt,
		&session.RevokeReason, &session.CreatedAt, &session.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query identity session: %w", err)
	}
	if rotatedFromID != nil {
		session.RotatedFromID = *rotatedFromID
	}
	if rotatedToID != nil {
		session.RotatedToID = *rotatedToID
	}
	session.RevokedAt = revokedAt
	return session, nil
}

func (r *PgSessionRepository) Update(ctx context.Context, session *domain.Session) error {
	if session == nil || session.ID == "" {
		return ErrSessionNotFound
	}
	tag, err := r.db(ctx).Exec(ctx, `UPDATE sessions SET
		refresh_token_digest=$1, token_family_id=$2, rotated_from_id=NULLIF($3, ''), rotated_to_id=NULLIF($4, ''),
		device_id=$5, device_name=$6, device_type=$7, ip_address='', ip_hash=$8, user_agent=$9,
		authentication_strength=$10, mfa_authenticated_at=$11, last_active_at=$12, expires_at=$13, revoked_at=$14, revoke_reason=$15, updated_at=$16
		WHERE id=$17 AND deleted_at IS NULL`,
		session.RefreshTokenDigest, session.TokenFamilyID, session.RotatedFromID, session.RotatedToID,
		session.DeviceID, session.DeviceName, session.DeviceType, session.IPHash, session.UserAgent,
		session.AuthenticationStrength, session.MFAAuthenticatedAt, session.LastActiveAt, session.ExpiresAt, session.RevokedAt, session.RevokeReason, session.UpdatedAt, session.ID)
	if err != nil {
		return fmt.Errorf("update identity session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (r *PgSessionRepository) ListByUser(ctx context.Context, userID string) ([]*domain.Session, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT id, user_id, refresh_token_digest, token_family_id,
		rotated_from_id, rotated_to_id, device_id, device_name, device_type,
		ip_hash, user_agent, authentication_strength, mfa_authenticated_at, last_active_at, expires_at, revoked_at, revoke_reason, created_at, updated_at
		FROM sessions WHERE user_id=$1 AND deleted_at IS NULL ORDER BY last_active_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list identity sessions: %w", err)
	}
	defer rows.Close()
	items := make([]*domain.Session, 0)
	for rows.Next() {
		session := &domain.Session{}
		var fromID, toID *string
		var revokedAt *time.Time
		if err := rows.Scan(&session.ID, &session.UserID, &session.RefreshTokenDigest, &session.TokenFamilyID,
			&fromID, &toID, &session.DeviceID, &session.DeviceName, &session.DeviceType, &session.IPHash,
			&session.UserAgent, &session.AuthenticationStrength, &session.MFAAuthenticatedAt, &session.LastActiveAt, &session.ExpiresAt, &revokedAt, &session.RevokeReason,
			&session.CreatedAt, &session.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan identity session: %w", err)
		}
		if fromID != nil {
			session.RotatedFromID = *fromID
		}
		if toID != nil {
			session.RotatedToID = *toID
		}
		session.RevokedAt = revokedAt
		items = append(items, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate identity sessions: %w", err)
	}
	return items, nil
}

func (r *PgSessionRepository) RevokeFamily(ctx context.Context, familyID, reason string, now time.Time) (int64, error) {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE sessions SET revoked_at=$1, revoke_reason=$2, updated_at=$1
		WHERE token_family_id=$3 AND revoked_at IS NULL AND deleted_at IS NULL`, now.UTC(), reason, familyID)
	if err != nil {
		return 0, fmt.Errorf("revoke identity session family: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *PgSessionRepository) RevokeByUser(ctx context.Context, userID, reason string, now time.Time) (int64, error) {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE sessions SET revoked_at=$1, revoke_reason=$2, updated_at=$1
		WHERE user_id=$3 AND revoked_at IS NULL AND deleted_at IS NULL`, now.UTC(), reason, userID)
	if err != nil {
		return 0, fmt.Errorf("revoke identity user sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

var _ SessionRepository = (*PgSessionRepository)(nil)
