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
	"github.com/campusos/CampusOS/pkg/auth"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func postAuthorPostgres(t *testing.T, owner string) (*PostService, *pgxpool.Pool, *domain.Thread, identityport.SessionPrincipalResolver, *auth.JWTClaims) {
	t.Helper()
	threads, _, pool, parent, user, claims := authorPostgres(t, owner)
	threads.SetCategoryRepository(repository.NewPgCategoryRepository(pool))
	threads.SetThreadTypePolicyRepository(repository.NewPgThreadTypePolicyRepository(pool))
	s := NewPostService(repository.NewPgPostRepository(pool), nil)
	s.SetThreadRepository(threads.repo)
	s.SetCategoryRepository(threads.categoryRepo)
	s.SetThreadCommandSource(threads)
	s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
	return s, pool, parent, authorPgProof(user, claims), claims
}

type postAuthorProbeRepo struct {
	*repository.PgPostRepository
	after func(context.Context)
}

func (r postAuthorProbeRepo) Create(ctx context.Context, p *domain.Post) error {
	if err := r.PgPostRepository.Create(ctx, p); err != nil {
		return err
	}
	r.after(ctx)
	return nil
}
func (r postAuthorProbeRepo) UpdatePostIfFactsVersion(ctx context.Context, p *domain.Post, v int64) error {
	if err := r.PgPostRepository.UpdatePostIfFactsVersion(ctx, p, v); err != nil {
		return err
	}
	r.after(ctx)
	return nil
}
func (r postAuthorProbeRepo) DeletePostIfFactsVersion(ctx context.Context, id string, v int64, a identityport.PrincipalRef) error {
	if err := r.PgPostRepository.DeletePostIfFactsVersion(ctx, id, v, a); err != nil {
		return err
	}
	r.after(ctx)
	return nil
}

