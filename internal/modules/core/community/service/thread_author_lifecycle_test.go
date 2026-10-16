package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestAuthorLifecycleActualStatesAndDomains(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"trash", "restore", "submit_review"} {
		for _, pub := range []domain.PublicationStatus{"draft", "private", "published"} {
			for _, mod := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
				for _, del := range []domain.DeletionStatus{"active", "trashed", "purged"} {
					t.Run(op+"/"+string(pub)+"/"+string(mod)+"/"+string(del), func(t *testing.T) {
						for _, kind := range []string{"owner", "other", "admin"} {
							s, repo, _, _ := authorUpdateFixture(t)
							row, _ := repo.GetByID(ctx, "190101")
							row.PublicationStatus, row.ModerationStatus, row.DeletionStatus = pub, mod, del
							repo.Update(ctx, row)
							resolve := authorUpdateProof("190102")
							if kind == "other" {
								resolve = authorUpdateProof("190104")
							}
							if kind == "admin" {
								resolve = func(ctx context.Context) (port.PrincipalContext, error) {
									p, _ := authorUpdateProof("190102")(ctx)
									p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
									return p, nil
								}
							}
							allow := kind == "owner" && ((op == "trash" && del == "active") || (op == "restore" && del == "trashed") || (op == "submit_review" && del == "active" && (mod == "rejected" || mod == "taken_down")))
							result, err := s.AuthorLifecycleForPrincipal(ctx, row.ID, op, resolve)
							if (err == nil) != allow || (err != nil && result != nil) {
								t.Fatalf("%s allow=%t result=%v err=%v", kind, allow, result, err)
							}
							if !allow {
								actual, _ := repo.GetByID(ctx, row.ID)
								if actual.FactsVersion != row.FactsVersion {
									t.Fatal("denied lifecycle changed resource")
								}
							}
						}
					})
				}
			}
		}
	}
}
func TestAuthorLifecycleCurrentProofAndRequiredAuditRollback(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"trash", "restore", "submit_review"} {
		for _, mode := range []string{"credential", "audit", "board-ABA"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				s, repo, gov, store := authorUpdateFixture(t)
				row, _ := repo.GetByID(ctx, "190101")
				if op == "restore" {
					row.DeletionStatus = "trashed"
				}
				if op == "submit_review" {
					row.ModerationStatus = "taken_down"
				}
				repo.Update(ctx, row)
				before := gov.Snapshot()
				calls := 0
				resolve := func(ctx context.Context) (port.PrincipalContext, error) {
					p, _ := authorUpdateProof("190102")(ctx)
					calls++
					if calls >= 3 && mode == "credential" {
						p.CredentialID = "changed-session"
					}
					if calls == 2 && mode == "board-ABA" {
						copy, _ := repo.GetByID(ctx, row.ID)
						copy.CategoryID = "190105"
						repo.Update(ctx, copy)
						copy.CategoryID = row.CategoryID
						repo.Update(ctx, copy)
					}
					return p, nil
				}
				if mode == "audit" {
					s.SetReliability(reliability.NewService(transaction.NewMemory(repo, gov, store), authorFailAudit{Store: store}))
				}
				result, err := s.AuthorLifecycleForPrincipal(ctx, row.ID, op, resolve)
				if err == nil || result != nil {
					t.Fatal("stale lifecycle returned allow")
				}
				actual, _ := repo.GetByID(ctx, row.ID)
				if actual.DeletionStatus != row.DeletionStatus || actual.ModerationStatus != row.ModerationStatus || actual.CurrentRevision != row.CurrentRevision || !reflect.DeepEqual(before, gov.Snapshot()) {
					t.Fatal("partial lifecycle remained after rollback")
				}
			})
		}
	}
}
