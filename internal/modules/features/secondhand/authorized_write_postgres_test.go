package secondhand

import (
	"context"
	"errors"
	"fmt"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgconn"
)

type authorLockStore struct {
	Store
	afterLock func(context.Context, *Detail)
}

func (s *authorLockStore) LockDetailForAuthorCommand(ctx context.Context, id string) (*Detail, error) {
	d, err := s.Store.(authorDetailLocker).LockDetailForAuthorCommand(ctx, id)
	if err == nil && s.afterLock != nil {
		s.afterLock(ctx, d)
	}
	return d, err
}
func TestSecondhandAuthorWritePostgreSQLAtomicLocksAndCounters(t *testing.T) {
	for i, status := range []bool{false, true} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			ctx := context.Background()
			owner := fmt.Sprint(114701 + i)
			s, core, pool, proof, claims := typedCreatePostgres(t, owner)
			req := validCreateRequest()
			req.CategoryID = "114111"
			created, err := s.CreateForPrincipal(ctx, "Writer", req, proof)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE threads SET view_count=17,reply_count=3,like_count=2 WHERE id=$1`, created.Thread.ID); err != nil {
				t.Fatal(err)
			}
			once := false
			s.store = &authorLockStore{Store: s.store, afterLock: func(ctx context.Context, d *Detail) {
				if once {
					return
				}
				once = true
				for _, q := range []struct{ sql, id string }{{`SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, owner}, {`SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT`, claims.SessionID}, {`SELECT id FROM threads WHERE id=$1 FOR UPDATE NOWAIT`, created.Thread.ID}, {`SELECT thread_id FROM secondhand_details WHERE thread_id=$1 FOR UPDATE NOWAIT`, created.Thread.ID}} {
					_, err := pool.Exec(context.Background(), q.sql, q.id)
					var pe *pgconn.PgError
					if !errors.As(err, &pe) || pe.Code != "55P03" {
						t.Fatal("actual author facts not locked", err)
					}
				}
			}}
			run := func(version int64) (*Result, error) {
				if status {
					return s.UpdateStatusForPrincipal(ctx, created.Thread.ID, UpdateStatusRequest{TradeStatus: TradeStatusReserved, Version: version}, proof)
				}
				request := typedAuthorUpdateRequest()
				request.Version = version
				return s.UpdateForPrincipal(ctx, created.Thread.ID, request, proof)
			}
			result, err := run(1)
			if err != nil || result == nil || result.Detail.Version != 2 || result.Detail.CreatedBy != owner || result.Thread.ViewCount != 17 || result.Thread.ReplyCount != 3 || result.Thread.LikeCount != 2 {
				t.Fatal(result, err)
			}
			before := typedCreateCounts(t, pool, owner)
			var oldTitle string
			var oldVersion int64
			if err := pool.QueryRow(ctx, `SELECT title,facts_version FROM threads WHERE id=$1`, created.Thread.ID).Scan(&oldTitle, &oldVersion); err != nil {
				t.Fatal(err)
			}
			if status {
				s.store = NewPgStore(pool)
				if _, err := pool.Exec(ctx, `UPDATE secondhand_details SET trade_status='available' WHERE thread_id=$1`, created.Thread.ID); err != nil {
					t.Fatal(err)
				}
			}
			durable := reliability.NewService(transaction.NewPostgreSQL(pool), createFailAudit{reliability.NewPostgreSQLStore(pool)})
			core.SetReliability(durable)
			s.SetReliability(durable)
			if result, err := run(2); err == nil || result != nil {
				t.Fatal("required audit failure accepted")
			}
			after := typedCreateCounts(t, pool, owner)
			for i := range before {
				if before[i] != after[i] {
					t.Fatal("partial audit/outbox/revision", before, after)
				}
			}
			var title string
			var facts, detailVersion int64
			if err := pool.QueryRow(ctx, `SELECT title,facts_version,(SELECT version FROM secondhand_details WHERE thread_id=threads.id) FROM threads WHERE id=$1`, created.Thread.ID).Scan(&title, &facts, &detailVersion); err != nil || title != oldTitle || facts != oldVersion || detailVersion != 2 {
				t.Fatal("partial author write persisted", title, facts, detailVersion, err)
			}
		})
	}
}
func TestSecondhandAuthorWritePostgreSQLCommittedChanges(t *testing.T) {
	for i, mode := range []string{"board-aba", "body-aba", "detail", "session", "auth-version"} {
		for j, status := range []bool{false, true} {
			t.Run(fmt.Sprint(mode, "/", status), func(t *testing.T) {
				ctx := context.Background()
				owner := fmt.Sprint(114711 + i*2 + j)
				s, _, pool, base, claims := typedCreatePostgres(t, owner)
				if _, err := pool.Exec(ctx, `INSERT INTO categories(id,name,slug,parent_id,node_kind,lifecycle_status) VALUES(114112,'Other typed writer','typed-writer-other',114110,'board','active') ON CONFLICT(id) DO NOTHING; INSERT INTO category_thread_type_policies(category_id,thread_type,enabled) VALUES(114112,'mutual_aid',TRUE),(114112,'secondhand',TRUE) ON CONFLICT(category_id,thread_type) DO NOTHING`); err != nil {
					t.Fatal(err)
				}
				req := validCreateRequest()
				req.CategoryID = "114111"
				created, err := s.CreateForPrincipal(ctx, "Writer", req, base)
				if err != nil {
					t.Fatal(err)
				}
				before := typedCreateCounts(t, pool, owner)
				calls := 0
				proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
					calls++
					if calls == 2 {
						var err error
						switch mode {
						case "board-aba":
							_, err = pool.Exec(ctx, `UPDATE threads SET category_id=114112 WHERE id=$1`, created.Thread.ID)
							if err == nil {
								_, err = pool.Exec(ctx, `UPDATE threads SET category_id=114111 WHERE id=$1`, created.Thread.ID)
							}
						case "body-aba":
							_, err = pool.Exec(ctx, `UPDATE threads SET content=content||' changed' WHERE id=$1`, created.Thread.ID)
							if err == nil {
								_, err = pool.Exec(ctx, `UPDATE threads SET content=$2 WHERE id=$1`, created.Thread.ID, created.Thread.Content)
							}
						case "detail":
							_, err = pool.Exec(ctx, `UPDATE secondhand_details SET location_scope='external',version=version+1 WHERE thread_id=$1`, created.Thread.ID)
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
				var result *Result
				if status {
					result, err = s.UpdateStatusForPrincipal(ctx, created.Thread.ID, UpdateStatusRequest{TradeStatus: TradeStatusReserved, Version: 1}, proof)
				} else {
					result, err = s.UpdateForPrincipal(ctx, created.Thread.ID, typedAuthorUpdateRequest(), proof)
				}
				if err == nil || result != nil {
					t.Fatal("committed stale authority accepted", result, err)
				}
				after := typedCreateCounts(t, pool, owner)
				for i := range before {
					if before[i] != after[i] {
						t.Fatal("stale command persisted", before, after)
					}
				}
			})
		}
	}
}
