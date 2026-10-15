package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	"github.com/campusos/CampusOS/internal/modules/core/community/repository"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func postReadMemory(t *testing.T) (*PostService, *repository.MemoryThreadRepository, *repository.MemoryPostRepository, *domain.Post) {
	t.Helper()
	s, threads, posts, _ := postAuthorFixture(t)
	p := &domain.Post{ID: "135201", ThreadID: "190101", AuthorID: "190102", AuthorName: "Owner", Content: "private reply", Status: "published", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := posts.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return s, threads.repo.(*repository.MemoryThreadRepository), posts, p
}

func TestPostReadCurrentPrincipalAndParentMatrix(t *testing.T) {
	ctx := context.Background()
	for _, actor := range []string{"anonymous", "owner", "other", "admin"} {
		for _, pub := range []domain.PublicationStatus{"draft", "published", "private"} {
			for _, mod := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
				for _, del := range []domain.DeletionStatus{"active", "trashed", "purged"} {
					t.Run(fmt.Sprint(actor, "/", pub, "/", mod, "/", del), func(t *testing.T) {
						s, threads, _, post := postReadMemory(t)
						parent, _ := threads.GetByID(ctx, post.ThreadID)
						parent.PublicationStatus, parent.ModerationStatus, parent.DeletionStatus = pub, mod, del
						if err := threads.Update(ctx, parent); err != nil {
							t.Fatal(err)
						}
						var proof identityport.SessionPrincipalResolver
						switch actor {
						case "owner":
							proof = authorUpdateProof("190102")
						case "other":
							proof = authorUpdateProof("190104")
						case "admin":
							proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
								p, _ := authorUpdateProof("190102")(ctx)
								p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
								return p, nil
							}
						}
						rows, total, err := s.ListPostsForPrincipal(ctx, post.ThreadID, 1, 20, proof)
						want := actor != "admin" && del == "active" && (actor == "owner" || (pub == "published" && mod == "clear"))
						if (err == nil) != want || (want && (len(rows) != 1 || total != 1 || rows[0].Content != post.Content)) || (!want && (rows != nil || total != 0)) {
							t.Fatal(rows, total, err, want)
						}
						got, err := s.GetPostForPrincipal(ctx, post.ThreadID, post.ID, proof)
						if (err == nil) != want || (!want && got != nil) {
							t.Fatal(got, err, want)
						}
					})
				}
			}
		}
	}
}

type postReadProbe struct {
	repository.PostRepository
	snapshots repository.PostReadSnapshotRepository
	after     func()
	calls     int
}

