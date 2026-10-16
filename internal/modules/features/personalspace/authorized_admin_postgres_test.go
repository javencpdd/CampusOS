package space

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

type spaceAdminPGData struct {
	s                  *Service
	repo               *PgRepository
	pool               *pgxpool.Pool
	proof              identityport.SessionPrincipalResolver
	claims             *auth.JWTClaims
	owner, actor, role string
}

func spaceAdminPostgres(t *testing.T, seed int, existing, opaque bool) spaceAdminPGData {
	t.Helper()
	ctx := context.Background()
	owner := fmt.Sprint(seed)
	s, repo, _, pool, _, _ := filesPostgres(t, owner)
	if !existing {
		if _, err := pool.Exec(ctx, "DELETE FROM user_spaces WHERE user_id=$1", owner); err != nil {
			t.Fatal(err)
		}
	}
	g := spaceAdminPGData{s: s, repo: repo, pool: pool, owner: owner, actor: owner, role: "space_admin_" + owner}
	if opaque {
		g.actor = "adm.space." + owner
	}
	admins, sessions := identityrepo.NewPgAdminDomainRepository(pool), identityrepo.NewPgAdminSessionRepository(pool)
	if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: g.actor, Username: "space-admin-" + owner, Email: "space-admin-" + owner + "@example.test"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	at := now.Add(-time.Minute)
	sid := "adms.space." + owner
	if err := sessions.CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: g.actor, RefreshTokenDigest: fmt.Sprintf("%064x", seed), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &at, CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "space-admin-pg-test-key", Issuer: "space-admin-pg", Audience: "admin", AccessTTL: time.Hour})
	current := identityservice.NewAdminSessionService(admins, sessions, jwt)
	token, err := jwt.GenerateAccessToken(g.actor, "Admin", auth.AccessTokenContext{SessionID: sid, AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	g.claims, err = jwt.VerifyAccessToken(token)
	if err != nil {
		t.Fatal(err)
	}
	g.proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
		return identityport.ResolveAdminSessionPrincipal(ctx, current, g.claims)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO admin_roles(id,name,description,is_system)VALUES($1,$1,'Space admin test',FALSE)", g.role); err != nil {
		t.Fatal(err)
	}
	if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.space." + owner, AccountID: g.actor, RoleID: g.role, GrantedBy: g.actor}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO admin_role_permissions(role_id,permission_code,created_by)VALUES($1,'personal_space.space.manage','test')", g.role); err != nil {
		t.Fatal(err)
	}
	s.SetAdminExecutionAuthority(identityservice.NewBoardGovernanceAuthorityService(nil, admins, current))
	s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	return g
}

type spaceAdminPGHook struct {
	*PgRepository
	afterLock  func(context.Context)
	afterWrite func(context.Context)
	afterRead  func()
}

