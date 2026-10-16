package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type userExecutionPG struct {
	s            *UserScopedExecutionAuthorityService
	pool         *pgxpool.Pool
	claims       *auth.JWTClaims
	proof        identityport.SessionPrincipalResolver
	query        identityport.UserScopedExecutionQuery
	role, ur, rp int64
	definition   int64
}

func userExecutionPostgres(t *testing.T, seed int) userExecutionPG {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("CAMPUSOS_AUTHOR_WRITE_TEST_DATABASE_URL is not configured")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "campusos_v12_02b_author_write" {
		t.Fatal("User execution requires owned test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	users := repository.NewPgUserRepository(pool)
	u := &domain.User{ID: fmt.Sprint(seed), Username: fmt.Sprintf("user-execution-%d", seed), Nickname: "User execution", Email: fmt.Sprintf("user-execution-%d@example.test", seed), Status: "active", AuthVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := users.CreateVerifiedAccount(ctx, u.ID, u.Email, "owned-user-execution-fixture-credential"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(152110,'User execution group','user-execution-group','group','active') ON CONFLICT(id) DO NOTHING;
INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(152111,'User execution board','user-execution-board',152110,'board','active'),(152112,'Other execution board','user-execution-other',152110,'board','active') ON CONFLICT(id) DO NOTHING;
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(152111,'discussion',TRUE),(152112,'discussion',TRUE) ON CONFLICT(category_id,thread_type) DO NOTHING;`); err != nil {
		t.Fatal(err)
	}
	g := userExecutionPG{pool: pool, role: int64(seed + 1), ur: int64(seed + 2), rp: int64(seed + 3), query: identityport.UserScopedExecutionQuery{PermissionCode: "community.thread.pin", Scope: identityport.PolicyResourceRef{Kind: "community.board", ID: "152111"}}}
	if err := pool.QueryRow(ctx, `SELECT id FROM permission_definitions WHERE code=$1 AND deprecated_at IS NULL`, g.query.PermissionCode).Scan(&g.definition); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO roles(id,name,description,is_system) VALUES($1,$2,'User execution',FALSE)`, g.role, fmt.Sprintf("user_execution_%d", seed)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_roles(id,user_id,role_id,scope_type,scope_id) VALUES($1,$2,$3,'category',152111)`, g.ur, u.ID, g.role); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO role_permissions(id,role_id,permission_id,created_by) VALUES($1,$2,$3,'user-execution')`, g.rp, g.role, g.definition); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "user-execution-owned-pg-test-key", Issuer: "user-execution", Audience: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour})
	sessions, err := NewSessionService(repository.NewPgSessionRepository(pool), users, jwt, SessionConfig{IPHashSecret: "user-execution-owned-pg-ip-key"})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := sessions.Issue(ctx, u, SessionMetadata{DeviceName: "User execution"})
	if err != nil {
		t.Fatal(err)
	}
	g.claims, err = jwt.VerifyAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	g.s = NewUserScopedExecutionAuthorityService(repository.NewPgRoleRepository(pool), sessions, repository.NewPgAdminAccountRepository(pool))
	g.proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveUserSessionPrincipal(ctx, sessions, sessions, g.claims)
	}
	return g
}

