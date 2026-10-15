package richtext

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communityservice "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	identityservice "github.com/campusos/CampusOS/internal/modules/core/identity/service"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type articleGovernancePG struct {
	s               *Service
	core            *communityservice.ThreadService
	pool            *pgxpool.Pool
	gov             *communityrepo.PgContentGovernanceRepository
	proof           identityport.SessionPrincipalResolver
	claims          *auth.JWTClaims
	role, id, actor string
}

func articleGovernancePostgres(t *testing.T, seed int, opaque bool, op string) articleGovernancePG {
	t.Helper()
	ctx := context.Background()
	owner := fmt.Sprint(seed)
	s, core, pool, author, _ := articleCreatePostgres(t, owner)
	g := articleGovernancePG{s: s, core: core, pool: pool, actor: owner, role: "article_gov_" + owner}
	req := articleCreateRequest()
	req.CategoryID = "115111"
	draft, err := s.CreateDraftForPrincipal(ctx, "Owner", req, author)
	if err != nil {
		t.Fatal(err)
	}
	g.id = draft.ThreadID
	if _, err := s.PublishForPrincipal(ctx, g.id, author); err != nil {
		t.Fatal(err)
	}
	g.gov = communityrepo.NewPgContentGovernanceRepository(pool)
	core.SetGovernanceRepository(g.gov)
	if op == "direct_restore" {
		if _, err := pool.Exec(ctx, `UPDATE threads SET moderation_status='taken_down',moderation_by=$2 WHERE id=$1`, g.id, owner); err != nil {
			t.Fatal(err)
		}
		if err := g.gov.CreateModerationCase(ctx, &domain.ModerationCase{ID: owner + "7", ThreadID: g.id, Status: "taken_down", Reason: "original case", OpenedBy: owner, OpenedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	if opaque {
		g.actor = "adm.article." + owner
	}
	admins := identityrepo.NewPgAdminDomainRepository(pool)
	if err := admins.CreateAccount(ctx, identityrepo.AdminDomainAccount{ID: g.actor, Username: "article-gov-" + owner, Email: "article-gov-" + owner + "@example.test"}); err != nil {
		t.Fatal(err)
	}
	sessions := identityrepo.NewPgAdminSessionRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	at := now.Add(-time.Minute)
	sid := "adms.article." + owner
	if err := sessions.CreateSession(ctx, identityrepo.AdminSession{ID: sid, AccountID: g.actor, RefreshTokenDigest: fmt.Sprintf("%064x", seed), TokenFamilyID: sid, AuthenticationStrength: "mfa", MFAAuthenticatedAt: &at, CreatedAt: now, UpdatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTManager(auth.JWTConfig{Secret: "article-gov-pg-admin-test-key", Issuer: "article-gov-pg", Audience: "admin", AccessTTL: time.Hour})
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
	if _, err := pool.Exec(ctx, `INSERT INTO admin_roles(id,name,description,is_system) VALUES($1,$1,'Article governance drill',FALSE)`, g.role); err != nil {
		t.Fatal(err)
	}
	if err := admins.BindRole(ctx, identityrepo.AdminDomainRoleBinding{ID: "binding.article." + owner, AccountID: g.actor, RoleID: g.role, GrantedBy: g.actor}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO admin_role_permissions(role_id,permission_code,created_by) SELECT $1,code,'test' FROM unnest($2::text[]) code`, g.role, []string{"community.thread.take_down", "community.thread.direct_restore", "community.thread.trash"}); err != nil {
		t.Fatal(err)
	}
	source := identityservice.NewBoardGovernanceAuthorityService(nil, admins, current)
	core.SetGovernanceAuthority(source)
	core.SetAdminExecutionAuthority(source)
	return g
}
func articleGovernanceCounts(t *testing.T, g articleGovernancePG) []int {
	t.Helper()
	out := make([]int, 5)
	for i, q := range []string{`SELECT count(*) FROM content_revisions WHERE thread_id=$1`, `SELECT count(*) FROM content_moderation_cases WHERE thread_id=$1`, `SELECT count(*) FROM content_moderation_actions WHERE thread_id=$1`, `SELECT count(*) FROM platform_command_audits WHERE resource_id=$1`, `SELECT count(*) FROM platform_outbox WHERE aggregate_id=$1`} {
		if err := g.pool.QueryRow(context.Background(), q, g.id).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

type articleGovernanceFailHistory struct {
	communityrepo.ContentGovernanceRepository
	communityrepo.ModerationCaseCommandRepository
}

func (r articleGovernanceFailHistory) CreateRevision(ctx context.Context, row *domain.ContentRevision) error {
	if err := r.ContentGovernanceRepository.CreateRevision(ctx, row); err != nil {
		return err
	}
	return errors.New("required revision failure after SQL insert")
}

type articleGovernanceFailOutbox struct{ reliability.Store }

func (s articleGovernanceFailOutbox) Enqueue(ctx context.Context, event *reliability.Event) (*reliability.Event, error) {
	if _, err := s.Store.Enqueue(ctx, event); err != nil {
		return nil, err
	}
	return nil, errors.New("required outbox failure after SQL insert")
}

func TestRichTextGovernancePostgreSQLAtomicTypedActorsAndCurrentLocks(t *testing.T) {
	for i, op := range []string{"take_down", "direct_restore", "trash"} {
		for j, opaque := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", op, opaque), func(t *testing.T) {
				g := articleGovernancePostgres(t, 149501+i*2+j, opaque, op)
				ctx := context.Background()
				base := g.s.store
				beforeArticle, _ := base.GetArticleByThreadID(ctx, g.id)
				threads := communityrepo.NewPgThreadRepository(g.pool)
				beforeThread, _ := threads.GetByID(ctx, g.id)
				beforeCounts := articleGovernanceCounts(t, g)
				for _, mode := range []string{"article", "revision", "audit", "outbox"} {
					t.Run(mode, func(t *testing.T) {
						var audit reliability.Store = reliability.NewPostgreSQLStore(g.pool)
						g.s.store = base
						g.core.SetGovernanceRepository(g.gov)
						if mode == "article" {
							g.s.store = &articleGovernanceHookStore{Store: base, afterUpdate: func(context.Context, *Article) error { return errors.New("required Article failure after SQL update") }}
						}
						if mode == "revision" {
							g.core.SetGovernanceRepository(articleGovernanceFailHistory{g.gov, g.gov})
						}
						if mode == "audit" {
							audit = articleCreateFailAudit{audit}
						}
						if mode == "outbox" {
							audit = articleGovernanceFailOutbox{audit}
						}
						d := reliability.NewService(transaction.NewPostgreSQL(g.pool), audit)
						g.core.SetReliability(d)
						g.s.SetReliability(d)
						if result, err := g.s.AdminGovernanceForPrincipal(ctx, g.id, op, "actual governance", g.proof); err == nil || result != nil {
							t.Fatal("mandatory failure accepted", result, err)
						}
						afterArticle, _ := base.GetArticleByThreadID(ctx, g.id)
						afterThread, _ := threads.GetByID(ctx, g.id)
						if !reflect.DeepEqual(beforeArticle, afterArticle) || !reflect.DeepEqual(beforeThread, afterThread) || !reflect.DeepEqual(beforeCounts, articleGovernanceCounts(t, g)) {
							t.Fatal("partial Article/Core/history/audit/outbox", mode)
						}
					})
				}
				g.core.SetGovernanceRepository(g.gov)
				d := reliability.NewService(transaction.NewPostgreSQL(g.pool), reliability.NewPostgreSQLStore(g.pool))
				g.core.SetReliability(d)
				g.s.SetReliability(d)
				locked := false
				g.s.store = &articleGovernanceHookStore{Store: base, afterLock: func(_ context.Context, a *Article) error {
					if locked {
						return nil
					}
					locked = true
					for _, q := range []struct{ sql, id string }{{`SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT`, g.actor}, {`SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID}, {`SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT id FROM admin_role_bindings WHERE account_id=$1 FOR UPDATE NOWAIT`, g.actor}, {`SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT`, g.id}, {`SELECT id FROM richtext_article_contents WHERE thread_id=$1 FOR UPDATE NOWAIT`, g.id}} {
						_, err := g.pool.Exec(ctx, q.sql, q.id)
						var pe *pgconn.PgError
						if !errors.As(err, &pe) || pe.Code != "55P03" {
							t.Fatal("actual authority/Article/Core lock missing", q.sql, err)
						}
					}
					return nil
				}}
				result, err := g.s.AdminGovernanceForPrincipal(ctx, g.id, op, "actual governance", g.proof)
				if err != nil || result == nil || !locked {
					t.Fatal(result, locked, err)
				}
				after, _ := base.GetArticleByThreadID(ctx, g.id)
				if after.FactsVersion <= beforeArticle.FactsVersion || after.UpdatedBy != "" || after.UpdatedActorKind != "admin" || after.UpdatedActorID != g.actor || after.ContentHTML != beforeArticle.ContentHTML {
					t.Fatal(after)
				}
				var actorKind, actor, legacy, payload string
				if err := g.pool.QueryRow(ctx, `SELECT actor_kind,actor_ref_id,COALESCE(actor_id::text,'') FROM content_moderation_actions WHERE thread_id=$1 ORDER BY created_at DESC LIMIT 1`, g.id).Scan(&actorKind, &actor, &legacy); err != nil || actorKind != "admin" || actor != g.actor || legacy != "" {
					t.Fatal(actorKind, actor, legacy, err)
				}
				if err := g.pool.QueryRow(ctx, `SELECT payload::text FROM platform_outbox WHERE aggregate_id=$1 ORDER BY created_at DESC LIMIT 1`, g.id).Scan(&payload); err != nil || strings.Contains(payload, beforeArticle.ContentHTML) || strings.Contains(payload, beforeArticle.Title) {
					t.Fatal(payload, err)
				}
			})
		}
	}
}

