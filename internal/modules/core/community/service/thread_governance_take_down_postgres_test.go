package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type takeDownPG struct {
	s           *ThreadService
	pool        *pgxpool.Pool
	threads     *repository.PgThreadRepository
	row         *domain.Thread
	proof       identityport.SessionPrincipalResolver
	claims      *auth.JWTClaims
	grant, role string
	actor       string
}

func takeDownPostgres(t *testing.T, seed int, kind string) takeDownPG {
	t.Helper()
	s, threads, pool, row, user, claims := authorPostgres(t, fmt.Sprint(seed))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	grants := identityrepo.NewPgDelegationRepository(pool)
	g := takeDownPG{s: s, pool: pool, threads: threads, row: row, proof: authorPgProof(user, claims), claims: claims, actor: "user"}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	var admin *identityservice.AdminSessionService
	if kind == "user" {
		g.grant = fmt.Sprintf("grant.take-down.%d", seed)
		if err := grants.InsertDelegations(ctx, []identityrepo.Delegation{{ID: g.grant, Kind: "grant", SubjectKind: "user", SubjectID: claims.UserID, Action: "community.thread.take_down", BoardID: row.CategoryID, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RequiredStrength: "password", Status: "active", Version: 1}}); err != nil {
			t.Fatal(err)
		}
	} else {
		g.actor = "admin"
		id := row.AuthorID
		if kind == "admin_opaque" {
			id = "adm.take-down." + id
		}
		if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: id, Username: fmt.Sprintf("take-down-%d", seed), Email: fmt.Sprintf("take-down-%d@example.test", seed)}); err != nil {
			t.Fatal(err)
		}
		sessions := identityrepo.NewPgAdminSessionRepository(pool)
		sid := fmt.Sprintf("adms.take-down.%d", seed)
		at := now.Add(-time.Minute)
		if err := sessions.CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: id, RefreshTokenDigest: fmt.Sprintf("%064x", seed), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &at, CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "take-down-pg-admin-test-key", Issuer: "take-down-pg", Audience: "admin", AccessTTL: time.Hour})
		admin = identityservice.NewAdminSessionService(admins, sessions, jwt)
		token, err := jwt.GenerateAccessToken(id, "Admin", auth.AccessTokenContext{SessionID: sid, AuthVersion: 1})
		if err != nil {
			t.Fatal(err)
		}
		g.claims, err = jwt.VerifyAccessToken(token)
		if err != nil {
			t.Fatal(err)
		}
		g.proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
			return identityport.ResolveAdminSessionPrincipal(ctx, admin, g.claims)
		}
		g.role = fmt.Sprintf("take_down_role_%d", seed)
		if _, err := pool.Exec(ctx, `INSERT INTO admin_roles(id,name,description,is_system) VALUES($1,$1,'Take down drill',FALSE)`, g.role); err != nil {
			t.Fatal(err)
		}
		if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: fmt.Sprintf("binding.take-down.%d", seed), AccountID: id, RoleID: g.role, GrantedBy: id}); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.take_down','test')`, g.role); err != nil {
			t.Fatal(err)
		}
	}
	s.SetGovernanceAuthority(identityservice.NewBoardGovernanceAuthorityService(grants, admins, admin))
	return g
}