func TestUserScopedExecutionAuthorityPostgreSQLActualLocksAndLegacyAdmission(t *testing.T) {
	g := userExecutionPostgres(t, 152501)
	ctx := context.Background()
	before, err := g.s.LoadUserScopedExecutionAuthority(ctx, g.query, g.proof, false)
	if err != nil || !before.Allowed {
		t.Fatal(before, err)
	}
	if _, err := g.s.LoadUserScopedExecutionAuthority(ctx, g.query, g.proof, true); err == nil {
		t.Fatal("actual transaction required")
	}
	err = transaction.NewPostgreSQL(g.pool).Within(ctx, func(tx context.Context) error {
		a, err := g.s.LoadUserScopedExecutionAuthority(tx, g.query, g.proof, true)
		if err != nil {
			return err
		}
		for _, row := range []struct {
			sql string
			id  any
		}{
			{`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID},
			{`SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID},
			{`SELECT id FROM roles WHERE id=$1 FOR UPDATE NOWAIT`, g.role},
			{`SELECT id FROM user_roles WHERE id=$1 FOR UPDATE NOWAIT`, g.ur},
			{`SELECT id FROM role_permissions WHERE id=$1 FOR UPDATE NOWAIT`, g.rp},
			{`SELECT id FROM permission_definitions WHERE id=$1 FOR UPDATE NOWAIT`, g.definition},
		} {
			_, err := g.pool.Exec(ctx, row.sql, row.id)
			var pe *pgconn.PgError
			if !errors.As(err, &pe) || pe.Code != "55P03" {
				t.Fatal("actual User execution witness not held", row.sql, err)
			}
		}
		return identityport.CheckUserScopedExecutionAuthority(before, a)
	})
	if err != nil {
		t.Fatal(err)
	}
	// A legacy User admin role still requires its own admission. It does not
	// authenticate the independent management domain, even with the same ID.
	roles := repository.NewPgRoleRepository(g.pool)
	admin, err := roles.GetRoleByName(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roles.RevokeRole(ctx, g.claims.UserID, g.role); err != nil {
		t.Fatal(err)
	}
	if _, err := roles.AssignRole(ctx, g.claims.UserID, admin.ID, "global", nil); err != nil {
		t.Fatal(err)
	}
	missing, err := g.s.LoadUserScopedExecutionAuthority(ctx, g.query, g.proof, false)
	if err != nil || missing.Allowed {
		t.Fatal("legacy admission bypass", missing, err)
	}
	if _, err := g.s.admissions.EnsureActive(ctx, g.claims.UserID, "user-execution-fixture"); err != nil {
		t.Fatal(err)
	}
	if err := transaction.NewPostgreSQL(g.pool).Within(ctx, func(tx context.Context) error {
		a, err := g.s.LoadUserScopedExecutionAuthority(tx, g.query, g.proof, true)
		if err != nil || !a.Allowed || a.Principal.Actor.Kind != "user" {
			t.Fatal(a, err)
		}
		_, err = g.pool.Exec(ctx, `SELECT id FROM identity_admin_accounts WHERE user_id=$1 FOR UPDATE NOWAIT`, g.claims.UserID)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "55P03" {
			t.Fatal("legacy User role admission was not held", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUserScopedExecutionAuthorityPostgreSQLCommittedChanges(t *testing.T) {
	for i, mode := range []string{"role_metadata_aba", "scope_aba", "link_metadata_aba", "definition_metadata_aba", "role_revoke", "code_revoke", "scope_move", "user_version", "user_status", "session_revoke", "session_family", "session_strength", "guard_disabled"} {
		t.Run(mode, func(t *testing.T) {
			g := userExecutionPostgres(t, 152601+i*10)
			ctx := context.Background()
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 2 {
					tx, err := g.pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(ctx)
					update := func(sql string, args ...any) {
						if _, err := tx.Exec(ctx, sql, args...); err != nil {
							t.Fatal(mode, err)
						}
					}
					switch mode {
					case "role_metadata_aba":
						update(`UPDATE roles SET description='Temporary' WHERE id=$1`, g.role)
						update(`UPDATE roles SET description='User execution' WHERE id=$1`, g.role)
					case "scope_aba":
						update(`UPDATE user_roles SET scope_id=152112 WHERE id=$1`, g.ur)
						update(`UPDATE user_roles SET scope_id=152111 WHERE id=$1`, g.ur)
					case "link_metadata_aba":
						update(`UPDATE role_permissions SET created_by='Temporary' WHERE id=$1`, g.rp)
						update(`UPDATE role_permissions SET created_by='user-execution' WHERE id=$1`, g.rp)
					case "definition_metadata_aba":
						var original string
						if err := tx.QueryRow(ctx, `SELECT description FROM permission_definitions WHERE id=$1`, g.definition).Scan(&original); err != nil {
							t.Fatal(err)
						}
						update(`UPDATE permission_definitions SET description='Temporary' WHERE id=$1`, g.definition)
						update(`UPDATE permission_definitions SET description=$2 WHERE id=$1`, g.definition, original)
					case "role_revoke":
						update(`UPDATE user_roles SET deleted_at=NOW() WHERE id=$1`, g.ur)
					case "code_revoke":
						update(`UPDATE role_permissions SET deleted_at=NOW() WHERE id=$1`, g.rp)
					case "scope_move":
						update(`UPDATE user_roles SET scope_id=152112 WHERE id=$1`, g.ur)
					case "user_version":
						update(`UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, g.claims.UserID)
					case "user_status":
						update(`UPDATE users SET status='suspended' WHERE id=$1`, g.claims.UserID)
					case "session_revoke":
						update(`UPDATE sessions SET revoked_at=NOW(),revoke_reason='test' WHERE id=$1`, g.claims.SessionID)
					case "session_family":
						update(`UPDATE sessions SET token_family_id='changed-family' WHERE id=$1`, g.claims.SessionID)
					case "session_strength":
						update(`UPDATE sessions SET authentication_strength='mfa',mfa_authenticated_at=NOW() WHERE id=$1`, g.claims.SessionID)
					case "guard_disabled":
						update(`ALTER TABLE roles DISABLE TRIGGER trg_roles_user_scope_facts_version`)
						t.Cleanup(func() {
							if _, err := g.pool.Exec(context.Background(), `ALTER TABLE roles ENABLE TRIGGER trg_roles_user_scope_facts_version`); err != nil {
								t.Error(err)
							}
						})
					}
					if err := tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}
				return g.proof(ctx)
			}
			if a, err := g.s.LoadUserScopedExecutionAuthority(ctx, g.query, proof, false); err == nil {
				t.Fatal("committed authority change accepted", a)
			}
		})
	}
}
