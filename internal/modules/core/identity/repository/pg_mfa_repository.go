package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgMFARepository keeps MFA state in Identity-owned tables. It receives only
// encrypted factor envelopes and one-way digests, never plaintext factor or
// recovery material.
type PgMFARepository struct{ pool *pgxpool.Pool }

func NewPgMFARepository(pool *pgxpool.Pool) *PgMFARepository { return &PgMFARepository{pool: pool} }

func (r *PgMFARepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgMFARepository) CreatePendingTOTP(ctx context.Context, method *MFATOTPMethod) error {
	if method == nil || method.ID == "" || method.UserID == "" || method.KeyID == "" || method.Nonce == "" || method.Ciphertext == "" {
		return errors.New("identity pending MFA method is incomplete")
	}
	db := r.db(ctx)
	if _, err := db.Exec(ctx, `DELETE FROM identity_mfa_totp_methods WHERE user_id=$1 AND status='pending'`, method.UserID); err != nil {
		return fmt.Errorf("delete pending MFA enrollment: %w", err)
	}
	_, err := db.Exec(ctx, `INSERT INTO identity_mfa_totp_methods (
		id,user_id,status,key_id,nonce,ciphertext,last_accepted_step,enrollment_expires_at,confirmed_at,disabled_at,created_at,updated_at
	) VALUES ($1,$2,'pending',$3,$4,$5,0,$6,NULL,NULL,$7,$7)`,
		method.ID, method.UserID, method.KeyID, method.Nonce, method.Ciphertext, method.EnrollmentExpiresAt.UTC(), method.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("insert pending MFA enrollment: %w", err)
	}
	return nil
}

func (r *PgMFARepository) GetPendingTOTPForUpdate(ctx context.Context, userID string) (*MFATOTPMethod, error) {
	return r.getTOTP(ctx, userID, MFAMethodPending, true)
}

func (r *PgMFARepository) GetActiveTOTP(ctx context.Context, userID string) (*MFATOTPMethod, error) {
	return r.getTOTP(ctx, userID, MFAMethodActive, false)
}

func (r *PgMFARepository) GetActiveTOTPForUpdate(ctx context.Context, userID string) (*MFATOTPMethod, error) {
	return r.getTOTP(ctx, userID, MFAMethodActive, true)
}

func (r *PgMFARepository) getTOTP(ctx context.Context, userID, status string, forUpdate bool) (*MFATOTPMethod, error) {
	query := `SELECT id::text,user_id::text,status,key_id,nonce,ciphertext,last_accepted_step,enrollment_expires_at,
		confirmed_at,disabled_at,created_at,updated_at
		FROM identity_mfa_totp_methods
		WHERE user_id=$1 AND status=$2
		ORDER BY created_at DESC,id DESC LIMIT 1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	method := &MFATOTPMethod{}
	err := r.db(ctx).QueryRow(ctx, query, userID, status).Scan(
		&method.ID, &method.UserID, &method.State, &method.KeyID, &method.Nonce, &method.Ciphertext, &method.LastAcceptedStep,
		&method.EnrollmentExpiresAt, &method.ConfirmedAt, &method.DisabledAt, &method.CreatedAt, &method.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMFAMethodNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get MFA method: %w", err)
	}
	return method, nil
}

func (r *PgMFARepository) ActivatePendingTOTP(ctx context.Context, userID, methodID string, step int64, at time.Time) error {
	db := r.db(ctx)
	if _, err := db.Exec(ctx, `UPDATE identity_mfa_totp_methods
		SET status='disabled',disabled_at=$2,updated_at=$2
		WHERE user_id=$1 AND status='active'`, userID, at.UTC()); err != nil {
		return fmt.Errorf("disable prior MFA method: %w", err)
	}
	tag, err := db.Exec(ctx, `UPDATE identity_mfa_totp_methods
		SET status='active',last_accepted_step=$3,confirmed_at=$4,updated_at=$4
		WHERE id=$1 AND user_id=$2 AND status='pending' AND enrollment_expires_at>$4`, methodID, userID, step, at.UTC())
	if err != nil {
		return fmt.Errorf("activate MFA method: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMFAMethodNotFound
	}
	return nil
}

func (r *PgMFARepository) DisableActiveTOTP(ctx context.Context, userID string, at time.Time) error {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE identity_mfa_totp_methods
		SET status='disabled',disabled_at=$2,updated_at=$2
		WHERE user_id=$1 AND status='active'`, userID, at.UTC())
	if err != nil {
		return fmt.Errorf("disable MFA method: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMFAMethodNotFound
	}
	return nil
}

