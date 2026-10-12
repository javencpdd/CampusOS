package service

import (
	"context"
	"fmt"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestAuthorLifecyclePostgreSQLCommandsAndAuditRollback(t *testing.T) {
	for i, op := range []string{"trash", "restore", "submit_review"} {
		t.Run(op, func(t *testing.T) {
			ctx := context.Background()
			s, repo, pool, row, session, claims := authorPostgres(t, fmt.Sprint(113230+i))
			if op == "restore" {
				row.DeletionStatus = "trashed"
			}
			if op == "submit_review" {
				row.ModerationStatus = "taken_down"
			}
			if err := repo.Update(ctx, row); err != nil {
				t.Fatal(err)
			}
			before, _ := repo.GetByID(ctx, row.ID)
			resolve := authorPgProof(session, claims)
			s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), authorFailAudit{Store: reliability.NewPostgreSQLStore(pool)}))
			if result, err := s.AuthorLifecycleForPrincipal(ctx, row.ID, op, resolve); err == nil || result != nil {
				t.Fatal("required audit failure accepted")
			}
			actual, _ := repo.GetByID(ctx, row.ID)
			if actual.FactsVersion != before.FactsVersion || actual.DeletionStatus != before.DeletionStatus || actual.ModerationStatus != before.ModerationStatus {
				t.Fatal("partial PG lifecycle survived rollback")
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM content_revisions WHERE thread_id=$1`, row.ID).Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
			s.SetReliability(reliability.NewService(transaction.NewPostgreSQL(pool), reliability.NewPostgreSQLStore(pool)))
			result, err := s.AuthorLifecycleForPrincipal(ctx, row.ID, op, resolve)
			if err != nil || result == nil || result.FactsVersion == before.FactsVersion {
				t.Fatal(result, err)
			}
			if op == "trash" && result.DeletionStatus != "trashed" {
				t.Fatal(result)
			}
			if op == "restore" && (result.DeletionStatus != "active" || result.PublicationStatus != before.PublicationStatus || result.ModerationStatus != before.ModerationStatus) {
				t.Fatal("restore widened moderation", result)
			}
			if op == "submit_review" && result.ModerationStatus != "pending" {
				t.Fatal(result)
			}
			var kind, actor string
			if err := pool.QueryRow(ctx, `SELECT actor_type,actor_id FROM platform_command_audits WHERE resource_id=$1`, row.ID).Scan(&kind, &actor); err != nil || kind != "user" || actor != claims.UserID {
				t.Fatal(kind, actor, err)
			}
		})
	}
}
func TestAuthorLifecyclePostgreSQLCommittedStateABA(t *testing.T) {
	for i, op := range []string{"trash", "restore", "submit_review"} {
		t.Run(op, func(t *testing.T) {
			ctx := context.Background()
			s, repo, pool, row, session, claims := authorPostgres(t, fmt.Sprint(113240+i))
			if op == "restore" {
				row.DeletionStatus = "trashed"
			}
			if op == "submit_review" {
				row.ModerationStatus = "taken_down"
			}
			if err := repo.Update(ctx, row); err != nil {
				t.Fatal(err)
			}
			base := authorPgProof(session, claims)
			calls := 0
			resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				if calls == 2 {
					tx, err := pool.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := tx.Exec(ctx, `UPDATE threads SET category_id=113112 WHERE id=$1`, row.ID); err != nil {
						t.Fatal(err)
					}
					if _, err := tx.Exec(ctx, `UPDATE threads SET category_id=113111 WHERE id=$1`, row.ID); err != nil {
						t.Fatal(err)
					}
					if err := tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}
				return base(ctx)
			}
			if result, err := s.AuthorLifecycleForPrincipal(ctx, row.ID, op, resolve); err == nil || result != nil {
				t.Fatal("ABA lifecycle accepted")
			}
			actual, _ := repo.GetByID(ctx, row.ID)
			if actual.DeletionStatus != row.DeletionStatus || actual.ModerationStatus != row.ModerationStatus || actual.CurrentRevision != row.CurrentRevision {
				t.Fatal("stale lifecycle persisted")
			}
		})
	}
}
