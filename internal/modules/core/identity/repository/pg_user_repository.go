package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgUserRepository PostgreSQL 用户仓储实现
type PgUserRepository struct {
	pool *pgxpool.Pool
}

// NewPgUserRepository 创建 PostgreSQL 用户仓储
func NewPgUserRepository(pool *pgxpool.Pool) *PgUserRepository {
	return &PgUserRepository{pool: pool}
}

func (r *PgUserRepository) Create(ctx context.Context, user *domain.User) error {
	user.Email = domain.NormalizeEmail(user.Email)
	if user.AuthVersion < 1 {
		user.AuthVersion = 1
	}
	query := `INSERT INTO users (id, username, nickname, email, avatar, bio, status, auth_version, must_change_password, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := transaction.ExecutorFor(ctx, r.pool).Exec(ctx, query,
		user.ID, user.Username, user.Nickname, user.Email,
		user.Avatar, user.Bio, user.Status, user.AuthVersion, user.MustChangePassword, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *PgUserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	query := `SELECT id, username, nickname, email, avatar, bio, status, auth_version, must_change_password, created_at, updated_at
		FROM users WHERE id = $1 AND deleted_at IS NULL`
	user := &domain.User{}
	err := transaction.ExecutorFor(ctx, r.pool).QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.Email,
		&user.Avatar, &user.Bio, &user.Status, &user.AuthVersion, &user.MustChangePassword, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user by id: %w", err)
	}
	return user, nil
}

func (r *PgUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	email = domain.NormalizeEmail(email)
	query := `SELECT id, username, nickname, email, avatar, bio, status, auth_version, must_change_password, created_at, updated_at
		FROM users WHERE lower(btrim(email)) = $1 AND deleted_at IS NULL`
	user := &domain.User{}
	err := transaction.ExecutorFor(ctx, r.pool).QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.Email,
		&user.Avatar, &user.Bio, &user.Status, &user.AuthVersion, &user.MustChangePassword, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user by email: %w", err)
	}
	return user, nil
}

func (r *PgUserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	query := `SELECT id, username, nickname, email, avatar, bio, status, auth_version, must_change_password, created_at, updated_at
		FROM users WHERE username = $1 AND deleted_at IS NULL`
	user := &domain.User{}
	err := transaction.ExecutorFor(ctx, r.pool).QueryRow(ctx, query, username).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.Email,
		&user.Avatar, &user.Bio, &user.Status, &user.AuthVersion, &user.MustChangePassword, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user by username: %w", err)
	}
	return user, nil
}

func (r *PgUserRepository) Update(ctx context.Context, user *domain.User) error {
	query := `UPDATE users SET nickname=$1, email=$2, avatar=$3, bio=$4, status=$5, updated_at=$6
		WHERE id = $7 AND deleted_at IS NULL`
	tag, err := transaction.ExecutorFor(ctx, r.pool).Exec(ctx, query,
		user.Nickname, user.Email, user.Avatar, user.Bio,
		user.Status, time.Now().UTC(), user.ID)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// BumpAuthVersion invalidates all previously issued access JWTs for a user.
// It is intentionally atomic and is used together with Session revocation by
// the Identity reliable command boundary.
func (r *PgUserRepository) BumpAuthVersion(ctx context.Context, id string) (*domain.User, error) {
	user := &domain.User{}
	err := transaction.ExecutorFor(ctx, r.pool).QueryRow(ctx, `UPDATE users
		SET auth_version=GREATEST(auth_version, 1)+1, updated_at=NOW()
		WHERE id=$1 AND deleted_at IS NULL
		RETURNING id, username, nickname, email, avatar, bio, status, auth_version, must_change_password, created_at, updated_at`, id).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.Email, &user.Avatar, &user.Bio,
		&user.Status, &user.AuthVersion, &user.MustChangePassword, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bump user auth version: %w", err)
	}
	return user, nil
}

func (r *PgUserRepository) List(ctx context.Context, page, pageSize int) ([]*domain.User, int64, error) {
	// 查询总数
	var total int64
	err := transaction.ExecutorFor(ctx, r.pool).QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE deleted_at IS NULL").Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	// 查询列表
	offset := (page - 1) * pageSize
	query := `SELECT id, username, nickname, email, avatar, bio, status, auth_version, must_change_password, created_at, updated_at
		FROM users WHERE deleted_at IS NULL ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := transaction.ExecutorFor(ctx, r.pool).Query(ctx, query, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		user := &domain.User{}
		if err := rows.Scan(&user.ID, &user.Username, &user.Nickname, &user.Email,
			&user.Avatar, &user.Bio, &user.Status, &user.AuthVersion, &user.MustChangePassword, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	return users, total, nil
}

// CreateVerifiedAccount creates a new email credential only after a
// registration Challenge Ticket has been consumed by the owning command.
func (r *PgUserRepository) CreateVerifiedAccount(ctx context.Context, userID, email, hashedPassword string) error {
	email = domain.NormalizeEmail(email)
	now := time.Now().UTC()
	query := `INSERT INTO accounts (
		id, user_id, type, identifier, identifier_normalized, credential, verified,
		verification_state, verified_at, verification_source, password_changed_at, credential_version, created_at, updated_at
	) VALUES ($1, $2, 'email', $3, $3, $4, true, 'verified', $5, 'registration_challenge', $5, 1, $5, $5)`
	_, err := transaction.ExecutorFor(ctx, r.pool).Exec(ctx, query, idgen.New(), userID, email, hashedPassword, now)
	return err
}

// DeleteForRegistration compensates a partially composed registration for
// legacy callers that have not yet installed TxKernel. When called inside a
// transaction it remains safe and the outer rollback still owns the result.
func (r *PgUserRepository) DeleteForRegistration(ctx context.Context, userID string) error {
	_, err := transaction.ExecutorFor(ctx, r.pool).Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	return err
}

// GetCredentialByEmail 通过邮箱获取密码哈希
func (r *PgUserRepository) GetCredentialByEmail(ctx context.Context, email string) (userID, credential string, err error) {
	email = domain.NormalizeEmail(email)
	query := `SELECT user_id, credential FROM accounts WHERE type='email' AND identifier_normalized=$1 AND deleted_at IS NULL`
	err = transaction.ExecutorFor(ctx, r.pool).QueryRow(ctx, query, email).Scan(&userID, &credential)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", ErrUserNotFound
		}
		return "", "", fmt.Errorf("query credential: %w", err)
	}
	return userID, credential, nil
}

