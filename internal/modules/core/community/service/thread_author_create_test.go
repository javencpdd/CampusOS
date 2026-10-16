package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func authorCreateFixture(t *testing.T) (*ThreadService, *repository.MemoryCategoryRepository, *repository.MemoryThreadTypePolicyRepository) {
	t.Helper()
	s, _, gov, store := authorUpdateFixture(t)
	cats := repository.NewMemoryCategoryRepository()
	parent := "190106"
	cats.Create(context.Background(), &domain.Category{ID: parent, NodeKind: domain.CategoryNodeGroup, LifecycleStatus: domain.CategoryLifecycleActive, Version: 1})
	cats.Create(context.Background(), &domain.Category{ID: "190103", ParentID: &parent, NodeKind: domain.CategoryNodeBoard, LifecycleStatus: domain.CategoryLifecycleActive, Version: 1})
	types := repository.NewMemoryThreadTypePolicyRepository()
	types.Replace(context.Background(), "190103", []domain.ThreadType{domain.ThreadTypeDiscussion})
	s.SetCategoryRepository(cats)
	s.SetThreadTypePolicyRepository(types)
	s.SetReliability(reliability.NewService(transaction.NewMemory(s.repo.(transaction.Snapshotter), cats, types, gov, store), store))
	return s, cats, types
}
func TestAuthorCreateCurrentUserAndPostingAuthority(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"current", "admin", "missing-proof", "closed", "archived", "group-archived", "type-disabled"} {
		t.Run(mode, func(t *testing.T) {
			s, cats, types := authorCreateFixture(t)
			resolve := authorUpdateProof("190102")
			switch mode {
			case "admin":
				resolve = func(ctx context.Context) (port.PrincipalContext, error) {
					p, _ := authorUpdateProof("190102")(ctx)
					p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
					return p, nil
				}
			case "missing-proof":
				resolve = nil
			case "closed", "archived":
				board, _ := cats.GetByID(ctx, "190103")
				if mode == "closed" {
					board.IsClosed = true
				} else {
					board.LifecycleStatus = "archived"
				}
				cats.Update(ctx, board)
			case "group-archived":
				group, _ := cats.GetByID(ctx, "190106")
				group.LifecycleStatus = "archived"
				cats.Update(ctx, group)
			case "type-disabled":
				types.Replace(ctx, "190103", []domain.ThreadType{domain.ThreadTypeArticle})
			}
			result, err := s.CreateThreadForPrincipal(ctx, "Owner", domain.CreateThreadRequest{CategoryID: "190103", Title: "new post", Content: "body"}, resolve)
			if (err == nil) != (mode == "current") || (err != nil && result != nil) {
				t.Fatal(result, err)
			}
			if result != nil && (result.AuthorID != "190102" || result.FactsVersion < 1) {
				t.Fatal("client owner was used")
			}
		})
	}
}
func TestAuthorCreateAuthorityDriftAndAuditRollback(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"board", "type", "credential", "audit"} {
		t.Run(mode, func(t *testing.T) {
			s, cats, types := authorCreateFixture(t)
			repo := s.repo.(transaction.Snapshotter)
			before := repo.Snapshot()
			calls := 0
			resolve := func(ctx context.Context) (port.PrincipalContext, error) {
				p, _ := authorUpdateProof("190102")(ctx)
				calls++
				if calls == 2 && mode == "board" {
					board, _ := cats.GetByID(ctx, "190103")
					board.IsClosed = true
					cats.Update(ctx, board)
				}
				if calls == 2 && mode == "type" {
					types.Replace(ctx, "190103", []domain.ThreadType{domain.ThreadTypeArticle})
				}
				if calls >= 3 && mode == "credential" {
					p.CredentialID = "changed"
				}
				return p, nil
			}
			if mode == "audit" {
				gov := s.governance.(transaction.Snapshotter)
				store := reliability.NewMemoryStore()
				s.SetReliability(reliability.NewService(transaction.NewMemory(repo, cats, types, gov, store), authorFailAudit{Store: store}))
			}
			result, err := s.CreateThreadForPrincipal(ctx, "Owner", domain.CreateThreadRequest{CategoryID: "190103", Title: "must rollback", Content: "body"}, resolve)
			if err == nil || result != nil {
				t.Fatal("creation drift accepted")
			}
			if !reflect.DeepEqual(before, repo.Snapshot()) {
				t.Fatal("partial created resource persisted")
			}
		})
	}
}
