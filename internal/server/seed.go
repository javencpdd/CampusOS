package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5"
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

// adminSeedOptions separates the local compatibility path from the deployment
// bootstrap secret. It is intentionally process-only configuration: neither
// value is persisted outside the password credential hash.
type adminSeedOptions struct {
	Environment                  string
	PasswordHashEnabled          bool
	BootstrapAdminSecret         string
	AllowDevelopmentDefaultAdmin bool
}

func (o adminSeedOptions) developmentMode() bool {
	return strings.EqualFold(strings.TrimSpace(o.Environment), "development")
}

func (o adminSeedOptions) credentialForBootstrap() (credential, source string, err error) {
	secret := strings.TrimSpace(o.BootstrapAdminSecret)
	if secret != "" {
		credential, err = o.storedCredential(secret)
		return credential, "configured bootstrap secret", err
	}
	if o.developmentMode() && o.AllowDevelopmentDefaultAdmin {
		credential, err = o.storedCredential(defaultAdminPassword)
		return credential, "development compatibility credential", err
	}
	return "", "", errors.New("bootstrap administrator needs AUTH_BOOTSTRAP_ADMIN_SECRET; the legacy default credential is disabled outside explicit development mode")
}

func (o adminSeedOptions) storedCredential(value string) (string, error) {
	if !o.PasswordHashEnabled {
		return value, nil
	}
	return auth.HashPassword(value)
}

// SeedAdmin ensures that a bootstrap administrator and the default board exist.
// It never logs a password, secret, hash, or account recovery value. A changed
// administrator credential is preserved even when a bootstrap secret is set.
func SeedAdmin(pool *pgxpool.Pool, options adminSeedOptions) error {
	if pool == nil {
		return errors.New("database pool is required for administrator bootstrap")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin administrator bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	userID, outcome, err := ensureAdminAccount(ctx, tx, options)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_roles (id, user_id, role_id, scope_type, created_at)
		SELECT $1, $2, 1, 'global', NOW()
		WHERE NOT EXISTS (
			SELECT 1
			FROM user_roles
			WHERE user_id = $2
			  AND role_id = 1
			  AND scope_type = 'global'
			  AND scope_id IS NULL
			  AND deleted_at IS NULL
		)
		ON CONFLICT (id) DO NOTHING`,
		defaultAdminRoleMapID, userID,
	); err != nil {
		return fmt.Errorf("ensure administrator role: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO identity_admin_accounts (
			id, user_id, credential_account_id, status, activation_source,
			activated_at, version, created_at, updated_at
		)
		SELECT ur.id, ur.user_id, a.id, 'active', 'bootstrap_seed',
		       COALESCE(ur.created_at, NOW()), 1, COALESCE(ur.created_at, NOW()), NOW()
		FROM user_roles ur
		INNER JOIN accounts a
			ON a.user_id=ur.user_id AND a.type='email' AND a.deleted_at IS NULL
		WHERE ur.user_id=$1
		  AND ur.role_id=1
		  AND ur.scope_type='global'
		  AND ur.scope_id IS NULL
		  AND ur.deleted_at IS NULL
		ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
		return fmt.Errorf("ensure administrator management account: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit administrator bootstrap: %w", err)
	}

	if err := ensureDefaultCategory(ctx, pool); err != nil {
		return err
	}
	log.Printf("bootstrap administrator ready (%s); credentials were not logged", outcome)
	return nil
}

func ensureAdminAccount(ctx context.Context, tx pgx.Tx, options adminSeedOptions) (int64, string, error) {
	var accountUserID int64
	var existingCredential string
	err := tx.QueryRow(ctx, `
		SELECT user_id, credential
		FROM accounts
		WHERE type = 'email' AND identifier = $1 AND deleted_at IS NULL
		FOR UPDATE`, defaultAdminEmail,
	).Scan(&accountUserID, &existingCredential)
	if err == nil {
		var username string
		if err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, accountUserID).Scan(&username); err != nil {
			return 0, "", fmt.Errorf("read bootstrap administrator user: %w", err)
		}
		if username != "admin" {
			return 0, "", fmt.Errorf("bootstrap administrator email belongs to unexpected username %q", username)
		}
		if !isDefaultAdminCredential(existingCredential) {
			return accountUserID, "existing non-default credential preserved", nil
		}
		if options.developmentMode() && options.AllowDevelopmentDefaultAdmin && strings.TrimSpace(options.BootstrapAdminSecret) == "" {
			return accountUserID, "development compatibility credential remains active", nil
		}
		credential, source, err := options.credentialForBootstrap()
		if err != nil {
			return 0, "", err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE accounts
			SET credential = $1, verified = TRUE, updated_at = NOW()
			WHERE type = 'email' AND identifier = $2 AND deleted_at IS NULL`,
			credential, defaultAdminEmail,
		); err != nil {
			return 0, "", fmt.Errorf("rotate bootstrap administrator credential: %w", err)
		}
		return accountUserID, "legacy credential rotated using " + source, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", fmt.Errorf("read bootstrap administrator account: %w", err)
	}

	credential, source, err := options.credentialForBootstrap()
	if err != nil {
		return 0, "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (id, username, nickname, email, avatar, bio, status, created_at, updated_at)
		VALUES ($1, 'admin', '系统管理员', $2, '', 'CampusOS 系统管理员', 'active', NOW(), NOW())
		ON CONFLICT (username) WHERE deleted_at IS NULL DO NOTHING`,
		defaultAdminUserID, defaultAdminEmail,
	); err != nil {
		return 0, "", fmt.Errorf("create bootstrap administrator user: %w", err)
	}
	userID := defaultAdminUserID
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE username = 'admin' AND deleted_at IS NULL FOR UPDATE`).Scan(&userID); err != nil {
		return 0, "", fmt.Errorf("read bootstrap administrator user: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, user_id, type, identifier, credential, verified, created_at, updated_at)
		VALUES ($1, $2, 'email', $3, $4, TRUE, NOW(), NOW())`,
		defaultAdminAccountID, userID, defaultAdminEmail, credential,
	); err != nil {
		return 0, "", fmt.Errorf("create bootstrap administrator account: %w", err)
	}
	return userID, "administrator created using " + source, nil
}

func isDefaultAdminCredential(credential string) bool {
	credential = strings.TrimSpace(credential)
	if credential == defaultAdminPassword || credential == legacyAdminBadHash {
		return true
	}
	return auth.CheckPassword(defaultAdminPassword, credential)
}

func ensureDefaultCategory(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO categories (id, name, slug, description, sort_order, created_at, updated_at)
		VALUES ($1, '默认版块', 'default', '系统默认版块', 0, NOW(), NOW())
		ON CONFLICT (slug) WHERE deleted_at IS NULL DO NOTHING`,
		int64(1000000000000000004))
	if err != nil {
		return fmt.Errorf("ensure default category: %w", err)
	}

	log.Printf("default category ready")
	return nil
}
