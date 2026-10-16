package service

import (
	"context"
	"errors"
	"fmt"
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

type governancePG struct {
	service         *BoardGovernanceAuthorityService
	pool            *pgxpool.Pool
	proof           identityport.SessionPrincipalResolver
	claims          *auth.JWTClaims
	query           identityport.BoardGovernanceQuery
	grantID, roleID string
}

func governancePostgres(t *testing.T, seed int, actor string) governancePG {
	t.Helper()
	admin, pool, adminClaims := adminCurrentPostgres(t, seed)
	ctx := context.Background()
	delegations := repository.NewPgDelegationRepository(pool)
	g := governancePG{pool: pool, query: identityport.BoardGovernanceQuery{BoardID: "118111", Action: "community.thread.take_down"}, service: NewBoardGovernanceAuthorityService(delegations, admin.admins, admin)}
	// The fixture has an actual board; this slice loads execution facts only,
	// and does not claim acceptance of any resource mutation or route.
	if _, err := pool.Exec(ctx, `INSERT INTO categories(id,name,slug,node_kind,lifecycle_status) VALUES(118110,'Governance group','governance-group','group','active') ON CONFLICT(id) DO NOTHING;
INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(118111,'Governance board','governance-board',118110,'board','active'),(118112,'Other governance board','governance-other-board',118110,'board','active') ON CONFLICT(id) DO NOTHING;
INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(118111,'discussion',TRUE),(118112,'discussion',TRUE) ON CONFLICT(category_id,thread_type) DO NOTHING;`); err != nil {
		t.Fatal(err)
	}
	if actor == "admin" {
		g.claims = adminClaims
		g.proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
			return identityport.ResolveAdminSessionPrincipal(ctx, admin, adminClaims)
		}
		g.roleID = fmt.Sprintf("governance_role_%d", seed)
		if _, err := pool.Exec(ctx, `INSERT INTO admin_roles(id,name,description,is_system) VALUES($1,$1,'Governance witness drill',FALSE)`, g.roleID); err != nil {
			t.Fatal(err)
		}
		if err := admin.admins.BindRole(ctx, repository.AdminDomainRoleBinding{ID: fmt.Sprintf("binding.gov.%d", seed), AccountID: adminClaims.UserID, RoleID: g.roleID, GrantedBy: adminClaims.UserID}); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,$2,'gov-test')`, g.roleID, g.query.Action); err != nil {
			t.Fatal(err)
		}
	} else {
		users := repository.NewPgUserRepository(pool)
		id := fmt.Sprint(seed)
		u := &domain.User{ID: id, Username: "gov-user-" + id, Email: "gov-user-" + id + "@example.test", Status: "active", AuthVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
		jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "governance-pg-user-test-key", Issuer: "governance-pg", Audience: "user", AccessTTL: time.Hour, RefreshTTL: time.Hour})
		sessions, err := NewSessionService(repository.NewPgSessionRepository(pool), users, jwt, SessionConfig{IPHashSecret: "governance-pg-ip-test-key"})
		if err != nil {
			t.Fatal(err)
		}
		tokens, err := sessions.Issue(ctx, u, SessionMetadata{DeviceName: "Governance"})
		if err != nil {
			t.Fatal(err)
		}
		g.claims, err = jwt.VerifyAccessToken(tokens.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		g.proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
			return identityport.ResolveUserSessionPrincipal(ctx, sessions, sessions, g.claims)
		}
		g.grantID = fmt.Sprintf("grant.gov.%d", seed)
		now := time.Now().UTC().Truncate(time.Microsecond)
		if err := delegations.InsertDelegations(ctx, []repository.Delegation{{ID: g.grantID, Kind: "grant", SubjectKind: "user", SubjectID: id, Action: g.query.Action, BoardID: g.query.BoardID, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

func TestBoardGovernanceAuthorityPostgreSQLActualLocks(t *testing.T) {
	for i, actor := range []string{"user", "admin"} {
		t.Run(actor, func(t *testing.T) {
			g := governancePostgres(t, 118201+i, actor)
			ctx := context.Background()
			if _, err := g.service.LoadBoardGovernanceAuthority(ctx, g.query, g.proof, true); !errors.Is(err, identityport.ErrGovernanceAuthorityUnavailable) {
				t.Fatal("lock outside actual transaction accepted", err)
			}
			err := transaction.NewPostgreSQL(g.pool).Within(ctx, func(txCtx context.Context) error {
				a, err := g.service.LoadBoardGovernanceAuthority(txCtx, g.query, g.proof, true)
				if err != nil {
					return err
				}
				if len(a.Grants) != 1 || a.Principal.Actor.Kind != actor || !decideGovernanceAuthority(t, a, g.query) {
					t.Fatal(a)
				}
				queries := []struct{ sql, id string }{{`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID}, {`SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID}, {`SELECT id FROM identity_delegations WHERE id=$1 FOR UPDATE NOWAIT`, g.grantID}}
				if actor == "admin" {
					queries = []struct{ sql, id string }{{`SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID}, {`SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID}, {`SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT`, g.roleID}, {`SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT`, g.roleID}, {`SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT`, g.roleID}}
				}
				for _, q := range queries {
					_, err := g.pool.Exec(ctx, q.sql, q.id)
					var pe *pgconn.PgError
					if !errors.As(err, &pe) || pe.Code != "55P03" {
						t.Fatal("current execution witness row not held", q.sql, err)
					}
				}
				current, err := g.service.LoadBoardGovernanceAuthority(txCtx, g.query, g.proof, true)
				if err != nil {
					return err
				}
				return identityport.CheckBoardGovernanceAuthority(a, current)
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBoardGovernanceAuthorityPostgreSQLCommittedChanges(t *testing.T) {
	modes := []string{"user/grant_revoke", "user/grant_aba", "user/grant_board", "user/grant_window", "user/grant_metadata", "user/session_revoke", "user/auth_version", "admin/permission_reinsert", "admin/permission_metadata_aba", "admin/binding_revoke", "admin/role_metadata", "admin/session_version", "admin/mfa_expire", "admin/account_aba"}
	for i, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			actor := "user"
			if mode[:5] == "admin" {
				actor = "admin"
			}
			g := governancePostgres(t, 118301+i, actor)
			ctx := context.Background()
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 2 {
					tx, err := g.pool.Begin(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(context.Background())
					exec := func(sql string, args ...any) {
						if _, e := tx.Exec(context.Background(), sql, args...); e != nil {
							t.Fatal(e)
						}
					}
					switch mode {
					case "user/grant_revoke":
						exec(`UPDATE identity_delegations SET status='revoked',revoked_at=NOW(),version=version+1 WHERE id=$1`, g.grantID)
					case "user/grant_aba":
						exec(`UPDATE identity_delegations SET status='suspended',version=version+1 WHERE id=$1`, g.grantID)
						exec(`UPDATE identity_delegations SET status='active',version=version+1 WHERE id=$1`, g.grantID)
					case "user/grant_board":
						exec(`UPDATE identity_delegations SET board_id='118112',version=version+1 WHERE id=$1`, g.grantID)
					case "user/grant_window":
						exec(`UPDATE identity_delegations SET expires_at=NOW(),version=version+1 WHERE id=$1`, g.grantID)
					case "user/grant_metadata":
						exec(`UPDATE identity_delegations SET created_by='changed' WHERE id=$1`, g.grantID)
					case "user/session_revoke":
						exec(`UPDATE sessions SET revoked_at=NOW(),revoke_reason='governance-test' WHERE id=$1`, g.claims.SessionID)
					case "user/auth_version":
						exec(`UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, g.claims.UserID)
					case "admin/permission_reinsert":
						exec(`DELETE FROM admin_role_permissions WHERE role_id=$1`, g.roleID)
						exec(`INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,$2,'gov-test')`, g.roleID, g.query.Action)
					case "admin/permission_metadata_aba":
						exec(`UPDATE admin_role_permissions SET created_by='changed' WHERE role_id=$1`, g.roleID)
						exec(`UPDATE admin_role_permissions SET created_by='gov-test' WHERE role_id=$1`, g.roleID)
					case "admin/binding_revoke":
						exec(`UPDATE admin_role_bindings SET status='revoked',revoked_at=NOW(),version=version+1 WHERE role_id=$1`, g.roleID)
					case "admin/role_metadata":
						exec(`UPDATE admin_roles SET description='Changed role' WHERE id=$1`, g.roleID)
					case "admin/session_version":
						exec(`UPDATE admin_sessions SET version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "admin/mfa_expire":
						exec(`UPDATE admin_sessions SET mfa_authenticated_at=NOW()-interval '16 minutes',version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "admin/account_aba":
						exec(`UPDATE admin_accounts SET status='suspended',version=version+1 WHERE id=$1`, g.claims.UserID)
						exec(`UPDATE admin_accounts SET status='active',version=version+1 WHERE id=$1`, g.claims.UserID)
					}
					if err := tx.Commit(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				return g.proof(ctx)
			}
			a, err := g.service.LoadBoardGovernanceAuthority(ctx, g.query, proof, false)
			if err == nil || a.Binding != nil || len(a.Grants) != 0 {
				t.Fatal("committed execution fact change accepted", a, err)
			}
		})
	}
}
