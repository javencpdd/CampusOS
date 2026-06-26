package server

import (
	"context"
	"log"
	"time"

	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedAdmin 确保默认管理员账号存在
func SeedAdmin(pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 检查是否已有管理员
	var count int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE username = 'admin' AND deleted_at IS NULL`).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		log.Printf("✅ 管理员账号已存在，跳过种子数据")
		return nil
	}

	// 生成 bcrypt 哈希
	hashedPwd, err := auth.HashPassword("Admin@123456")
	if err != nil {
		return err
	}

	// 使用固定的雪花 ID
	adminUserID := int64(1000000000000000001)
	accountID := int64(1000000000000000002)
	roleMappingID := int64(1000000000000000003)

	// 插入管理员用户
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, username, nickname, email, avatar, bio, status, created_at, updated_at)
		VALUES ($1, 'admin', '系统管理员', 'admin@campusos.local', '', 'CampusOS 系统管理员', 'active', NOW(), NOW())
		ON CONFLICT (username) WHERE deleted_at IS NULL DO NOTHING`,
		adminUserID)
	if err != nil {
		return err
	}

	// 插入账号凭据
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, type, identifier, credential, verified, created_at, updated_at)
		VALUES ($1, $2, 'email', 'admin@campusos.local', $3, TRUE, NOW(), NOW())
		ON CONFLICT (type, identifier) WHERE deleted_at IS NULL DO NOTHING`,
		accountID, adminUserID, hashedPwd)
	if err != nil {
		return err
	}

	// 分配 admin 角色（role_id = 1）
	_, err = pool.Exec(ctx, `
		INSERT INTO user_roles (id, user_id, role_id, scope_type, created_at)
		VALUES ($1, $2, 1, 'global', NOW())
		ON CONFLICT (user_id, role_id, scope_type, scope_id) WHERE deleted_at IS NULL DO NOTHING`,
		roleMappingID, adminUserID)
	if err != nil {
		return err
	}

	log.Printf("✅ 默认管理员账号已创建")
	log.Printf("   邮箱: admin@campusos.local")
	log.Printf("   密码: Admin@123456")
	log.Printf("   角色: admin")
	return nil
}
