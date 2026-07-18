package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgAdminAccountRepository struct {
	pool *pgxpool.Pool
}

func NewPgAdminAccountRepository(pool *pgxpool.Pool) *PgAdminAccountRepository {
	return &PgAdminAccountRepository{pool: pool}
}

func (r *PgAdminAccountRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgAdminAccountRepository) IsActive(ctx context.Context, userID string) (bool, error) {
	var active bool
	err := r.db(ctx).QueryRow(ctx, `SELECT EXISTS (
		SELECT 1
		FROM identity_admin_accounts aa
		INNER JOIN users u ON u.id=aa.user_id AND u.deleted_at IS NULL AND u.status='active'
		INNER JOIN accounts a ON a.id=aa.credential_account_id AND a.user_id=aa.user_id AND a.deleted_at IS NULL
		WHERE aa.user_id=$1 AND aa.status='active'
	)`, userID).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("check administrator account admission: %w", err)
	}
	return active, nil
}

func (r *PgAdminAccountRepository) EnsureActive(ctx context.Context, userID, source string) (bool, error) {
	var changed bool
	err := r.db(ctx).QueryRow(ctx, `WITH credential AS (
		SELECT id
		FROM accounts
		WHERE user_id=$1 AND type='email' AND deleted_at IS NULL
		ORDER BY created_at ASC, id ASC
		LIMIT 1
	), upsert AS (
		INSERT INTO identity_admin_accounts (
			id,user_id,credential_account_id,status,activation_source,activated_at,version,created_at,updated_at
		)
		SELECT $2,$1,credential.id,'active',$3,NOW(),1,NOW(),NOW()
		FROM credential
		ON CONFLICT (user_id) DO UPDATE SET
			credential_account_id=EXCLUDED.credential_account_id,
			status=CASE WHEN identity_admin_accounts.status='suspended' THEN 'suspended' ELSE 'active' END,
			activation_source=CASE WHEN identity_admin_accounts.status='suspended' THEN identity_admin_accounts.activation_source ELSE EXCLUDED.activation_source END,
			activated_at=CASE WHEN identity_admin_accounts.status='revoked' THEN NOW() ELSE identity_admin_accounts.activated_at END,
			revoked_at=CASE WHEN identity_admin_accounts.status='suspended' THEN identity_admin_accounts.revoked_at ELSE NULL END,
			updated_at=NOW(),
			version=identity_admin_accounts.version+1
		RETURNING (status='active') AS active
	)
	SELECT COALESCE((SELECT bool_or(upsert.active) FROM upsert), FALSE)`, userID, idgen.New(), source).Scan(&changed)
	if err != nil {
		return false, fmt.Errorf("ensure administrator account admission: %w", err)
	}
	return changed, nil
}

func (r *PgAdminAccountRepository) Revoke(ctx context.Context, userID string) (bool, error) {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE identity_admin_accounts aa
		SET status='revoked',revoked_at=COALESCE(revoked_at,NOW()),updated_at=NOW(),version=version+1
		WHERE aa.user_id=$1
		  AND aa.status<>'revoked'
		  AND NOT EXISTS (
			SELECT 1
			FROM user_roles ur
			INNER JOIN roles r ON r.id=ur.role_id AND r.name='admin' AND r.deleted_at IS NULL
			WHERE ur.user_id=aa.user_id AND ur.scope_type='global' AND ur.scope_id IS NULL AND ur.deleted_at IS NULL
		  )`, userID)
	if err != nil {
		return false, fmt.Errorf("revoke administrator account admission: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PgAdminAccountRepository) MarkAuthenticated(ctx context.Context, userID string, at time.Time) error {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE identity_admin_accounts
		SET last_authenticated_at=$2,updated_at=NOW()
		WHERE user_id=$1 AND status='active'`, userID, at.UTC())
	if err != nil {
		return fmt.Errorf("record administrator authentication: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAdminAccountNotFound
	}
	return nil
}
