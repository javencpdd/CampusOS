package server

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultAdminEmail     = "admin@campusos.local"
	defaultAdminPassword  = "Admin@123456"
	legacyAdminBadHash    = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	defaultAdminUserID    = int64(1000000000000000001)
	defaultAdminAccountID = int64(1000000000000000002)
	defaultAdminRoleMapID = int64(1000000000000000003)
)

// SeedAdmin 确保默认管理员账号存在
func SeedAdmin(pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := ensureDefaultCategory(ctx, pool); err != nil {
		return err
	}

	hashedPwd, err := auth.HashPassword(defaultAdminPassword)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, username, nickname, email, avatar, bio, status, created_at, updated_at)
		VALUES ($1, 'admin', '系统管理员', 'admin@campusos.local', '', 'CampusOS 系统管理员', 'active', NOW(), NOW())
		ON CONFLICT (username) WHERE deleted_at IS NULL DO NOTHING`,
		defaultAdminUserID)
	if err != nil {
		return err
	}

	adminUserID := defaultAdminUserID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM users WHERE username = 'admin' AND deleted_at IS NULL`,
	).Scan(&adminUserID); err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, type, identifier, credential, verified, created_at, updated_at)
		VALUES ($1, $2, 'email', 'admin@campusos.local', $3, TRUE, NOW(), NOW())
		ON CONFLICT (type, identifier) WHERE deleted_at IS NULL DO NOTHING`,
		defaultAdminAccountID, adminUserID, hashedPwd)
	if err != nil {
		return err
	}

	if err := fixLegacyAdminPassword(ctx, pool, hashedPwd); err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO user_roles (id, user_id, role_id, scope_type, created_at)
		VALUES ($1, $2, 1, 'global', NOW())
		ON CONFLICT (user_id, role_id, scope_type, scope_id) WHERE deleted_at IS NULL DO NOTHING`,
		defaultAdminRoleMapID, adminUserID)
	if err != nil {
		return err
	}

	log.Printf("✅ 默认管理员账号已就绪")
	log.Printf("   邮箱: %s", defaultAdminEmail)
	log.Printf("   默认密码: %s", defaultAdminPassword)
	log.Printf("   角色: admin")
	return nil
}

func fixLegacyAdminPassword(ctx context.Context, pool *pgxpool.Pool, hashedPwd string) error {
	var credential string
	err := pool.QueryRow(ctx, `
		SELECT credential FROM accounts
		WHERE type = 'email' AND identifier = $1 AND deleted_at IS NULL`,
		defaultAdminEmail,
	).Scan(&credential)
	if err != nil {
		return err
	}
	if strings.TrimSpace(credential) != legacyAdminBadHash {
		return nil
	}

	_, err = pool.Exec(ctx, `
		UPDATE accounts
		SET credential = $1, verified = TRUE, updated_at = NOW()
		WHERE type = 'email' AND identifier = $2 AND deleted_at IS NULL`,
		hashedPwd, defaultAdminEmail,
	)
	return err
}

func ensureDefaultCategory(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO categories (id, name, slug, description, sort_order, created_at, updated_at)
		VALUES ($1, '默认版块', 'default', '系统默认版块', 0, NOW(), NOW())
		ON CONFLICT (slug) WHERE deleted_at IS NULL DO NOTHING`,
		int64(1000000000000000004))
	if err != nil {
		return err
	}

	log.Printf("✅ 默认版块已就绪")
	return nil
}