func (r *spaceAdminPGHook) LoadSpaceAuthority(ctx context.Context, owner string, lock bool) (json.RawMessage, error) {
	raw, err := r.PgRepository.LoadSpaceAuthority(ctx, owner, lock)
	if err == nil || errors.Is(err, ErrSpaceNotFound) {
		if lock && r.afterLock != nil {
			f := r.afterLock
			r.afterLock = nil
			f(ctx)
		}
		if !lock && r.afterRead != nil {
			f := r.afterRead
			r.afterRead = nil
			f()
		}
	}
	return raw, err
}
func (r *spaceAdminPGHook) UpdateAdminSpaceState(ctx context.Context, owner string, version int64, at *time.Time, actor, reason string, updated time.Time) (int64, error) {
	receipt, err := r.PgRepository.UpdateAdminSpaceState(ctx, owner, version, at, actor, reason, updated)
	if err == nil && r.afterWrite != nil {
		r.afterWrite(ctx)
	}
	return receipt, err
}
func spaceAdminPGState(t *testing.T, g spaceAdminPGData) string {
	t.Helper()
	var raw string
	err := g.pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'spaces',(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY id),'[]'::jsonb)FROM user_spaces s WHERE user_id=$1),
 'snapshots',(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY id),'[]'::jsonb)FROM user_space_style_snapshots s WHERE user_id=$1::text),
 'user',(SELECT to_jsonb(u)FROM users u WHERE id=$1),
 'admin',(SELECT to_jsonb(a)FROM admin_accounts a WHERE id=$2),
 'session',(SELECT to_jsonb(a)FROM admin_sessions a WHERE id=$3),
 'bindings',(SELECT COALESCE(jsonb_agg(to_jsonb(a)ORDER BY id),'[]'::jsonb)FROM admin_role_bindings a WHERE account_id=$2),
 'permissions',(SELECT COALESCE(jsonb_agg(to_jsonb(a)ORDER BY permission_code),'[]'::jsonb)FROM admin_role_permissions a WHERE role_id=$4),
 'audit',(SELECT COALESCE(jsonb_agg(to_jsonb(a)ORDER BY id),'[]'::jsonb)FROM platform_command_audits a WHERE actor_id=$2),
 'outbox',(SELECT COALESCE(jsonb_agg(to_jsonb(a)ORDER BY id),'[]'::jsonb)FROM platform_outbox a WHERE aggregate_id=$1::text))::text`, g.owner, g.actor, g.claims.SessionID, g.role).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func assertSpaceAdminPGLock(t *testing.T, pool *pgxpool.Pool, sql, id string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, id)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatal("required actual row lock missing", sql, err)
	}
}
func TestSpaceAdminPostgreSQLActualLocksTypedActorsAndRequiredRollback(t *testing.T) {
	ctx := context.Background()
	index := 0
	for _, existing := range []bool{false, true} {
		for _, opaque := range []bool{false, true} {
			for _, op := range []string{"disable", "enable"} {
				for _, failure := range []string{"none", "platform_command_audits", "platform_outbox", "user_spaces", "extra_aba"} {
					t.Run(fmt.Sprint(existing, "/", opaque, "/", op, "/", failure), func(t *testing.T) {
						index++
						g := spaceAdminPostgres(t, 2520201+index*10, existing, opaque)
						if op == "enable" && existing {
							if _, err := g.s.DisableSpaceForPrincipal(ctx, g.owner, "first disable", g.proof); err != nil {
								t.Fatal(err)
							}
						}
						before := spaceAdminPGState(t, g)
						locked, written := false, false
						g.s.repo = &spaceAdminPGHook{PgRepository: g.repo, afterLock: func(tx context.Context) {
							locked = true
							for _, row := range []struct{ query, id string }{{"SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT", g.actor}, {"SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT", g.claims.SessionID}, {"SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT", g.role}, {"SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT", g.role}, {"SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT", g.role}, {"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", g.owner}} {
								assertSpaceAdminPGLock(t, g.pool, row.query, row.id)
							}
							if existing {
								assertSpaceAdminPGLock(t, g.pool, "SELECT id FROM user_spaces WHERE user_id=$1 FOR UPDATE NOWAIT", g.owner)
							}
							var acquired bool
							if err := g.pool.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('personal-space:'||$1::text,0))", g.owner).Scan(&acquired); err != nil || acquired {
								t.Fatal("owner admission lock missing", acquired, err)
							}
						}, afterWrite: func(tx context.Context) {
							written = true
							if failure == "extra_aba" {
								var title string
								if err := g.repo.db(tx).QueryRow(tx, "SELECT title FROM user_spaces WHERE user_id=$1", g.owner).Scan(&title); err != nil {
									t.Fatal(err)
								}
								if _, err := g.repo.db(tx).Exec(tx, "UPDATE user_spaces SET title='extra' WHERE user_id=$1", g.owner); err != nil {
									t.Fatal(err)
								}
								if _, err := g.repo.db(tx).Exec(tx, "UPDATE user_spaces SET title=$2 WHERE user_id=$1", g.owner, title); err != nil {
									t.Fatal(err)
								}
							}
						}}
						noWrite := op == "enable" && !existing
						if failure == "user_spaces" || failure == "platform_command_audits" || failure == "platform_outbox" {
							if _, err := g.pool.Exec(ctx, "CREATE FUNCTION space_admin_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'required space admin write rejected'; END $$"); err != nil {
								t.Fatal(err)
							}
							event := "INSERT"
							if failure == "user_spaces" {
								event = "UPDATE"
							}
							if _, err := g.pool.Exec(ctx, "CREATE TRIGGER space_admin_reject BEFORE "+event+" ON "+failure+" FOR EACH ROW EXECUTE FUNCTION space_admin_reject()"); err != nil {
								t.Fatal(err)
							}
							t.Cleanup(func() {
								if _, err := g.pool.Exec(ctx, "DROP TRIGGER space_admin_reject ON "+failure+";DROP FUNCTION space_admin_reject()"); err != nil {
									t.Fatal(err)
								}
							})
						}
						var ack *SpaceAdminMutationAck
						var err error
						if op == "disable" {
							ack, err = g.s.DisableSpaceForPrincipal(ctx, g.owner, "actual Admin", g.proof)
						} else {
							ack, err = g.s.EnableSpaceForPrincipal(ctx, g.owner, g.proof)
						}
						allowed := failure == "none" || noWrite && (failure == "user_spaces" || failure == "extra_aba")
						if !locked || (err == nil) != allowed {
							t.Fatal(ack, err, locked, written)
						}
						if !allowed {
							if ack != nil || spaceAdminPGState(t, g) != before {
								t.Fatal("failed command retained SQL state", ack, err)
							}
							if failure != "extra_aba" {
								var pgErr *pgconn.PgError
								if !errors.As(err, &pgErr) || pgErr.Code != "P0001" {
									t.Fatal("SQL rejection was not reached", err)
								}
							}
							return
						}
						if noWrite {
							if ack.SpaceID != "" || ack.Disabled || written {
								t.Fatal("default enable wrote space", ack, written)
							}
							return
						}
						row, err := g.repo.GetByUserID(ctx, g.owner)
						if err != nil || !written || ack.SpaceID != row.ID || row.DisabledBy != "" {
							t.Fatal(row, ack, err, written)
						}
						if op == "disable" {
							if row.DisabledAt == nil || row.DisabledActorKind != "admin" || row.DisabledActorID != g.actor {
								t.Fatal(row)
							}
						} else {
							if row.DisabledAt != nil || row.DisabledActorKind != "legacy_unknown" || row.DisabledActorID != "" || row.DisabledReason != "" {
								t.Fatal(row)
							}
						}
						var kind, payload string
						if err := g.pool.QueryRow(ctx, "SELECT actor_type FROM platform_command_audits WHERE actor_id=$1 ORDER BY created_at DESC LIMIT 1", g.actor).Scan(&kind); err != nil || kind != "admin" {
							t.Fatal(kind, err)
						}
						if err := g.pool.QueryRow(ctx, "SELECT payload::text FROM platform_outbox WHERE aggregate_id=$1 ORDER BY created_at DESC LIMIT 1", g.owner).Scan(&payload); err != nil {
							t.Fatal(err)
						}
						var fields map[string]json.RawMessage
						if json.Unmarshal([]byte(payload), &fields) != nil || len(fields) != 3 || fields["owner_id"] == nil || fields["space_id"] == nil || fields["disabled"] == nil {
							t.Fatal("unsafe event", payload)
						}
					})
				}
			}
		}
	}
}
func TestSpaceAdminPostgreSQLRejectCommittedFullFactsAndAdmissionChanges(t *testing.T) {
	ctx := context.Background()
	for i, mode := range []string{"space_aba", "user_email", "admin_session", "permission_aba", "default_created", "guard"} {
		t.Run(mode, func(t *testing.T) {
			g := spaceAdminPostgres(t, 2530201+i*10, mode != "default_created", true)
			ran := false
			g.s.repo = &spaceAdminPGHook{PgRepository: g.repo, afterRead: func() {
				ran = true
				exec := func(sql string, args ...any) {
					t.Helper()
					if _, err := g.pool.Exec(ctx, sql, args...); err != nil {
						t.Fatal(err)
					}
				}
				switch mode {
				case "space_aba":
					var title string
					if err := g.pool.QueryRow(ctx, "SELECT title FROM user_spaces WHERE user_id=$1", g.owner).Scan(&title); err != nil {
						t.Fatal(err)
					}
					exec("UPDATE user_spaces SET title='extra' WHERE user_id=$1", g.owner)
					exec("UPDATE user_spaces SET title=$2 WHERE user_id=$1", g.owner, title)
				case "user_email":
					exec("UPDATE users SET email='changed-space@example.test' WHERE id=$1", g.owner)
				case "admin_session":
					exec("UPDATE admin_sessions SET revoked_at=now(),revoke_reason='test',version=version+1 WHERE id=$1", g.claims.SessionID)
				case "permission_aba":
					exec("DELETE FROM admin_role_permissions WHERE role_id=$1", g.role)
					exec("INSERT INTO admin_role_permissions(role_id,permission_code,created_by)VALUES($1,'personal_space.space.manage','restore')", g.role)
				case "default_created":
					user, err := g.s.users.GetByID(ctx, g.owner)
					if err != nil {
						t.Fatal(err)
					}
					row := defaultSpace(user)
					row.ID = g.owner + "2"
					row.IsDefault = false
					if err := g.repo.Upsert(ctx, row); err != nil {
						t.Fatal(err)
					}
				case "guard":
					exec("ALTER TABLE user_spaces DISABLE TRIGGER trg_user_spaces_facts_version")
					t.Cleanup(func() { exec("ALTER TABLE user_spaces ENABLE TRIGGER trg_user_spaces_facts_version") })
				}
			}}
			ack, err := g.s.DisableSpaceForPrincipal(ctx, g.owner, "current proof", g.proof)
			if !ran || err == nil || ack != nil {
				t.Fatal("changed full facts admitted", ran, ack, err)
			}
			var count int
			if err := g.pool.QueryRow(ctx, "SELECT count(*) FROM platform_command_audits WHERE actor_id=$1", g.actor).Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}
