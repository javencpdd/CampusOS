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

type PgChallengePolicyRepository struct {
	pool *pgxpool.Pool
}

func NewPgChallengePolicyRepository(pool *pgxpool.Pool) *PgChallengePolicyRepository {
	return &PgChallengePolicyRepository{pool: pool}
}

func (r *PgChallengePolicyRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgChallengePolicyRepository) GetChallengePolicy(ctx context.Context) (*domain.ChallengePolicy, error) {
	policy := &domain.ChallengePolicy{}
	var updatedBy *int64
	err := r.db(ctx).QueryRow(ctx, `SELECT id, email_window_minutes, email_max_requests,
		ip_window_minutes, ip_max_requests, version, updated_by, updated_at
		FROM identity_challenge_policies WHERE id = $1`, domain.ChallengePolicyID).Scan(
		&policy.ID, &policy.EmailWindowMinutes, &policy.EmailMaxRequests,
		&policy.IPWindowMinutes, &policy.IPMaxRequests, &policy.Version, &updatedBy, &policy.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChallengePolicyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query identity challenge policy: %w", err)
	}
	if updatedBy != nil {
		policy.UpdatedBy = strconv.FormatInt(*updatedBy, 10)
	}
	return policy, nil
}

func (r *PgChallengePolicyRepository) UpdateChallengePolicy(ctx context.Context, policy *domain.ChallengePolicy, expectedVersion int64) error {
	if policy == nil || policy.ID != domain.ChallengePolicyID {
		return ErrChallengePolicyNotFound
	}
	var updatedBy any
	if policy.UpdatedBy != "" {
		parsed, err := strconv.ParseInt(policy.UpdatedBy, 10, 64)
		if err != nil {
			return fmt.Errorf("parse challenge policy actor: %w", err)
		}
		updatedBy = parsed
	}
	err := r.db(ctx).QueryRow(ctx, `UPDATE identity_challenge_policies SET
		email_window_minutes=$1, email_max_requests=$2, ip_window_minutes=$3, ip_max_requests=$4,
		version=version+1, updated_by=$5, updated_at=NOW()
		WHERE id=$6 AND version=$7
		RETURNING version, updated_at`, policy.EmailWindowMinutes, policy.EmailMaxRequests,
		policy.IPWindowMinutes, policy.IPMaxRequests, updatedBy, policy.ID, expectedVersion).Scan(
		&policy.Version, &policy.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChallengePolicyVersionConflict
	}
	if err != nil {
		return fmt.Errorf("update identity challenge policy: %w", err)
	}
	return nil
}

var _ ChallengePolicyRepository = (*PgChallengePolicyRepository)(nil)