func (r *postReadProbe) ReadPublishedPostSnapshot(ctx context.Context, threadID string, version int64, viewer, id string, page, size int) ([]*domain.Post, int64, error) {
	rows, total, err := r.snapshots.ReadPublishedPostSnapshot(ctx, threadID, version, viewer, id, page, size)
	r.calls++
	if r.calls == 1 && r.after != nil {
		r.after()
	}
	return rows, total, err
}
func TestPostReadWholePageAndEmptyRechecks(t *testing.T) {
	for _, mode := range []string{"parent_private", "parent_aba", "post_aba", "member", "offpage", "order", "owner", "credential", "strength", "empty_parent", "empty_credential", "counters"} {
		t.Run(mode, func(t *testing.T) {
			s, threads, posts, post := postReadMemory(t)
			ctx := context.Background()
			parent, _ := threads.GetByID(ctx, post.ThreadID)
			parent.PublicationStatus = "published"
			parent.ModerationStatus = "clear"
			parent.DeletionStatus = "active"
			threads.Update(ctx, parent)
			probe := &postReadProbe{PostRepository: posts, snapshots: posts, after: func() {
				switch mode {
				case "parent_private", "empty_parent":
					parent.PublicationStatus = "private"
					threads.Update(ctx, parent)
				case "parent_aba":
					original := parent.Content
					parent.Content = "temporary"
					threads.Update(ctx, parent)
					parent.Content = original
					threads.Update(ctx, parent)
				case "post_aba":
					x, _ := posts.GetByID(ctx, post.ID)
					x.Content = "temporary"
					posts.Update(ctx, x)
					x.Content = post.Content
					posts.Update(ctx, x)
				case "member":
					posts.Delete(ctx, post.ID)
				case "offpage":
					x := *post
					x.ID = "135202"
					x.FloorNumber = 10
					posts.Create(ctx, &x)
				case "order":
					x, _ := posts.GetByID(ctx, post.ID)
					x.FloorNumber = 9
					posts.Update(ctx, x)
				case "owner":
					x, _ := posts.GetByID(ctx, post.ID)
					x.AuthorID = "190104"
					posts.Update(ctx, x)
				case "counters":
					x, _ := posts.GetByID(ctx, post.ID)
					x.LikeCount = 9
					posts.Update(ctx, x)
				}
			}}
			s.repo = probe
			var proof identityport.SessionPrincipalResolver
			if mode == "credential" || mode == "strength" || mode == "empty_credential" {
				calls := 0
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					calls++
					p, _ := authorUpdateProof("190102")(ctx)
					if calls > 1 {
						if mode == "strength" {
							p.AuthenticationStrength = "mfa"
						} else {
							p.CredentialID = "changed"
						}
					}
					return p, nil
				}
			}
			page := 1
			if mode == "empty_parent" || mode == "empty_credential" {
				page = 99
			}
			rows, total, err := s.ListPostsForPrincipal(ctx, post.ThreadID, page, 1, proof)
			if mode == "counters" {
				if err != nil || total != 1 || len(rows) != 1 || rows[0].LikeCount != 9 {
					t.Fatal(rows, total, err)
				}
				return
			}
			if err == nil || rows != nil || total != 0 {
				t.Fatal("changed whole page returned", rows, total, err)
			}
		})
	}
}

type postReadBareRepo struct{ repository.PostRepository }

func TestPostReadMissingFactsAndBoundedPagination(t *testing.T) {
	ctx := context.Background()
	s, threads, posts, post := postReadMemory(t)
	parent, _ := threads.GetByID(ctx, post.ThreadID)
	parent.PublicationStatus = "published"
	parent.ModerationStatus = "clear"
	parent.DeletionStatus = "active"
	threads.Update(ctx, parent)
	for _, args := range [][2]int{{0, 20}, {1, 101}, {int(^uint(0) >> 1), 100}} {
		rows, total, err := s.ListPostsForPrincipal(ctx, post.ThreadID, args[0], args[1], nil)
		if err == nil || rows != nil || total != 0 {
			t.Fatal(rows, total, err)
		}
	}
	s.repo = postReadBareRepo{posts}
	if rows, total, err := s.ListPostsForPrincipal(ctx, post.ThreadID, 1, 20, nil); err == nil || rows != nil || total != 0 {
		t.Fatal(rows, total, err)
	}
	s.repo = posts
	x := *post
	x.ID = "135202"
	x.Status = "pending"
	posts.Create(ctx, &x)
	x = *post
	x.ID = "135203"
	posts.Create(ctx, &x)
	posts.Delete(ctx, x.ID)
	rows, total, err := s.ListPostsForPrincipal(ctx, post.ThreadID, 1, 20, nil)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatal(rows, total, err)
	}
	if got, err := s.GetPostForPrincipal(ctx, "different-parent", post.ID, nil); err == nil || got != nil {
		t.Fatal(got, err)
	}
	if got, err := s.GetPostForPrincipal(ctx, post.ThreadID, "135202", nil); !errors.Is(err, repository.ErrPostNotFound) || got != nil {
		t.Fatal(got, err)
	}
	if got, err := s.GetPostForPrincipal(ctx, post.ThreadID, "", nil); !errors.Is(err, repository.ErrPostNotFound) || got != nil {
		t.Fatal(got, err)
	}
	proof := func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
	}
	if rows, total, err := s.ListPostsForPrincipal(ctx, post.ThreadID, 1, 20, proof); err == nil || rows != nil || total != 0 {
		t.Fatal(rows, total, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if rows, total, err := s.ListPostsForPrincipal(canceled, post.ThreadID, 1, 20, nil); !errors.Is(err, context.Canceled) || rows != nil || total != 0 {
		t.Fatal(rows, total, err)
	}
}
