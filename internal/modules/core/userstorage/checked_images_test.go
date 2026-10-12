package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	community "github.com/campusos/CampusOS/internal/modules/core/community"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	communityport "github.com/campusos/CampusOS/internal/modules/core/community/port"
	communityrepo "github.com/campusos/CampusOS/internal/modules/core/community/repository"
	communitysvc "github.com/campusos/CampusOS/internal/modules/core/community/service"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func imageProof(owner string) identityport.SessionPrincipalResolver {
	return func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: owner}, Audience: "user", AuthenticationStrength: "password", CredentialID: "image-session"}, nil
	}
}

func imageFixture(t *testing.T) (*ContentImageStore, *ContentImage, *communityrepo.MemoryThreadRepository, communityport.AuthenticatedThreadQuery) {
	t.Helper()
	adapter, err := NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	images, err := NewContentImageStore(adapter, adapter, DefaultMaxContentImage)
	if err != nil {
		t.Fatal(err)
	}
	a, err := images.Save("1001", "image.png", bytes.NewReader(testPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	repo := communityrepo.NewMemoryThreadRepository()
	thread := &domain.Thread{ID: "1234", AuthorID: "1001", CategoryID: "1", ThreadType: domain.ThreadTypeDiscussion, ContentFormat: "safe_html", Content: `<p>body</p><img src="` + a.FileURL + `">`, Title: "Image", PublicationStatus: domain.PublicationStatusPublished, ModerationStatus: domain.ModerationStatusClear, DeletionStatus: domain.DeletionStatusActive}
	if err := repo.Create(context.Background(), thread); err != nil {
		t.Fatal(err)
	}
	query := community.NewContentQuery(communitysvc.NewThreadService(repo, nil)).(communityport.AuthenticatedThreadQuery)
	return images, a, repo, query
}

func TestContentImageCheckedStateMatrix(t *testing.T) {
	ctx := context.Background()
	images, asset, repo, query := imageFixture(t)
	for _, publication := range []domain.PublicationStatus{"draft", "private", "published"} {
		for _, moderation := range []domain.ModerationStatus{"clear", "pending", "rejected", "taken_down"} {
			for _, deletion := range []domain.DeletionStatus{"active", "trashed", "purged"} {
				t.Run(string(publication)+"/"+string(moderation)+"/"+string(deletion), func(t *testing.T) {
					thread, _ := repo.GetByID(ctx, "1234")
					thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus = publication, moderation, deletion
					if err := repo.Update(ctx, thread); err != nil {
						t.Fatal(err)
					}
					admin := func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, _ := imageProof("1001")(ctx)
						p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
						return p, nil
					}
					for _, actor := range []struct {
						name  string
						proof identityport.SessionPrincipalResolver
						allow bool
					}{
						{"public", nil, publication == "published" && moderation == "clear" && deletion == "active"},
						{"owner", imageProof("1001"), deletion == "active"},
						{"other", imageProof("1002"), publication == "published" && moderation == "clear" && deletion == "active"},
						{"same-ID-admin", admin, false},
					} {
						image, err := images.ReadForPrincipal(ctx, "1001", asset.FileName, "1234", query, actor.proof)
						if (err == nil) != actor.allow || (err != nil && len(image.Data) != 0) {
							t.Fatalf("%s bytes=%d err=%v", actor.name, len(image.Data), err)
						}
					}
				})
			}
		}
	}
}

type imageQueryHook struct {
	communityport.AuthenticatedThreadQuery
	gets  int
	after func(int)
}

func (q *imageQueryHook) GetThreadForPrincipal(ctx context.Context, id string, resolve func(context.Context) (identityport.PrincipalContext, error)) (*domain.Thread, error) {
	r, e := q.AuthenticatedThreadQuery.GetThreadForPrincipal(ctx, id, resolve)
	q.gets++
	if q.after != nil {
		q.after(q.gets)
	}
	return r, e
}

func TestContentImageCheckedBindingAndRevocation(t *testing.T) {
	for _, mode := range []string{"owner", "body", "state", "trash", "body-ABA", "bytes", "session", "credential"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			images, asset, repo, query := imageFixture(t)
			q := &imageQueryHook{AuthenticatedThreadQuery: query}
			revoked := false
			q.after = func(n int) {
				if n != 1 {
					return
				}
				if mode == "session" || mode == "credential" {
					revoked = true
					return
				}
				thread, _ := repo.GetByID(ctx, "1234")
				switch mode {
				case "owner":
					thread.AuthorID = "1002"
				case "body":
					thread.Content = "<p>no image</p>"
				case "state":
					thread.PublicationStatus = "private"
				case "trash":
					thread.DeletionStatus = "trashed"
				case "body-ABA":
					old := thread.Content
					thread.Content = "<p>changed</p>"
					if err := repo.Update(ctx, thread); err != nil {
						t.Fatal(err)
					}
					thread.Content = old
				case "bytes":
					return
				}
				if err := repo.Update(ctx, thread); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "bytes" {
				q.after = func(n int) {
					if n == 2 {
						path, _ := images.Path("1001", asset.FileName)
						data := testPNG(t)
						data[len(data)-1] ^= 1
						if err := os.WriteFile(path, data, 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			var resolve identityport.SessionPrincipalResolver
			if mode == "session" || mode == "credential" {
				resolve = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, _ := imageProof("1001")(ctx)
					if revoked {
						if mode == "session" {
							return p, identityport.ErrUserSessionPrincipalUnavailable
						}
						p.CredentialID = "other-session"
					}
					return p, nil
				}
			}
			image, err := images.ReadForPrincipal(ctx, "1001", asset.FileName, "1234", q, resolve)
			if err == nil || len(image.Data) != 0 {
				t.Fatalf("stale image bytes=%d err=%v", len(image.Data), err)
			}
		})
	}
	images, a, repo, q := imageFixture(t)
	ctx := context.Background()
	for _, body := range []string{"<p>" + a.FileURL + "</p>", `<a href="` + a.FileURL + `">x</a>`, `<img src="` + a.FileURL + `.suffix">`} {
		thread, _ := repo.GetByID(ctx, "1234")
		thread.Content = body
		repo.Update(ctx, thread)
		if img, err := images.ReadForPrincipal(ctx, "1001", a.FileName, "1234", q, nil); err == nil || len(img.Data) != 0 {
			t.Fatal("non-image reference admitted")
		}
	}
	if _, err := images.ReadForPrincipal(ctx, "1001", a.FileName, "1234", nil, nil); !errors.Is(err, identityport.ErrPolicyDecisionMismatch) {
		t.Fatal(err)
	}
	if img, err := images.ReadForPrincipal(ctx, "1001", a.FileName, "", nil, nil); err == nil || len(img.Data) != 0 {
		t.Fatal("anonymous unbound image")
	}
	if img, err := images.ReadForPrincipal(ctx, "1001", a.FileName, "", nil, imageProof("1001")); err != nil || len(img.Data) == 0 {
		t.Fatal(err)
	}
	if _, err := images.ReadForPrincipal(ctx, "1001", a.FileName, "", nil, imageProof("1002")); err == nil {
		t.Fatal("cross owner")
	}
	path, _ := images.Path("1001", a.FileName)
	os.Remove(path)
	os.Symlink("/etc/passwd", path)
	if img, err := images.ReadForPrincipal(ctx, "1001", a.FileName, "", nil, imageProof("1001")); err == nil || len(img.Data) != 0 {
		t.Fatal("symlink read")
	}
	if _, err := images.ListForPrincipal(ctx, nil); !errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable) {
		t.Fatal(err)
	}
}