func (r *PgUserRepository) GetEmailAccount(ctx context.Context, userID string) (*domain.EmailAccount, error) {
	account := &domain.EmailAccount{}
	query := `SELECT id, user_id, identifier_normalized, verification_state, verified_at,
		verification_source, credential_version, password_changed_at
		FROM accounts
		WHERE user_id=$1 AND type='email' AND deleted_at IS NULL`
	err := transaction.ExecutorFor(ctx, r.pool).QueryRow(ctx, query, userID).Scan(
		&account.ID, &account.UserID, &account.IdentifierNormalized, &account.VerificationState,
		&account.VerifiedAt, &account.VerificationSource, &account.CredentialVersion, &account.PasswordChangedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query email account: %w", err)
	}
	return account, nil
}

// UpdatePasswordForVerifiedEmail only accepts an already verified personal
// email account. The recovery command separately locks and consumes its
// Ticket before this narrow credential mutation joins the same transaction.
func (r *PgUserRepository) UpdatePasswordForVerifiedEmail(ctx context.Context, userID, email, credential string) error {
	email = domain.NormalizeEmail(email)
	now := time.Now().UTC()
	tag, err := transaction.ExecutorFor(ctx, r.pool).Exec(ctx, `WITH account AS (
		UPDATE accounts
		SET credential=$1, credential_version=GREATEST(credential_version, 1)+1,
			password_changed_at=$4, updated_at=$4
		WHERE user_id=$2 AND type='email' AND identifier_normalized=$3
			AND verification_state='verified' AND deleted_at IS NULL
		RETURNING user_id
	)
	UPDATE users SET must_change_password=FALSE, updated_at=$4
	WHERE id=(SELECT user_id FROM account) AND deleted_at IS NULL`, credential, userID, email, now)
	if err != nil {
		return mapCredentialMutationError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotEligible
	}
	return nil
}

