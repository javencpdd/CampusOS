package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5"
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
	query := `INSERT INTO users (id, username, nickname, email, avatar, bio, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.pool.Exec(ctx, query,
		user.ID, user.Username, user.Nickname, user.Email,
		user.Avatar, user.Bio, user.Status, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *PgUserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	query := `SELECT id, username, nickname, email, avatar, bio, status, created_at, updated_at
		FROM users WHERE id = $1 AND deleted_at IS NULL`
	user := &domain.User{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.Email,
		&user.Avatar, &user.Bio, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user by id: %w", err)
	}
	return user, nil
}

func (r *PgUserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	query := `SELECT id, username, nickname, email, avatar, bio, status, created_at, updated_at
		FROM users WHERE email = $1 AND deleted_at IS NULL`
	user := &domain.User{}
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.Email,
		&user.Avatar, &user.Bio, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user by email: %w", err)
	}
	return user, nil
}

func (r *PgUserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	query := `SELECT id, username, nickname, email, avatar, bio, status, created_at, updated_at
		FROM users WHERE username = $1 AND deleted_at IS NULL`
	user := &domain.User{}
	err := r.pool.QueryRow(ctx, query, username).Scan(
		&user.ID, &user.Username, &user.Nickname, &user.Email,
		&user.Avatar, &user.Bio, &user.Status, &user.CreatedAt, &user.UpdatedAt)
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
	tag, err := r.pool.Exec(ctx, query,
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

func (r *PgUserRepository) List(ctx context.Context, page, pageSize int) ([]*domain.User, int64, error) {
	// 查询总数
	var total int64
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE deleted_at IS NULL").Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	// 查询列表
	offset := (page - 1) * pageSize
	query := `SELECT id, username, nickname, email, avatar, bio, status, created_at, updated_at
		FROM users WHERE deleted_at IS NULL ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, query, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		user := &domain.User{}
		if err := rows.Scan(&user.ID, &user.Username, &user.Nickname, &user.Email,
			&user.Avatar, &user.Bio, &user.Status, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	return users, total, nil
}

// CreateAccount 创建账号（密码等凭据）
func (r *PgUserRepository) CreateAccount(ctx context.Context, userID, email, hashedPassword string) error {
	query := `INSERT INTO accounts (id, user_id, type, identifier, credential, verified, created_at, updated_at)
		VALUES ($1, $2, 'email', $3, $4, false, $5, $5)`
	_, err := r.pool.Exec(ctx, query, idgen.New(), userID, email, hashedPassword, time.Now().UTC())
	return err
}

// GetCredentialByEmail 通过邮箱获取密码哈希
func (r *PgUserRepository) GetCredentialByEmail(ctx context.Context, email string) (userID, credential string, err error) {
	query := `SELECT user_id, credential FROM accounts WHERE type='email' AND identifier=$1 AND deleted_at IS NULL`
	err = r.pool.QueryRow(ctx, query, email).Scan(&userID, &credential)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", ErrUserNotFound
		}
		return "", "", fmt.Errorf("query credential: %w", err)
	}
	return userID, credential, nil
}
