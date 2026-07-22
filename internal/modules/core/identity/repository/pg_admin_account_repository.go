package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5"
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
		SET status='revoked',revoked_at=COALESCE(revoked_at,NOW()),status_reason='admin_role_revoked',
			status_changed_by=NULL,status_changed_at=NOW(),updated_at=NOW(),version=version+1
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

func (r *PgAdminAccountRepository) List(ctx context.Context, filter AdminAccountListFilter) ([]AdminAccount, int64, error) {
	page, pageSize := normalizeAdminAccountPage(filter.Page, filter.PageSize)
	status := strings.TrimSpace(filter.Status)
	var total int64
	if err := r.db(ctx).QueryRow(ctx, `SELECT count(*) FROM identity_admin_accounts WHERE ($1='' OR status=$1)`, status).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count administrator accounts: %w", err)
	}
	rows, err := r.db(ctx).Query(ctx, adminAccountSelect+` WHERE ($1='' OR aa.status=$1)
		ORDER BY aa.updated_at DESC,aa.user_id ASC LIMIT $2 OFFSET $3`, status, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list administrator accounts: %w", err)
	}
	defer rows.Close()
	items := make([]AdminAccount, 0)
	for rows.Next() {
		item, scanErr := scanAdminAccount(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate administrator accounts: %w", err)
	}
	return items, total, nil
}

func (r *PgAdminAccountRepository) Get(ctx context.Context, userID string) (*AdminAccount, error) {
	item, err := scanAdminAccount(r.db(ctx).QueryRow(ctx, adminAccountSelect+` WHERE aa.user_id=$1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAdminAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get administrator account: %w", err)
	}
	return &item, nil
}

// Suspend uses a PostgreSQL transaction-scoped advisory lock to serialize the
// effective-admin aggregate. A row lock alone cannot protect two concurrent
// administrators attempting to suspend each other.
func (r *PgAdminAccountRepository) Suspend(ctx context.Context, userID string, expectedVersion int64, actorID, reason string, at time.Time) (*AdminAccount, error) {
	return r.transition(ctx, userID, expectedVersion, actorID, reason, at, AdminAccountStatusActive, AdminAccountStatusSuspended)
}

func (r *PgAdminAccountRepository) RestoreAdmission(ctx context.Context, userID string, expectedVersion int64, actorID, reason string, at time.Time) (*AdminAccount, error) {
	return r.transition(ctx, userID, expectedVersion, actorID, reason, at, AdminAccountStatusSuspended, AdminAccountStatusActive)
}

func (r *PgAdminAccountRepository) transition(ctx context.Context, userID string, expectedVersion int64, actorID, reason string, at time.Time, fromStatus, toStatus string) (*AdminAccount, error) {
	if expectedVersion < 1 {
		return nil, ErrAdminAccountVersionConflict
	}
	if transaction.Active(ctx) {
		return r.transitionWithin(ctx, userID, expectedVersion, actorID, reason, at, fromStatus, toStatus)
	}
	if r.pool == nil {
		return nil, errors.New("administrator account database is unavailable")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin administrator admission transition: %w", err)
	}
	defer tx.Rollback(ctx)
	item, err := r.transitionExecutor(ctx, tx, userID, expectedVersion, actorID, reason, at, fromStatus, toStatus)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit administrator admission transition: %w", err)
	}
	return item, nil
}

func (r *PgAdminAccountRepository) transitionWithin(ctx context.Context, userID string, expectedVersion int64, actorID, reason string, at time.Time, fromStatus, toStatus string) (*AdminAccount, error) {
	return r.transitionExecutor(ctx, r.db(ctx), userID, expectedVersion, actorID, reason, at, fromStatus, toStatus)
}

func (r *PgAdminAccountRepository) transitionExecutor(ctx context.Context, db transaction.Executor, userID string, expectedVersion int64, actorID, reason string, at time.Time, fromStatus, toStatus string) (*AdminAccount, error) {
	if _, err := db.Exec(ctx, `SELECT pg_advisory_xact_lock($1,$2)`, int64(130013), int64(39)); err != nil {
		return nil, fmt.Errorf("lock administrator admission transition: %w", err)
	}
	current, err := scanAdminAccount(db.QueryRow(ctx, adminAccountSelect+` WHERE aa.user_id=$1 FOR UPDATE`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAdminAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock administrator account: %w", err)
	}
	if current.Version != expectedVersion {
		return nil, ErrAdminAccountVersionConflict
	}
	if current.Status != fromStatus {
		return nil, ErrAdminAccountInvalidTransition
	}
	if fromStatus == AdminAccountStatusActive {
		var activeCount int
		if err := db.QueryRow(ctx, `SELECT count(*)
			FROM identity_admin_accounts aa
			INNER JOIN users u ON u.id=aa.user_id AND u.deleted_at IS NULL AND u.status='active'
			INNER JOIN accounts a ON a.id=aa.credential_account_id AND a.user_id=aa.user_id AND a.deleted_at IS NULL AND a.type='email'
			INNER JOIN user_roles ur ON ur.user_id=aa.user_id AND ur.scope_type='global' AND ur.scope_id IS NULL AND ur.deleted_at IS NULL
			INNER JOIN roles role ON role.id=ur.role_id AND role.name='admin' AND role.deleted_at IS NULL
			WHERE aa.status='active'`).Scan(&activeCount); err != nil {
			return nil, fmt.Errorf("count effective administrator accounts: %w", err)
		}
		if activeCount <= 1 {
			return nil, ErrLastActiveAdministrator
		}
	}
	at = at.UTC()
	activationSource := current.ActivationSource
	if toStatus == AdminAccountStatusActive {
		activationSource = "admin_restore"
	}
	row := db.QueryRow(ctx, `UPDATE identity_admin_accounts
		SET status=$2, activation_source=$3, status_reason=$4,
			status_changed_by=NULLIF($5,'')::bigint, status_changed_at=$6,
			updated_at=$6, version=version+1
		WHERE user_id=$1 AND version=$7
		RETURNING id::text,user_id::text,credential_account_id::text,status,activation_source,activated_at,revoked_at,
			last_authenticated_at,status_reason,COALESCE(status_changed_by::text,''),status_changed_at,version,created_at,updated_at`,
		userID, toStatus, activationSource, strings.TrimSpace(reason), strings.TrimSpace(actorID), at, expectedVersion)
	updated, err := scanAdminAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAdminAccountVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("update administrator account admission: %w", err)
	}
	return &updated, nil
}

const adminAccountSelect = `SELECT aa.id::text,aa.user_id::text,aa.credential_account_id::text,aa.status,aa.activation_source,
	aa.activated_at,aa.revoked_at,aa.last_authenticated_at,COALESCE(aa.status_reason,''),
	COALESCE(aa.status_changed_by::text,''),aa.status_changed_at,aa.version,aa.created_at,aa.updated_at
	FROM identity_admin_accounts aa`

func scanAdminAccount(row interface{ Scan(...any) error }) (AdminAccount, error) {
	item := AdminAccount{}
	err := row.Scan(&item.ID, &item.UserID, &item.CredentialAccountID, &item.Status, &item.ActivationSource,
		&item.ActivatedAt, &item.RevokedAt, &item.LastAuthenticatedAt, &item.StatusReason,
		&item.StatusChangedBy, &item.StatusChangedAt, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

var _ AdminAccountRepository = (*PgAdminAccountRepository)(nil)