func postAuthorCounts(t *testing.T, pool *pgxpool.Pool, parentID string) []int {
	t.Helper()
	out := make([]int, 4)
	for i, q := range []string{`SELECT count(*) FROM posts WHERE thread_id=$1`, `SELECT reply_count FROM threads WHERE id=$1`, `SELECT count(*) FROM platform_command_audits WHERE resource_id IN(SELECT id::text FROM posts WHERE thread_id=$1)`, `SELECT count(*) FROM platform_outbox WHERE aggregate_type='post' AND aggregate_id IN(SELECT id::text FROM posts WHERE thread_id=$1)`} {
		if err := pool.QueryRow(context.Background(), q, parentID).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestPostAuthorPostgreSQLAtomicThreePathsAndLocks(t *testing.T) {
	for i, op := range []string{"create", "edit", "delete"} {
		t.Run(op, func(t *testing.T) {
			s, pool, parent, proof, claims := postAuthorPostgres(t, fmt.Sprint(122201+i))
			ctx := context.Background()
			existing, err := s.CreatePostForPrincipal(ctx, parent.ID, "Owner", domain.CreatePostRequest{Content: "quote or target"}, proof)
			if err != nil {
				t.Fatal(err)
			}
			raw := s.repo.(*repository.PgPostRepository)
			s.repo = postAuthorProbeRepo{PgPostRepository: raw, after: func(context.Context) {
				queries := []struct{ q, id string }{{`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, claims.UserID}, {`SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT`, claims.SessionID}, {`SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT`, parent.ID}, {`SELECT id FROM posts WHERE id=$1 FOR UPDATE NOWAIT`, existing.ID}}
				if op == "create" {
					queries = append(queries, []struct{ q, id string }{{`SELECT id FROM categories WHERE id=$1 FOR UPDATE NOWAIT`, parent.CategoryID}, {`SELECT id FROM categories WHERE id=$1 FOR UPDATE NOWAIT`, "113110"}, {`SELECT category_id FROM category_thread_type_policies WHERE category_id=$1 AND thread_type='discussion' FOR UPDATE NOWAIT`, parent.CategoryID}}...)
				}
				for _, q := range queries {
					_, err := pool.Exec(ctx, q.q, q.id)
					var pe *pgconn.PgError
					if !errors.As(err, &pe) || pe.Code != "55P03" {
						t.Fatal("Post author authority row not locked", q.q, err)
					}
				}
			}}
			var result *domain.Post
			switch op {
			case "create":
				result, err = s.CreatePostForPrincipal(ctx, parent.ID, "Owner", domain.CreatePostRequest{Content: "new reply", ParentID: &existing.ID}, proof)
			case "edit":
				result, err = s.UpdatePostForPrincipal(ctx, existing.ID, "changed reply", proof)
			case "delete":
				err = s.DeletePostForPrincipal(ctx, existing.ID, proof)
			}
			if err != nil {
				t.Fatal(op, result, err)
			}
			if op == "create" && (result == nil || result.ParentFloorNumber != existing.FloorNumber) {
				t.Fatal(result)
			}
			if op == "edit" && (result == nil || result.FactsVersion <= existing.FactsVersion || result.Content != "changed reply") {
				t.Fatal(result)
			}
			if op == "delete" {
				deleted, e := raw.ReadPostCommandSnapshot(ctx, existing.ID)
				if e != nil || deleted.DeletedActorKind != "user" || deleted.DeletedActorID != claims.UserID || deleted.DeletedAt == nil {
					t.Fatal(deleted, e)
				}
			}
			s.repo = raw
			// Mandatory audit fails after mutation; all SQL state and event writes revert.
			fresh, err := s.CreatePostForPrincipal(ctx, parent.ID, "Owner", domain.CreatePostRequest{Content: "failure target"}, proof)
			if err != nil {
				t.Fatal(err)
			}
			before := postAuthorCounts(t, pool, parent.ID)
			beforeTarget, _ := raw.ReadPostCommandSnapshot(ctx, fresh.ID)
			s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), authorFailAudit{Store: reliability.NewPostgreSQLStore(pool)}))
			switch op {
			case "create":
				result, err = s.CreatePostForPrincipal(ctx, parent.ID, "Owner", domain.CreatePostRequest{Content: "rollback"}, proof)
			case "edit":
				result, err = s.UpdatePostForPrincipal(ctx, fresh.ID, "rollback", proof)
			case "delete":
				err = s.DeletePostForPrincipal(ctx, fresh.ID, proof)
			}
			if err == nil {
				t.Fatal("required audit failure accepted")
			}
			afterTarget, _ := raw.ReadPostCommandSnapshot(ctx, fresh.ID)
			if !reflect.DeepEqual(before, postAuthorCounts(t, pool, parent.ID)) || !samePostCommandState(beforeTarget, afterTarget) {
				t.Fatal("Post audit failure partially committed", before, afterTarget)
			}
		})
	}
}

func TestPostAuthorPostgreSQLCommittedCredentialAndResourceChanges(t *testing.T) {
	for i, op := range []string{"create", "edit", "delete"} {
		for j, mode := range []string{"session", "auth_version", "parent_aba", "post_or_quote_aba", "parent_trash", "owner_or_board"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				s, pool, parent, proof, claims := postAuthorPostgres(t, fmt.Sprint(122301+i*20+j))
				ctx := context.Background()
				target, err := s.CreatePostForPrincipal(ctx, parent.ID, "Owner", domain.CreatePostRequest{Content: "before"}, proof)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `INSERT INTO users(id,username,nickname,email,status,auth_version) VALUES(122399,'post-other-owner','Other','post-other-owner@example.test','active',1) ON CONFLICT(id) DO NOTHING`); err != nil {
					t.Fatal(err)
				}
				before := postAuthorCounts(t, pool, parent.ID)
				calls := 0
				changing := func(ctx context.Context) (identityport.PrincipalContext, error) {
					calls++
					if calls == 2 {
						tx, err := pool.Begin(context.Background())
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
						case "session":
							exec(`UPDATE sessions SET revoked_at=NOW(),revoke_reason='post-author-test' WHERE id=$1`, claims.SessionID)
						case "auth_version":
							exec(`UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, claims.UserID)
						case "parent_aba":
							exec(`UPDATE threads SET content='changed' WHERE id=$1`, parent.ID)
							exec(`UPDATE threads SET content=$2 WHERE id=$1`, parent.ID, parent.Content)
						case "post_or_quote_aba":
							exec(`UPDATE posts SET content='changed' WHERE id=$1`, target.ID)
							exec(`UPDATE posts SET content='before' WHERE id=$1`, target.ID)
						case "parent_trash":
							exec(`UPDATE threads SET deletion_status='trashed',status='archived' WHERE id=$1`, parent.ID)
						case "owner_or_board":
							if op == "create" {
								exec(`UPDATE categories SET is_closed=TRUE,version=version+1 WHERE id=$1`, parent.CategoryID)
							} else {
								exec(`UPDATE posts SET author_id=122399 WHERE id=$1`, target.ID)
							}
						}
						if err := tx.Commit(context.Background()); err != nil {
							t.Fatal(err)
						}
					}
					return proof(ctx)
				}
				var result *domain.Post
				switch op {
				case "create":
					result, err = s.CreatePostForPrincipal(ctx, parent.ID, "Owner", domain.CreatePostRequest{Content: "must reject", ParentID: &target.ID}, changing)
				case "edit":
					result, err = s.UpdatePostForPrincipal(ctx, target.ID, "must reject", changing)
				case "delete":
					err = s.DeletePostForPrincipal(ctx, target.ID, changing)
				}
				if err == nil || result != nil {
					t.Fatal("committed Post authority drift accepted", op, mode, result, err)
				}
				if !reflect.DeepEqual(before, postAuthorCounts(t, pool, parent.ID)) {
					t.Fatal("rejected Post drift mutated counter/audit/outbox")
				}
				if mode == "owner_or_board" && op == "create" {
					if _, err := pool.Exec(ctx, `UPDATE categories SET is_closed=FALSE,version=version+1 WHERE id=$1`, parent.CategoryID); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