func TestRichTextGovernancePostgreSQLCommittedABAAndGuard(t *testing.T) {
	for i, mode := range []string{"summary_aba", "json_aba", "actor_aba", "deleted_at_aba", "reinsert", "core_aba", "permission_aba", "session_revoke", "mfa_expired", "disabled_guard", "counters"} {
		t.Run(mode, func(t *testing.T) {
			g := articleGovernancePostgres(t, 149601+i, true, "take_down")
			ctx := context.Background()
			counts := articleGovernanceCounts(t, g)
			original, _ := g.s.store.GetArticleByThreadID(ctx, g.id)
			calls := 0
			base := g.proof
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 2 {
					tx, err := g.pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(ctx)
					exec := func(q string, args ...any) {
						if _, err := tx.Exec(ctx, q, args...); err != nil {
							t.Fatal(mode, err)
						}
					}
					switch mode {
					case "summary_aba":
						exec(`UPDATE richtext_article_contents SET summary='external' WHERE thread_id=$1`, g.id)
						exec(`UPDATE richtext_article_contents SET summary=$2 WHERE thread_id=$1`, g.id, original.Summary)
					case "json_aba":
						exec(`UPDATE richtext_article_contents SET content_json='{"value":9007199254740993}'::jsonb WHERE thread_id=$1`, g.id)
						exec(`UPDATE richtext_article_contents SET content_json=$2::jsonb WHERE thread_id=$1`, g.id, string(original.ContentJSON))
					case "actor_aba":
						exec(`UPDATE richtext_article_contents SET updated_by=NULL,updated_actor_kind='admin',updated_actor_id='adm.external' WHERE thread_id=$1`, g.id)
						exec(`UPDATE richtext_article_contents SET updated_by=created_by,updated_actor_kind='user',updated_actor_id=created_by::text WHERE thread_id=$1`, g.id)
					case "deleted_at_aba":
						exec(`UPDATE richtext_article_contents SET deleted_at=NOW() WHERE thread_id=$1`, g.id)
						exec(`UPDATE richtext_article_contents SET deleted_at=NULL WHERE thread_id=$1`, g.id)
					case "reinsert":
						exec(`CREATE TEMP TABLE saved_article ON COMMIT DROP AS SELECT * FROM richtext_article_contents WHERE thread_id=$1`, g.id)
						exec(`DELETE FROM richtext_article_contents WHERE thread_id=$1`, g.id)
						exec(`INSERT INTO richtext_article_contents SELECT * FROM saved_article`)
					case "core_aba":
						exec(`UPDATE threads SET content=content||' external' WHERE id=$1`, g.id)
						exec(`UPDATE threads SET content=left(content,length(content)-9) WHERE id=$1`, g.id)
					case "permission_aba":
						exec(`DELETE FROM admin_role_permissions WHERE role_id=$1 AND permission_code='community.thread.take_down'`, g.role)
						exec(`INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.thread.take_down','test')`, g.role)
					case "session_revoke":
						exec(`UPDATE admin_sessions SET revoked_at=NOW(),revoke_reason='test',version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "mfa_expired":
						exec(`UPDATE admin_sessions SET mfa_authenticated_at=NOW()-INTERVAL '16 minutes',version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "disabled_guard":
						exec(`ALTER TABLE richtext_article_contents DISABLE TRIGGER trg_article_facts_version`)
					case "counters":
						exec(`UPDATE threads SET view_count=17,reply_count=3,like_count=2 WHERE id=$1`, g.id)
					}
					if err := tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}
				return base(ctx)
			}
			if mode == "disabled_guard" {
				defer func() {
					if _, err := g.pool.Exec(ctx, `ALTER TABLE richtext_article_contents ENABLE TRIGGER trg_article_facts_version`); err != nil {
						t.Fatal(err)
					}
				}()
			}
			result, err := g.s.AdminGovernanceForPrincipal(ctx, g.id, "take_down", "actual governance", proof)
			if mode == "counters" {
				if err != nil || result == nil {
					t.Fatal(result, err)
				}
				var views, replies, likes int
				if err := g.pool.QueryRow(ctx, `SELECT view_count,reply_count,like_count FROM threads WHERE id=$1`, g.id).Scan(&views, &replies, &likes); err != nil || views != 17 || replies != 3 || likes != 2 {
					t.Fatal(views, replies, likes, err)
				}
			} else if err == nil || result != nil || !reflect.DeepEqual(counts, articleGovernanceCounts(t, g)) {
				t.Fatal("stale command persisted", result, err)
			}
		})
	}
}