func (r *PgMFARepository) AcceptTOTPStep(ctx context.Context, methodID string, step int64, at time.Time) (bool, error) {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE identity_mfa_totp_methods
		SET last_accepted_step=$2,updated_at=$3
		WHERE id=$1 AND status='active' AND last_accepted_step<$2`, methodID, step, at.UTC())
	if err != nil {
		return false, fmt.Errorf("accept MFA TOTP step: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PgMFARepository) CreateTicket(ctx context.Context, ticket *MFATicket) error {
	if ticket == nil || ticket.ID == "" || ticket.UserID == "" || ticket.TicketDigest == "" || ticket.MaxAttempts < 1 {
		return errors.New("identity MFA ticket is incomplete")
	}
	_, err := r.db(ctx).Exec(ctx, `INSERT INTO identity_mfa_tickets (
		id,user_id,audience,purpose,ticket_digest,expires_at,consumed_at,attempts,max_attempts,created_at,updated_at
	) VALUES ($1,$2,$3,$4,$5,$6,NULL,0,$7,$8,$8)`,
		ticket.ID, ticket.UserID, string(ticket.Audience), string(ticket.Purpose), ticket.TicketDigest, ticket.ExpiresAt.UTC(), ticket.MaxAttempts, ticket.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("insert MFA ticket: %w", err)
	}
	return nil
}

func (r *PgMFARepository) GetTicketForUpdate(ctx context.Context, digest string) (*MFATicket, error) {
	ticket := &MFATicket{}
	err := r.db(ctx).QueryRow(ctx, `SELECT id::text,user_id::text,audience,purpose,ticket_digest,expires_at,consumed_at,
		attempts,max_attempts,created_at,updated_at
		FROM identity_mfa_tickets WHERE ticket_digest=$1 FOR UPDATE`, digest).Scan(
		&ticket.ID, &ticket.UserID, &ticket.Audience, &ticket.Purpose, &ticket.TicketDigest, &ticket.ExpiresAt, &ticket.ConsumedAt,
		&ticket.Attempts, &ticket.MaxAttempts, &ticket.CreatedAt, &ticket.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMFATicketNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get MFA ticket: %w", err)
	}
	return ticket, nil
}

func (r *PgMFARepository) MarkTicketConsumed(ctx context.Context, digest string, at time.Time) (bool, error) {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE identity_mfa_tickets SET consumed_at=$2,updated_at=$2
		WHERE ticket_digest=$1 AND consumed_at IS NULL`, digest, at.UTC())
	if err != nil {
		return false, fmt.Errorf("consume MFA ticket: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PgMFARepository) RecordTicketFailure(ctx context.Context, digest string, at time.Time) (bool, error) {
	var blocked bool
	err := r.db(ctx).QueryRow(ctx, `UPDATE identity_mfa_tickets
		SET attempts=attempts+1,
			consumed_at=CASE WHEN attempts+1>=max_attempts THEN $2 ELSE consumed_at END,
			updated_at=$2
		WHERE ticket_digest=$1 AND consumed_at IS NULL
		RETURNING attempts>=max_attempts`, digest, at.UTC()).Scan(&blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("record MFA ticket failure: %w", err)
	}
	return blocked, nil
}

func (r *PgMFARepository) ReplaceRecoveryCodes(ctx context.Context, userID, methodID string, codes []MFARecoveryCode) error {
	db := r.db(ctx)
	if _, err := db.Exec(ctx, `DELETE FROM identity_mfa_recovery_codes WHERE user_id=$1`, userID); err != nil {
		return fmt.Errorf("delete MFA recovery codes: %w", err)
	}
	for _, code := range codes {
		if code.ID == "" || code.UserID != userID || code.MethodID != methodID || code.Digest == "" {
			return errors.New("identity MFA recovery code is incomplete")
		}
		if _, err := db.Exec(ctx, `INSERT INTO identity_mfa_recovery_codes (id,user_id,method_id,code_digest,used_at,created_at)
			VALUES ($1,$2,$3,$4,NULL,$5)`, code.ID, userID, methodID, code.Digest, code.CreatedAt.UTC()); err != nil {
			return fmt.Errorf("insert MFA recovery code: %w", err)
		}
	}
	return nil
}

func (r *PgMFARepository) ConsumeRecoveryCode(ctx context.Context, userID, digest string, at time.Time) (bool, error) {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE identity_mfa_recovery_codes SET used_at=$3
		WHERE user_id=$1 AND code_digest=$2 AND used_at IS NULL`, userID, digest, at.UTC())
	if err != nil {
		return false, fmt.Errorf("consume MFA recovery code: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PgMFARepository) CountRecoveryCodes(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db(ctx).QueryRow(ctx, `SELECT count(*) FROM identity_mfa_recovery_codes WHERE user_id=$1 AND used_at IS NULL`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count MFA recovery codes: %w", err)
	}
	return count, nil
}

func (r *PgMFARepository) GetPolicy(ctx context.Context) (*domain.MFAPolicy, error) {
	policy := &domain.MFAPolicy{}
	err := r.db(ctx).QueryRow(ctx, `SELECT id,mode,grace_ends_at,version,COALESCE(updated_by::text,''),updated_at
		FROM identity_mfa_policies WHERE id='admin'`).Scan(
		&policy.ID, &policy.Mode, &policy.GraceEndsAt, &policy.Version, &policy.UpdatedBy, &policy.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMFAPolicyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get MFA policy: %w", err)
	}
	return policy, nil
}

func (r *PgMFARepository) UpdatePolicy(ctx context.Context, policy domain.MFAPolicy, expectedVersion int64) (*domain.MFAPolicy, error) {
	updated := &domain.MFAPolicy{}
	err := r.db(ctx).QueryRow(ctx, `UPDATE identity_mfa_policies
		SET mode=$1,grace_ends_at=$2,updated_by=NULLIF($3,'')::bigint,updated_at=$4,version=version+1
		WHERE id='admin' AND version=$5
		RETURNING id,mode,grace_ends_at,version,COALESCE(updated_by::text,''),updated_at`,
		string(policy.Mode), policy.GraceEndsAt, strings.TrimSpace(policy.UpdatedBy), policy.UpdatedAt.UTC(), expectedVersion,
	).Scan(&updated.ID, &updated.Mode, &updated.GraceEndsAt, &updated.Version, &updated.UpdatedBy, &updated.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMFAPolicyConflict
	}
	if err != nil {
		return nil, fmt.Errorf("update MFA policy: %w", err)
	}
	return updated, nil
}

func (r *PgMFARepository) AdminCoverage(ctx context.Context) (domain.MFAAdminCoverage, error) {
	coverage := domain.MFAAdminCoverage{}
	err := r.db(ctx).QueryRow(ctx, `WITH active_admins AS (
		SELECT DISTINCT aa.user_id
		FROM identity_admin_accounts aa
		INNER JOIN users u ON u.id=aa.user_id AND u.status='active' AND u.deleted_at IS NULL
		INNER JOIN accounts a ON a.id=aa.credential_account_id AND a.user_id=aa.user_id AND a.type='email' AND a.deleted_at IS NULL
		INNER JOIN user_roles ur ON ur.user_id=aa.user_id AND ur.scope_type='global' AND ur.scope_id IS NULL AND ur.deleted_at IS NULL
		INNER JOIN roles role ON role.id=ur.role_id AND role.name='admin' AND role.deleted_at IS NULL
		WHERE aa.status='active'
	)
	SELECT count(*),count(method.user_id)
	FROM active_admins admin
	LEFT JOIN LATERAL (
		SELECT user_id FROM identity_mfa_totp_methods
		WHERE user_id=admin.user_id AND status='active' LIMIT 1
	) method ON TRUE`).Scan(&coverage.ActiveAdministrators, &coverage.MFAEnrolledAdministrators)
	if err != nil {
		return domain.MFAAdminCoverage{}, fmt.Errorf("calculate MFA admin coverage: %w", err)
	}
	return coverage, nil
}

var _ MFARepository = (*PgMFARepository)(nil)
