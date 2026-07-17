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

type PgChallengeRepository struct {
	pool *pgxpool.Pool
}

func NewPgChallengeRepository(pool *pgxpool.Pool) *PgChallengeRepository {
	return &PgChallengeRepository{pool: pool}
}

func (r *PgChallengeRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgChallengeRepository) TryConsumeRate(ctx context.Context, windows []domain.ChallengeRateWindow) (bool, error) {
	for _, window := range windows {
		if window.Limit <= 0 || window.Scope == "" || window.SubjectDigest == "" || window.WindowStart.IsZero() {
			return false, errors.New("invalid challenge rate window")
		}
		var count int
		err := r.db(ctx).QueryRow(ctx, `INSERT INTO identity_challenge_rate_limits
			(scope, subject_digest, window_started_at, request_count, updated_at)
			VALUES ($1, $2, $3, 1, NOW())
			ON CONFLICT (scope, subject_digest, window_started_at)
			DO UPDATE SET request_count = identity_challenge_rate_limits.request_count + 1, updated_at = NOW()
			WHERE identity_challenge_rate_limits.request_count < $4
			RETURNING request_count`, window.Scope, window.SubjectDigest, window.WindowStart.UTC(), window.Limit).Scan(&count)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("consume challenge rate limit: %w", err)
		}
	}
	return true, nil
}

func (r *PgChallengeRepository) CreateChallenge(ctx context.Context, challenge *domain.EmailChallenge) error {
	if challenge == nil {
		return errors.New("challenge is required")
	}
	var accountID any
	if challenge.AccountID != "" {
		parsed, err := strconv.ParseInt(challenge.AccountID, 10, 64)
		if err != nil {
			return fmt.Errorf("parse challenge account id: %w", err)
		}
		accountID = parsed
	}
	_, err := r.db(ctx).Exec(ctx, `INSERT INTO identity_email_challenges (
		id, public_id, purpose, email_normalized, account_id, key_id, nonce, expires_at,
		attempt_count, max_attempts, requested_ip_hash, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		challenge.ID, challenge.PublicID, challenge.Purpose, challenge.EmailNormalized, accountID,
		challenge.KeyID, challenge.Nonce, challenge.ExpiresAt, challenge.AttemptCount, challenge.MaxAttempts,
		challenge.RequestedIPHash, challenge.CreatedAt, challenge.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert identity email challenge: %w", err)
	}
	return nil
}

func (r *PgChallengeRepository) GetChallengeForUpdate(ctx context.Context, publicID string) (*domain.EmailChallenge, error) {
	return r.queryChallenge(ctx, `SELECT id, public_id, purpose, email_normalized, account_id, key_id, nonce, expires_at,
		attempt_count, max_attempts, verified_at, ticket_digest, ticket_expires_at, consumed_at, invalidated_at,
		requested_ip_hash, created_at, updated_at
		FROM identity_email_challenges WHERE public_id = $1 FOR UPDATE`, publicID)
}

func (r *PgChallengeRepository) GetChallenge(ctx context.Context, publicID string) (*domain.EmailChallenge, error) {
	return r.queryChallenge(ctx, `SELECT id, public_id, purpose, email_normalized, account_id, key_id, nonce, expires_at,
		attempt_count, max_attempts, verified_at, ticket_digest, ticket_expires_at, consumed_at, invalidated_at,
		requested_ip_hash, created_at, updated_at
		FROM identity_email_challenges WHERE public_id = $1`, publicID)
}

func (r *PgChallengeRepository) GetChallengeByID(ctx context.Context, id string) (*domain.EmailChallenge, error) {
	return r.queryChallenge(ctx, `SELECT id, public_id, purpose, email_normalized, account_id, key_id, nonce, expires_at,
		attempt_count, max_attempts, verified_at, ticket_digest, ticket_expires_at, consumed_at, invalidated_at,
		requested_ip_hash, created_at, updated_at
		FROM identity_email_challenges WHERE id = $1`, id)
}

func (r *PgChallengeRepository) queryChallenge(ctx context.Context, query, publicID string) (*domain.EmailChallenge, error) {
	challenge := &domain.EmailChallenge{}
	var accountID *int64
	var ticketDigest *string
	err := r.db(ctx).QueryRow(ctx, query, publicID).Scan(
		&challenge.ID, &challenge.PublicID, &challenge.Purpose, &challenge.EmailNormalized, &accountID,
		&challenge.KeyID, &challenge.Nonce, &challenge.ExpiresAt, &challenge.AttemptCount, &challenge.MaxAttempts,
		&challenge.VerifiedAt, &ticketDigest, &challenge.TicketExpiresAt, &challenge.ConsumedAt, &challenge.InvalidatedAt,
		&challenge.RequestedIPHash, &challenge.CreatedAt, &challenge.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChallengeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query identity email challenge: %w", err)
	}
	if accountID != nil {
		challenge.AccountID = strconv.FormatInt(*accountID, 10)
	}
	if ticketDigest != nil {
		challenge.TicketDigest = *ticketDigest
	}
	return challenge, nil
}

func (r *PgChallengeRepository) UpdateChallenge(ctx context.Context, challenge *domain.EmailChallenge) error {
	if challenge == nil || challenge.ID == "" {
		return errors.New("challenge id is required")
	}
	challenge.UpdatedAt = challenge.UpdatedAt.UTC()
	tag, err := r.db(ctx).Exec(ctx, `UPDATE identity_email_challenges SET
		attempt_count=$1, verified_at=$2, ticket_digest=NULLIF($3, ''), ticket_expires_at=$4,
		consumed_at=$5, invalidated_at=$6, updated_at=$7
		WHERE id=$8`, challenge.AttemptCount, challenge.VerifiedAt, challenge.TicketDigest,
		challenge.TicketExpiresAt, challenge.ConsumedAt, challenge.InvalidatedAt, challenge.UpdatedAt, challenge.ID)
	if err != nil {
		return fmt.Errorf("update identity email challenge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChallengeNotFound
	}
	return nil
}

var _ ChallengeRepository = (*PgChallengeRepository)(nil)
