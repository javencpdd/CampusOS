package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgRecoveryCaseRepository struct{ pool *pgxpool.Pool }

func NewPgRecoveryCaseRepository(pool *pgxpool.Pool) *PgRecoveryCaseRepository {
	return &PgRecoveryCaseRepository{pool: pool}
}

func (r *PgRecoveryCaseRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgRecoveryCaseRepository) Create(ctx context.Context, value *domain.RecoveryCase) error {
	if value == nil || value.ID == "" || value.PublicID == "" || value.UserID == "" || value.AccountID == "" || value.ChallengeID == "" {
		return errors.New("identity recovery case requires ids")
	}
	id, err := parseRecoveryID(value.ID, "case")
	if err != nil {
		return err
	}
	userID, err := parseRecoveryID(value.UserID, "user")
	if err != nil {
		return err
	}
	accountID, err := parseRecoveryID(value.AccountID, "account")
	if err != nil {
		return err
	}
	challengeID, err := parseRecoveryID(value.ChallengeID, "challenge")
	if err != nil {
		return err
	}
	var createdBy any
	if value.CreatedBy != "" {
		parsed, parseErr := parseRecoveryID(value.CreatedBy, "actor")
		if parseErr != nil {
			return parseErr
		}
		createdBy = parsed
	}
	_, err = r.db(ctx).Exec(ctx, `INSERT INTO identity_account_recovery_cases (
		id, public_id, user_id, account_id, target_email_normalized, challenge_id, created_by, proof_reference,
		status, expires_at, completed_at, cancelled_at, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		id, value.PublicID, userID, accountID, value.TargetEmailNormalized, challengeID, createdBy, value.ProofReference,
		value.Status, value.ExpiresAt, value.CompletedAt, value.CancelledAt, value.CreatedAt, value.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert identity recovery case: %w", err)
	}
	return nil
}

func (r *PgRecoveryCaseRepository) GetByPublicID(ctx context.Context, publicID string) (*domain.RecoveryCase, error) {
	return r.queryOne(ctx, `SELECT id, public_id, user_id, account_id, target_email_normalized, challenge_id, created_by,
		proof_reference, status, expires_at, completed_at, cancelled_at, created_at, updated_at
		FROM identity_account_recovery_cases WHERE public_id=$1`, publicID)
}

func (r *PgRecoveryCaseRepository) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.RecoveryCase, error) {
	return r.queryOne(ctx, `SELECT id, public_id, user_id, account_id, target_email_normalized, challenge_id, created_by,
		proof_reference, status, expires_at, completed_at, cancelled_at, created_at, updated_at
		FROM identity_account_recovery_cases WHERE public_id=$1 FOR UPDATE`, publicID)
}

func (r *PgRecoveryCaseRepository) GetByChallengeIDForUpdate(ctx context.Context, challengeID string) (*domain.RecoveryCase, error) {
	id, err := parseRecoveryID(challengeID, "challenge")
	if err != nil {
		return nil, ErrRecoveryCaseNotFound
	}
	return r.queryOneByChallengeID(ctx, `SELECT id, public_id, user_id, account_id, target_email_normalized, challenge_id, created_by,
		proof_reference, status, expires_at, completed_at, cancelled_at, created_at, updated_at
		FROM identity_account_recovery_cases WHERE challenge_id=$1 FOR UPDATE`, id)
}

func (r *PgRecoveryCaseRepository) Update(ctx context.Context, value *domain.RecoveryCase) error {
	if value == nil || value.ID == "" {
		return ErrRecoveryCaseNotFound
	}
	id, err := parseRecoveryID(value.ID, "case")
	if err != nil {
		return err
	}
	tag, err := r.db(ctx).Exec(ctx, `UPDATE identity_account_recovery_cases SET
		status=$1, expires_at=$2, completed_at=$3, cancelled_at=$4, updated_at=$5
		WHERE id=$6`, value.Status, value.ExpiresAt, value.CompletedAt, value.CancelledAt, value.UpdatedAt, id)
	if err != nil {
		return fmt.Errorf("update identity recovery case: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRecoveryCaseNotFound
	}
	return nil
}

func (r *PgRecoveryCaseRepository) List(ctx context.Context, limit int) ([]*domain.RecoveryCase, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := r.db(ctx).Query(ctx, `SELECT id, public_id, user_id, account_id, target_email_normalized, challenge_id, created_by,
		proof_reference, status, expires_at, completed_at, cancelled_at, created_at, updated_at
		FROM identity_account_recovery_cases ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list identity recovery cases: %w", err)
	}
	defer rows.Close()
	items := make([]*domain.RecoveryCase, 0)
	for rows.Next() {
		item, scanErr := scanRecoveryCase(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgRecoveryCaseRepository) queryOne(ctx context.Context, query, publicID string) (*domain.RecoveryCase, error) {
	item, err := scanRecoveryCase(r.db(ctx).QueryRow(ctx, query, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRecoveryCaseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query identity recovery case: %w", err)
	}
	return item, nil
}

func (r *PgRecoveryCaseRepository) queryOneByChallengeID(ctx context.Context, query string, challengeID int64) (*domain.RecoveryCase, error) {
	item, err := scanRecoveryCase(r.db(ctx).QueryRow(ctx, query, challengeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRecoveryCaseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query identity recovery case: %w", err)
	}
	return item, nil
}

func scanRecoveryCase(row interface{ Scan(...any) error }) (*domain.RecoveryCase, error) {
	item := &domain.RecoveryCase{}
	var id, userID, accountID, challengeID int64
	var createdBy *int64
	if err := row.Scan(&id, &item.PublicID, &userID, &accountID, &item.TargetEmailNormalized, &challengeID, &createdBy,
		&item.ProofReference, &item.Status, &item.ExpiresAt, &item.CompletedAt, &item.CancelledAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.ID = strconv.FormatInt(id, 10)
	item.UserID = strconv.FormatInt(userID, 10)
	item.AccountID = strconv.FormatInt(accountID, 10)
	item.ChallengeID = strconv.FormatInt(challengeID, 10)
	if createdBy != nil {
		item.CreatedBy = strconv.FormatInt(*createdBy, 10)
	}
	return item, nil
}

func parseRecoveryID(value, label string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid recovery %s id", label)
	}
	return parsed, nil
}

var _ RecoveryCaseRepository = (*PgRecoveryCaseRepository)(nil)
