package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestAuthorCreatePostgreSQLCurrentAuthorityAndRollback(t *testing.T) {
	ctx := context.Background()
	s, _, pool, _, session, claims := authorPostgres(t, "113251")
	s.SetCategoryRepository(repository.NewPgCategoryRepository(pool))
	s.SetThreadTypePolicyRepository(repository.NewPgThreadTypePolicyRepository(pool))
	resolve := authorPgProof(session, claims)
	req := domain.CreateThreadRequest{Title: "created owner post", Content: "body", CategoryID: "113111", IsPrivate: true}
	result, err := s.CreateThreadForPrincipal(ctx, "Writer", req, resolve)
	if err != nil || result == nil || result.AuthorID != claims.UserID || result.PublicationStatus != "private" {
		t.Fatal(result, err)
	}
	var actor, kind string
	if err := pool.QueryRow(ctx, `SELECT actor_id,actor_type FROM platform_command_audits WHERE resource_id=$1`, result.ID).Scan(&actor, &kind); err != nil || actor != claims.UserID || kind != "user" {
		t.Fatal(actor, kind, err)
	}
	var before, after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM threads WHERE author_id=$1`, claims.UserID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), authorFailAudit{Store: reliability.NewPostgreSQLStore(pool)}))
	if result, err := s.CreateThreadForPrincipal(ctx, "Writer", req, resolve); err == nil || result != nil {
		t.Fatal("audit failure created resource")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM threads WHERE author_id=$1`, claims.UserID).Scan(&after); err != nil || after != before {
		t.Fatal("partial creation survived", before, after, err)
	}
}
func TestAuthorCreatePostgreSQLCommittedAuthorityChanges(t *testing.T) {
	for i, mode := range []string{"board", "group", "type", "session"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, _, pool, _, session, claims := authorPostgres(t, fmt.Sprint(113252+i))
			s.SetCategoryRepository(repository.NewPgCategoryRepository(pool))
			s.SetThreadTypePolicyRepository(repository.NewPgThreadTypePolicyRepository(pool))
			base := authorPgProof(session, claims)
			calls := 0
			resolve := func(ctx context.Context) (port.PrincipalContext, error) {
				calls++
				if calls == 2 {
					var err error
					switch mode {
					case "board":
						_, err = pool.Exec(ctx, `UPDATE categories SET is_closed=TRUE,version=version+1 WHERE id=113111`)
					case "group":
						_, err = pool.Exec(ctx, `UPDATE categories SET version=version+1 WHERE id=113110`)
					case "type":
						_, err = pool.Exec(ctx, `UPDATE category_thread_type_policies SET enabled=FALSE,updated_at=now() WHERE category_id=113111 AND thread_type='discussion'`)
					case "session":
						_, err = pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, claims.SessionID)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				return base(ctx)
			}
			var before, after int
			pool.QueryRow(ctx, `SELECT count(*) FROM threads WHERE author_id=$1`, claims.UserID).Scan(&before)
			result, err := s.CreateThreadForPrincipal(ctx, "Writer", domain.CreateThreadRequest{Title: "must not persist", Content: "body", CategoryID: "113111"}, resolve)
			if err == nil || result != nil {
				t.Fatal("stale creator accepted")
			}
			pool.QueryRow(ctx, `SELECT count(*) FROM threads WHERE author_id=$1`, claims.UserID).Scan(&after)
			if before != after {
				t.Fatal("partial create persisted")
			}
			// Each subtest owns its mutation and restores the shared fixture board.
			if _, err := pool.Exec(ctx, `UPDATE categories SET is_closed=FALSE,lifecycle_status='active',version=version+1 WHERE id IN(113110,113111); UPDATE category_thread_type_policies SET enabled=TRUE,updated_at=now() WHERE category_id=113111 AND thread_type='discussion'`); err != nil {
				t.Fatal(err)
			}
		})
	}
}