// BindVerifiedEmail updates the compatibility users.email projection and the
// authoritative email account together. System-managed accounts deliberately
// cannot enter the public binding path.
func (r *PgUserRepository) BindVerifiedEmail(ctx context.Context, userID, email, source string) error {
	email = domain.NormalizeEmail(email)
	if email == "" || domain.IsReservedEmail(email) {
		return ErrAccountNotEligible
	}
	now := time.Now().UTC()
	tag, err := transaction.ExecutorFor(ctx, r.pool).Exec(ctx, `WITH account AS (
		UPDATE accounts
		SET identifier=$1, identifier_normalized=$1, verified=TRUE, verification_state='verified',
			verified_at=$4, verification_source=$3, updated_at=$4
		WHERE user_id=$2 AND type='email' AND deleted_at IS NULL
			AND verification_state <> 'system_managed'
		RETURNING user_id
	)
	UPDATE users SET email=$1, updated_at=$4
	WHERE id=(SELECT user_id FROM account) AND deleted_at IS NULL`, email, userID, source, now)
	if err != nil {
		return mapCredentialMutationError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotEligible
	}
	return nil
}

// RecoverAccountWithEmailAndPassword is limited to a legacy/unverified
// account selected by an administrator-approved RecoveryCase. It both binds
// the newly proven address and replaces the credential in the outer command.
func (r *PgUserRepository) RecoverAccountWithEmailAndPassword(ctx context.Context, userID, accountID, email, credential, source string) error {
	email = domain.NormalizeEmail(email)
	if email == "" || domain.IsReservedEmail(email) {
		return ErrAccountNotEligible
	}
	now := time.Now().UTC()
	tag, err := transaction.ExecutorFor(ctx, r.pool).Exec(ctx, `WITH account AS (
		UPDATE accounts
		SET identifier=$1, identifier_normalized=$1, credential=$4, verified=TRUE,
			verification_state='verified', verified_at=$6, verification_source=$5,
			password_changed_at=$6, credential_version=GREATEST(credential_version, 1)+1, updated_at=$6
		WHERE id=$2 AND user_id=$3 AND type='email' AND deleted_at IS NULL
			AND verification_state IN ('legacy_accepted', 'unverified')
		RETURNING user_id
	)
	UPDATE users SET email=$1, must_change_password=FALSE, updated_at=$6
	WHERE id=(SELECT user_id FROM account) AND deleted_at IS NULL`, email, accountID, userID, credential, source, now)
	if err != nil {
		return mapCredentialMutationError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotEligible
	}
	return nil
}

// UpdatePasswordForSystemManagedEmail exists solely for the local
// campusosctl recovery command. Public and Admin HTTP handlers never call it.
func (r *PgUserRepository) UpdatePasswordForSystemManagedEmail(ctx context.Context, userID, email, credential string) error {
	email = domain.NormalizeEmail(email)
	now := time.Now().UTC()
	tag, err := transaction.ExecutorFor(ctx, r.pool).Exec(ctx, `WITH account AS (
		UPDATE accounts
		SET credential=$1, credential_version=GREATEST(credential_version, 1)+1,
			password_changed_at=$4, updated_at=$4
		WHERE user_id=$2 AND type='email' AND identifier_normalized=$3
			AND verification_state='system_managed' AND deleted_at IS NULL
		RETURNING user_id
	)
	UPDATE users SET must_change_password=FALSE, updated_at=$4
	WHERE id=(SELECT user_id FROM account) AND deleted_at IS NULL`, credential, userID, email, now)
	if err != nil {
		return mapCredentialMutationError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotEligible
	}
	return nil
}

func mapCredentialMutationError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrEmailExists
	}
	return fmt.Errorf("mutate identity credential: %w", err)
}

var _ AccountCredentialMutator = (*PgUserRepository)(nil)
