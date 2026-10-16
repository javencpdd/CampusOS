package service

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func postAuthorFixture(t *testing.T) (*PostService, *ThreadService, *repository.MemoryPostRepository, *reliability.MemoryStore) {
	t.Helper()
	threads, _, _ := authorCreateFixture(t)
	posts := repository.NewMemoryPostRepository()
	store := reliability.NewMemoryStore()
	s := NewPostService(posts, nil)
	s.SetThreadRepository(threads.repo)
	s.SetCategoryRepository(threads.categoryRepo)
	s.SetThreadCommandSource(threads)
	s.SetReliability(reliability.NewService(transaction.NewMemory(posts, threads.repo.(transaction.Snapshotter)), store))
	return s, threads, posts, store
}

func TestPostAuthorCommandsCreateEditDeleteCurrentOwner(t *testing.T) {
	s, threads, posts, store := postAuthorFixture(t)
	ctx := context.Background()
	proof := authorUpdateProof("190102")
	created, err := s.CreatePostForPrincipal(ctx, "190101", "Owner", domain.CreatePostRequest{Content: "original reply"}, proof)
	if err != nil || created == nil || created.AuthorID != "190102" || created.FactsVersion < 1 {
		t.Fatal(created, err)
	}
	parent, _ := threads.repo.GetByID(ctx, "190101")
	if parent.ReplyCount != 1 {
		t.Fatal(parent)
	}
	updated, err := s.UpdatePostForPrincipal(ctx, created.ID, "edited reply", proof)
	if err != nil || updated == nil || updated.Content != "edited reply" || updated.FactsVersion <= created.FactsVersion {
		t.Fatal(updated, err)
	}
	if err := s.DeletePostForPrincipal(ctx, created.ID, proof); err != nil {
		t.Fatal(err)
	}
	deleted, err := posts.ReadPostCommandSnapshot(ctx, created.ID)
	if err != nil || deleted.DeletedAt == nil || deleted.DeletedActorKind != "user" || deleted.DeletedActorID != "190102" {
		t.Fatal(deleted, err)
	}
	parent, _ = threads.repo.GetByID(ctx, "190101")
	if parent.ReplyCount != 0 {
		t.Fatal(parent)
	}
	audits, total, err := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
	if err != nil || total != 3 || len(audits) != 3 {
		t.Fatal(audits, total, err)
	}
	for _, audit := range audits {
		if audit.ActorType != "user" || audit.ActorID != "190102" || audit.ResourceID != created.ID {
			t.Fatal(audit)
		}
	}
}

func TestPostAuthorCommandsDeniedAndAuditAtomic(t *testing.T) {
	for _, operation := range []string{"create", "edit", "delete"} {
		for _, mode := range []string{"other", "admin", "missing", "credential", "parent_aba", "post_aba", "audit"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				s, threads, posts, store := postAuthorFixture(t)
				ctx := context.Background()
				proof := authorUpdateProof("190102")
				var initial *domain.Post
				if operation != "create" {
					var err error
					initial, err = s.CreatePostForPrincipal(ctx, "190101", "Owner", domain.CreatePostRequest{Content: "original reply"}, proof)
					if err != nil {
						t.Fatal(err)
					}
				}
				beforeParent, _ := threads.repo.GetByID(ctx, "190101")
				beforePosts := posts.Snapshot()
				_, auditBefore, _ := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
				switch mode {
				case "other":
					proof = authorUpdateProof("190104")
				case "admin":
					proof = func(context.Context) (identityport.PrincipalContext, error) {
						p, _ := authorUpdateProof("190102")(ctx)
						p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
						return p, nil
					}
				case "missing":
					proof = nil
				case "audit":
					s.SetReliability(reliability.NewService(transaction.NewMemory(posts, threads.repo.(transaction.Snapshotter)), authorFailAudit{Store: store}))
				}
				calls := 0
				changing := proof
				if proof != nil {
					changing = func(ctx context.Context) (identityport.PrincipalContext, error) {
						calls++
						if calls == 2 {
							if mode == "parent_aba" {
								p, _ := threads.repo.GetByID(ctx, "190101")
								p.Content = "changed"
								threads.repo.Update(ctx, p)
								p.Content = beforeParent.Content
								threads.repo.Update(ctx, p)
							}
							if mode == "post_aba" && initial != nil {
								p, _ := posts.GetByID(ctx, initial.ID)
								p.Content = "changed"
								posts.Update(ctx, p)
								p.Content = initial.Content
								posts.Update(ctx, p)
							}
						}
						if mode == "credential" && calls >= 2 {
							return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
						}
						return proof(ctx)
					}
				}
				var result *domain.Post
				var err error
				switch operation {
				case "create":
					result, err = s.CreatePostForPrincipal(ctx, "190101", "Owner", domain.CreatePostRequest{Content: "must not persist"}, changing)
				case "edit":
					result, err = s.UpdatePostForPrincipal(ctx, initial.ID, "must not persist", changing)
				case "delete":
					err = s.DeletePostForPrincipal(ctx, initial.ID, changing)
				}
				// Post ABA is irrelevant when the resource has not been created yet.
				if operation == "create" && mode == "post_aba" {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				if err == nil || result != nil {
					t.Fatal("denied or failed command accepted", result, err)
				}
				parent, _ := threads.repo.GetByID(ctx, "190101")
				if parent.ReplyCount != beforeParent.ReplyCount {
					t.Fatal("failed command changed parent counter", parent)
				}
				if !reflect.DeepEqual(beforePosts, posts.Snapshot()) {
					t.Fatal("failed command changed Post state")
				}
				_, auditAfter, _ := store.ListCommandAudits(ctx, reliability.PageRequest{Page: 1, PageSize: 20})
				if auditAfter != auditBefore {
					t.Fatal("failed audit committed", auditBefore, auditAfter)
				}
				if mode == "audit" && !strings.Contains(err.Error(), "required audit failed") {
					t.Fatal(err)
				}
			})
		}
	}
}