func takeDownCounts(t *testing.T, g takeDownPG, id string) []int {
	t.Helper()
	out := make([]int, 5)
	for i, q := range []string{`SELECT count(*) FROM content_revisions WHERE thread_id=$1`, `SELECT count(*) FROM content_moderation_cases WHERE thread_id=$1`, `SELECT count(*) FROM content_moderation_actions WHERE thread_id=$1`, `SELECT count(*) FROM platform_command_audits WHERE resource_id=$1`, `SELECT count(*) FROM platform_outbox WHERE aggregate_id=$1`} {
		if err := g.pool.QueryRow(context.Background(), q, id).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

type takeDownLockHistory struct {
	repository.ContentGovernanceRepository
	afterRevision func(context.Context)
}

func (r takeDownLockHistory) CreateRevision(ctx context.Context, v *domain.ContentRevision) error {
	if err := r.ContentGovernanceRepository.CreateRevision(ctx, v); err != nil {
		return err
	}
	r.afterRevision(ctx)
	return nil
}

func TestThreadGovernanceTakeDownPostgreSQLAtomicDomainsAndLocks(t *testing.T) {
	for i, kind := range []string{"user", "admin_numeric", "admin_opaque"} {
		t.Run(kind, func(t *testing.T) {
			g := takeDownPostgres(t, 120201+i, kind)
			ctx := context.Background()
			original := g.s.governance
			g.s.SetGovernanceRepository(takeDownLockHistory{ContentGovernanceRepository: original, afterRevision: func(context.Context) {
				queries := []struct{ sql, id string }{{`SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT`, g.row.ID}}
				if g.actor == "user" {
					queries = append(queries, []struct{ sql, id string }{{`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID}, {`SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID}, {`SELECT id FROM identity_delegations WHERE id=$1 FOR UPDATE NOWAIT`, g.grant}}...)
				} else {
					queries = append(queries, []struct{ sql, id string }{{`SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID}, {`SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID}, {`SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role}}...)
				}
				for _, q := range queries {
					_, err := g.pool.Exec(ctx, q.sql, q.id)
					var pe *pgconn.PgError
					if !errors.As(err, &pe) || pe.Code != "55P03" {
						t.Fatal("governance write authority row not held", q.sql, err)
					}
				}
			}})
			before, _ := g.threads.GetByID(ctx, g.row.ID)
			result, err := g.s.TakeDownForPrincipal(ctx, g.row.ID, "actual actor domain", g.proof)
			if err != nil || result == nil {
				t.Fatal(result, err)
			}
			after, _ := g.threads.GetByID(ctx, g.row.ID)
			if after.ModerationActorKind != g.actor || after.ModerationActorID != g.claims.UserID || after.ModerationStatus != "taken_down" || after.Content != before.Content || after.PublicationStatus != before.PublicationStatus || after.FactsVersion <= before.FactsVersion {
				t.Fatal(after)
			}
			if g.actor == "admin" && after.ModerationBy != "" {
				t.Fatal("Admin entered User bigint", after)
			}
			for _, q := range []string{`SELECT created_actor_kind,created_actor_id,COALESCE(created_by::text,'') FROM content_revisions WHERE thread_id=$1`, `SELECT opened_actor_kind,opened_actor_id,COALESCE(opened_by::text,'') FROM content_moderation_cases WHERE thread_id=$1`, `SELECT actor_kind,actor_ref_id,COALESCE(actor_id::text,'') FROM content_moderation_actions WHERE thread_id=$1`, `SELECT actor_type,actor_id,'' FROM platform_command_audits WHERE resource_id=$1`} {
				var kind, id, legacy string
				if err := g.pool.QueryRow(ctx, q, g.row.ID).Scan(&kind, &id, &legacy); err != nil || kind != g.actor || id != g.claims.UserID || g.actor == "admin" && legacy != "" {
					t.Fatal(kind, id, legacy, err)
				}
			}
			if got := takeDownCounts(t, g, g.row.ID); !reflect.DeepEqual(got, []int{1, 1, 1, 1, 1}) {
				t.Fatal(got)
			}
			var payload []byte
			if err := g.pool.QueryRow(ctx, `SELECT payload FROM platform_outbox WHERE aggregate_id=$1`, g.row.ID).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(payload, &fields); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(payload), before.Content) || strings.Contains(string(payload), before.Title) || strings.Contains(string(payload), "author_id") {
				t.Fatal("execute event leaked private data", string(payload))
			}
			// Each mandatory failure uses a fresh actual resource and full SQL rollback.
			for _, mode := range []string{"audit", "history"} {
				row := *before
				row.ID = fmt.Sprint(idgen.New())
				row.FactsVersion = 0
				if err := g.threads.Create(ctx, &row); err != nil {
					t.Fatal(err)
				}
				g.s.SetGovernanceRepository(original)
				g.s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(g.pool), reliability.NewPostgreSQLStore(g.pool)))
				if mode == "audit" {
					g.s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(g.pool), authorFailAudit{Store: reliability.NewPostgreSQLStore(g.pool)}))
				} else {
					g.s.SetGovernanceRepository(takeDownFailHistory{ContentGovernanceRepository: original})
				}
				if result, err := g.s.TakeDownForPrincipal(ctx, row.ID, "must roll back", g.proof); err == nil || result != nil {
					t.Fatal("required failure accepted", mode, result, err)
				}
				stored, _ := g.threads.GetByID(ctx, row.ID)
				if stored.ModerationStatus != row.ModerationStatus || stored.FactsVersion != row.FactsVersion || !reflect.DeepEqual(takeDownCounts(t, g, row.ID), []int{0, 0, 0, 0, 0}) {
					t.Fatal("governance SQL failure partially persisted", mode, stored)
				}
			}
		})
	}
}

func TestThreadGovernanceTakeDownPostgreSQLCommittedChanges(t *testing.T) {
	modes := []string{"user/grant_revoke", "user/grant_aba", "user/session_revoke", "user/auth_version", "user/thread_board", "user/thread_body_aba", "user/thread_actor_aba", "admin/permission_aba", "admin/binding_revoke", "admin/session_version", "admin/mfa_expire", "admin/thread_owner"}
	for i, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			kind := "user"
			if strings.HasPrefix(mode, "admin/") {
				kind = "admin_opaque"
			}
			g := takeDownPostgres(t, 120301+i, kind)
			if mode == "admin/thread_owner" {
				if _, err := g.pool.Exec(context.Background(), `INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES(120399,'governance-other-owner','Other Owner','governance-other-owner@example.test','active',1) ON CONFLICT(id) DO NOTHING`); err != nil {
					t.Fatal(err)
				}
			}

			ctx := context.Background()
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 3 {
					tx, err := g.pool.Begin(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(context.Background())
					exec := func(q string, args ...any) {
						if _, err := tx.Exec(context.Background(), q, args...); err != nil {
							t.Fatal(err)
						}
					}
					switch mode {
					case "user/grant_revoke":
						exec(`UPDATE identity_delegations SET status='revoked',revoked_at=NOW(),version=version+1 WHERE id=$1`, g.grant)
					case "user/grant_aba":
						exec(`UPDATE identity_delegations SET status='suspended',version=version+1 WHERE id=$1`, g.grant)
						exec(`UPDATE identity_delegations SET status='active',version=version+1 WHERE id=$1`, g.grant)
					case "user/session_revoke":
						exec(`UPDATE sessions SET revoked_at=NOW(),revoke_reason='take-down-test' WHERE id=$1`, g.claims.SessionID)
					case "user/auth_version":
						exec(`UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, g.claims.UserID)
					case "user/thread_board":
						exec(`UPDATE threads SET category_id=113112 WHERE id=$1`, g.row.ID)
					case "user/thread_body_aba":
						exec(`UPDATE threads SET content='changed' WHERE id=$1`, g.row.ID)
						exec(`UPDATE threads SET content=$2 WHERE id=$1`, g.row.ID, g.row.Content)
					case "user/thread_actor_aba":
						exec(`UPDATE threads SET moderation_actor_kind='admin',moderation_actor_id='adm.temporary' WHERE id=$1`, g.row.ID)
						exec(`UPDATE threads SET moderation_actor_kind='legacy_unknown',moderation_actor_id='' WHERE id=$1`, g.row.ID)
					case "admin/permission_aba":
						exec(`DELETE FROM admin_role_permissions WHERE role_id=$1`, g.role)
						exec(`INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.take_down','test')`, g.role)
					case "admin/binding_revoke":
						exec(`UPDATE admin_role_bindings SET status='revoked',revoked_at=NOW(),version=version+1 WHERE role_id=$1`, g.role)
					case "admin/session_version":
						exec(`UPDATE admin_sessions SET version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "admin/mfa_expire":
						exec(`UPDATE admin_sessions SET mfa_authenticated_at=NOW()-interval '16 minutes',version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "admin/thread_owner":
						exec(`UPDATE threads SET author_id=120399 WHERE id=$1`, g.row.ID)
					}
					if err := tx.Commit(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				return g.proof(ctx)
			}
			result, err := g.s.TakeDownForPrincipal(ctx, g.row.ID, "must reject stale allow", proof)
			if err == nil || result != nil {
				t.Fatal("committed authority/resource change accepted", result, err)
			}
			after, _ := g.threads.GetByID(ctx, g.row.ID)
			if after.ModerationStatus == "taken_down" || !reflect.DeepEqual(takeDownCounts(t, g, g.row.ID), []int{0, 0, 0, 0, 0}) {
				t.Fatal("rejected race persisted governance", after)
			}
		})
	}
}
