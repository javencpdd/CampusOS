package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
)

type postGovernancePG struct {
	takeDownPG
	postService *PostService
	posts       *repository.PgPostRepository
	post        *domain.Post
}

func postGovernancePostgres(t *testing.T, seed int, kind string) postGovernancePG {
	t.Helper()
	base := takeDownPostgres(t, seed, kind)
	ctx := context.Background()
	if base.actor == "user" {
		if _, err := base.pool.Exec(ctx, `UPDATE identity_delegations SET action='community.post.delete',version=version+1 WHERE id=$1`, base.grant); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := base.pool.Exec(ctx, `UPDATE admin_role_permissions SET permission_code='community.post.delete' WHERE role_id=$1`, base.role); err != nil {
			t.Fatal(err)
		}
	}
	posts := repository.NewPgPostRepository(base.pool)
	post := &domain.Post{ID: fmt.Sprint(seed + 5000), ThreadID: base.row.ID, AuthorID: base.row.AuthorID, Content: "private governance reply", Status: "published", CreatedAt: base.row.CreatedAt, UpdatedAt: base.row.UpdatedAt}
	if err := posts.Create(ctx, post); err != nil {
		t.Fatal(err)
	}
	if err := base.threads.IncrementReplyCount(ctx, base.row.ID, 1); err != nil {
		t.Fatal(err)
	}
	s := NewPostService(posts, nil)
	s.SetThreadRepository(base.threads)
	s.SetGovernanceAuthority(base.s.governanceAuthority)
	s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(base.pool), reliability.NewPostgreSQLStore(base.pool)))
	return postGovernancePG{takeDownPG: base, postService: s, posts: posts, post: post}
}
func postGovernancePGCounts(t *testing.T, g postGovernancePG) []int {
	t.Helper()
	out := make([]int, 3)
	for i, q := range []string{`SELECT reply_count FROM threads WHERE id=$1`, `SELECT count(*) FROM platform_command_audits WHERE resource_id=$1`, `SELECT count(*) FROM platform_outbox WHERE aggregate_type='post' AND aggregate_id=$1`} {
		id := g.post.ID
		if i == 0 {
			id = g.row.ID
		}
		if err := g.pool.QueryRow(context.Background(), q, id).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestPostGovernanceDeletePostgreSQLActorLocksAndAtomicity(t *testing.T) {
	for i, kind := range []string{"user", "admin_numeric", "admin_opaque"} {
		t.Run(kind, func(t *testing.T) {
			g := postGovernancePostgres(t, 124201+i, kind)
			ctx := context.Background()
			g.postService.repo = postAuthorProbeRepo{PgPostRepository: g.posts, after: func(context.Context) {
				queries := []struct{ q, id string }{{`SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT`, g.row.ID}, {`SELECT id FROM posts WHERE id=$1 FOR UPDATE NOWAIT`, g.post.ID}}
				if g.actor == "user" {
					queries = append(queries, []struct{ q, id string }{{`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID}, {`SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID}, {`SELECT id FROM identity_delegations WHERE id=$1 FOR UPDATE NOWAIT`, g.grant}}...)
				} else {
					queries = append(queries, []struct{ q, id string }{{`SELECT id FROM admin_accounts WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.UserID}, {`SELECT id FROM admin_sessions WHERE id=$1 FOR UPDATE NOWAIT`, g.claims.SessionID}, {`SELECT id FROM admin_roles WHERE id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT id FROM admin_role_bindings WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role}, {`SELECT role_id FROM admin_role_permissions WHERE role_id=$1 FOR UPDATE NOWAIT`, g.role}}...)
				}
				for _, q := range queries {
					_, err := g.pool.Exec(ctx, q.q, q.id)
					var pe *pgconn.PgError
					if !errors.As(err, &pe) || pe.Code != "55P03" {
						t.Fatal("actual governance witness not locked", q.q, err)
					}
				}
			}}
			if err := g.postService.GovernanceDeleteForPrincipal(ctx, g.row.ID, g.post.ID, g.proof, nil); err != nil {
				t.Fatal(err)
			}
			deleted, err := g.posts.ReadPostCommandSnapshot(ctx, g.post.ID)
			if err != nil || deleted.DeletedAt == nil || deleted.DeletedActorKind != g.actor || deleted.DeletedActorID != g.claims.UserID || deleted.AuthorID != g.row.AuthorID {
				t.Fatal(deleted, err)
			}
			if got := postGovernancePGCounts(t, g); !reflect.DeepEqual(got, []int{0, 1, 1}) {
				t.Fatal(got)
			}
			// Another resource proves required audit rollback independently of the success.
			p := *g.post
			p.ID = fmt.Sprint(134201 + i)
			p.FactsVersion = 0
			if err := g.posts.Create(ctx, &p); err != nil {
				t.Fatal(err)
			}
			g.threads.IncrementReplyCount(ctx, g.row.ID, 1)
			g.postService.repo = g.posts
			g.postService.SetReliability(reliability.NewService(transaction.NewPostgreSQL(g.pool), authorFailAudit{Store: reliability.NewPostgreSQLStore(g.pool)}))
			if err := g.postService.GovernanceDeleteForPrincipal(ctx, g.row.ID, p.ID, g.proof, nil); err == nil {
				t.Fatal("platform audit failure accepted")
			}
			restored, err := g.posts.GetByID(ctx, p.ID)
			if err != nil || restored.DeletedAt != nil {
				t.Fatal(restored, err)
			}
			var replies int
			if err := g.pool.QueryRow(ctx, `SELECT reply_count FROM threads WHERE id=$1`, g.row.ID).Scan(&replies); err != nil || replies != 1 {
				t.Fatal(replies, err)
			}
		})
	}
}

func TestPostGovernanceDeletePostgreSQLCommittedChanges(t *testing.T) {
	modes := []string{"user/grant_revoke", "user/grant_aba", "user/session", "user/auth_version", "user/parent_board", "user/post_aba", "user/post_thread", "admin/permission_aba", "admin/binding_revoke", "admin/mfa_expire", "admin/session_version", "admin/parent_aba"}
	for i, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			kind := "user"
			if mode[:5] == "admin" {
				kind = "admin_opaque"
			}
			g := postGovernancePostgres(t, 124301+i, kind)
			ctx := context.Background()
			before := postGovernancePGCounts(t, g)
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
					case "user/session":
						exec(`UPDATE sessions SET revoked_at=NOW(),revoke_reason='post-gov-test' WHERE id=$1`, g.claims.SessionID)
					case "user/auth_version":
						exec(`UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, g.claims.UserID)
					case "user/parent_board":
						exec(`UPDATE threads SET category_id=113112 WHERE id=$1`, g.row.ID)
					case "user/post_aba":
						exec(`UPDATE posts SET content='other' WHERE id=$1`, g.post.ID)
						exec(`UPDATE posts SET content=$2 WHERE id=$1`, g.post.ID, g.post.Content)
					case "user/post_thread":
						exec(`UPDATE posts SET thread_id=119201 WHERE id=$1`, g.post.ID)
					case "admin/permission_aba":
						exec(`DELETE FROM admin_role_permissions WHERE role_id=$1`, g.role)
						exec(`INSERT INTO admin_role_permissions(role_id,permission_code,created_by) VALUES($1,'community.post.delete','test')`, g.role)
					case "admin/binding_revoke":
						exec(`UPDATE admin_role_bindings SET status='revoked',revoked_at=NOW(),version=version+1 WHERE role_id=$1`, g.role)
					case "admin/mfa_expire":
						exec(`UPDATE admin_sessions SET mfa_authenticated_at=NOW()-interval '16 minutes',version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "admin/session_version":
						exec(`UPDATE admin_sessions SET version=version+1 WHERE id=$1`, g.claims.SessionID)
					case "admin/parent_aba":
						exec(`UPDATE threads SET content='other' WHERE id=$1`, g.row.ID)
						exec(`UPDATE threads SET content=$2 WHERE id=$1`, g.row.ID, g.row.Content)
					}
					if err := tx.Commit(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				return g.proof(ctx)
			}
			if err := g.postService.GovernanceDeleteForPrincipal(ctx, g.row.ID, g.post.ID, proof, nil); err == nil {
				t.Fatal("committed current authority/resource drift accepted", mode)
			}
			if !reflect.DeepEqual(before, postGovernancePGCounts(t, g)) {
				t.Fatal("failed governance changed counter/audit/outbox")
			}
			current, err := g.posts.GetByID(ctx, g.post.ID)
			if err != nil || current.DeletedAt != nil {
				t.Fatal(current, err)
			}
		})
	}
}
