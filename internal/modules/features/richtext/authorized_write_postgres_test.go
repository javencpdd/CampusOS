package richtext

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
)

type articleLockStore struct {
	Store
	afterLock func(context.Context, *Article)
}

func (s *articleLockStore) LockArticleForAuthorCommand(ctx context.Context, id string) (*Article, error) {
	a, err := s.Store.(authorArticleLocker).LockArticleForAuthorCommand(ctx, id)
	if err == nil && s.afterLock != nil {
		s.afterLock(ctx, a)
	}
	return a, err
}
func runArticlePGWrite(s *Service, ctx context.Context, id, operation string, proof identityport.SessionPrincipalResolver) error {
	switch operation {
	case "update":
		_, err := s.UpdateDraftForPrincipal(ctx, id, SaveArticleRequest{Title: "Edited", ContentHTML: "<p>edited body</p>"}, proof)
		return err
	case "publish":
		_, err := s.PublishForPrincipal(ctx, id, proof)
		return err
	case "offline":
		_, err := s.OfflineForPrincipal(ctx, id, proof)
		return err
	case "trash":
		return s.DeleteForPrincipal(ctx, id, proof)
	}
	return fmt.Errorf("unknown test operation")
}
func TestRichTextAuthorWritePostgreSQLAtomicLocks(t *testing.T) {
	for i, operation := range []string{"update", "publish", "offline", "trash"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			owner := fmt.Sprint(115501 + i)
			s, core, pool, proof, claims := articleCreatePostgres(t, owner)
			request := articleCreateRequest()
			request.CategoryID = "115111"
			draft, err := s.CreateDraftForPrincipal(ctx, "Writer", request, proof)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE threads SET view_count=17,reply_count=3,like_count=2 WHERE id=$1`, draft.ThreadID); err != nil {
				t.Fatal(err)
			}
			original := s.store
			once := false
			s.store = &articleLockStore{Store: original, afterLock: func(ctx context.Context, a *Article) {
				if once {
					return
				}
				once = true
				for _, q := range []struct{ sql, id string }{{`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, owner}, {`SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT`, claims.SessionID}, {`SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT`, draft.ThreadID}, {`SELECT id FROM richtext_article_contents WHERE thread_id=$1 FOR UPDATE NOWAIT`, draft.ThreadID}} {
					_, err := pool.Exec(context.Background(), q.sql, q.id)
					var pe *pgconn.PgError
					if !errors.As(err, &pe) || pe.Code != "55P03" {
						t.Fatal("actual article facts not locked", err)
					}
				}
			}}
			if err := runArticlePGWrite(s, ctx, draft.ThreadID, operation, proof); err != nil {
				t.Fatal(err)
			}
			actual, err := s.store.GetArticleByThreadID(ctx, draft.ThreadID)
			if err != nil || actual.UpdatedBy != owner {
				t.Fatal(actual, err)
			}
			var views, replies, likes int
			if err := pool.QueryRow(ctx, `SELECT view_count,reply_count,like_count FROM threads WHERE id=$1`, draft.ThreadID).Scan(&views, &replies, &likes); err != nil || views != 17 || replies != 3 || likes != 2 {
				t.Fatal("counters", views, replies, likes, err)
			}
			s.store = original
			rollbackDraft, err := s.CreateDraftForPrincipal(ctx, "Writer", request, proof)
			if err != nil {
				t.Fatal(err)
			}
			before, err := s.store.GetArticleByThreadID(ctx, rollbackDraft.ThreadID)
			if err != nil {
				t.Fatal(err)
			}
			counts := articleCreateCounts(t, pool, owner)
			var version int64
			if err := pool.QueryRow(ctx, `SELECT facts_version FROM threads WHERE id=$1`, rollbackDraft.ThreadID).Scan(&version); err != nil {
				t.Fatal(err)
			}
			durable := reliability.NewService(transaction.NewPostgreSQL(pool), articleCreateFailAudit{reliability.NewPostgreSQLStore(pool)})
			core.SetReliability(durable)
			s.SetReliability(durable)
			if err := runArticlePGWrite(s, ctx, rollbackDraft.ThreadID, operation, proof); err == nil {
				t.Fatal("required audit failure accepted")
			}
			after, err := s.store.GetArticleByThreadID(ctx, rollbackDraft.ThreadID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("partial article persisted", after, err)
			}
			actualCounts := articleCreateCounts(t, pool, owner)
			if !reflect.DeepEqual(counts, actualCounts) {
				t.Fatal("partial revision/audit/outbox", counts, actualCounts)
			}
			var actualVersion int64
			if err := pool.QueryRow(ctx, `SELECT facts_version FROM threads WHERE id=$1`, rollbackDraft.ThreadID).Scan(&actualVersion); err != nil || version != actualVersion {
				t.Fatal("partial Core persisted", err)
			}
		})
	}
}
func TestRichTextAuthorWritePostgreSQLCommittedChanges(t *testing.T) {
	for i, operation := range []string{"update", "publish", "offline", "trash"} {
		for j, mode := range []string{"core-aba", "article", "session", "auth-version"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				owner := fmt.Sprint(115511 + i*4 + j)
				s, _, pool, base, claims := articleCreatePostgres(t, owner)
				request := articleCreateRequest()
				request.CategoryID = "115111"
				draft, err := s.CreateDraftForPrincipal(ctx, "Writer", request, base)
				if err != nil {
					t.Fatal(err)
				}
				before := articleCreateCounts(t, pool, owner)
				var originalContent string
				if err := pool.QueryRow(ctx, `SELECT content FROM threads WHERE id=$1`, draft.ThreadID).Scan(&originalContent); err != nil {
					t.Fatal(err)
				}
				calls := 0
				proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
					calls++
					if calls == 2 {
						var err error
						switch mode {
						case "core-aba":
							_, err = pool.Exec(ctx, `UPDATE threads SET content=content||' changed' WHERE id=$1`, draft.ThreadID)
							if err == nil {
								_, err = pool.Exec(ctx, `UPDATE threads SET content=$2 WHERE id=$1`, draft.ThreadID, originalContent)
							}
						case "article":
							_, err = pool.Exec(ctx, `UPDATE richtext_article_contents SET summary='external' WHERE thread_id=$1`, draft.ThreadID)
						case "session":
							_, err = pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, claims.SessionID)
						case "auth-version":
							_, err = pool.Exec(ctx, `UPDATE users SET auth_version=auth_version+1 WHERE id=$1`, owner)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					return base(ctx)
				}
				if err := runArticlePGWrite(s, ctx, draft.ThreadID, operation, proof); err == nil {
					t.Fatal("committed stale facts accepted")
				}
				after := articleCreateCounts(t, pool, owner)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("stale command persisted", before, after)
				}
			})
		}
	}
}
