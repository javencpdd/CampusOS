package richtext

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

func richReadProof(owner string) identityport.SessionPrincipalResolver {
	return func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: owner}, Audience: "user", AuthenticationStrength: "password", CredentialID: "richtext-verified-session"}, nil
	}
}

func TestRichTextAuthorizedReadStateMatrix(t *testing.T) {
	for _, publication := range []domain.PublicationStatus{domain.PublicationStatusPublished, domain.PublicationStatusPrivate, domain.PublicationStatusDraft} {
		for _, moderation := range []domain.ModerationStatus{domain.ModerationStatusClear, domain.ModerationStatusPending, domain.ModerationStatusRejected, domain.ModerationStatusTakenDown} {
			for _, deletion := range []domain.DeletionStatus{domain.DeletionStatusActive, domain.DeletionStatusTrashed, domain.DeletionStatusPurged} {
				t.Run(string(publication)+"/"+string(moderation)+"/"+string(deletion), func(t *testing.T) {
					s, repo := newTestService(true)
					draft := createAttachmentDraft(t, s)
					thread, err := repo.GetByID(context.Background(), draft.ThreadID)
					if err != nil {
						t.Fatal(err)
					}
					thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus = publication, moderation, deletion
					if err := repo.Update(context.Background(), thread); err != nil {
						t.Fatal(err)
					}
					admin := func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, _ := richReadProof("1001")(ctx)
						p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
						return p, nil
					}
					for _, actor := range []struct {
						name  string
						proof identityport.SessionPrincipalResolver
						allow bool
					}{
						{"anonymous", nil, publication == domain.PublicationStatusPublished && moderation == domain.ModerationStatusClear && deletion == domain.DeletionStatusActive},
						{"owner", richReadProof("1001"), deletion == domain.DeletionStatusActive},
						{"other", richReadProof("1002"), publication == domain.PublicationStatusPublished && moderation == domain.ModerationStatusClear && deletion == domain.DeletionStatusActive},
						{"same-ID-admin", admin, false},
					} {
						article, err := s.GetArticleForPrincipal(context.Background(), draft.ThreadID, actor.proof)
						if (err == nil) != actor.allow || (!actor.allow && article != nil) {
							t.Fatalf("%s article=%v err=%v", actor.name, article, err)
						}
						if actor.name == "anonymous" && article != nil && len(article.Attachments) != 0 {
							t.Fatal("anonymous attachment metadata")
						}
					}
					stored, _ := repo.GetByID(context.Background(), draft.ThreadID)
					if stored.ViewCount != 0 {
						t.Fatal("projection inflated views")
					}
				})
			}
		}
	}
}

type richReadStore struct {
	Store
	gets     int
	afterGet func(int)
}

func (r *richReadStore) GetArticleByThreadID(ctx context.Context, id string) (*Article, error) {
	value, err := r.Store.GetArticleByThreadID(ctx, id)
	r.gets++
	if r.afterGet != nil {
		r.afterGet(r.gets)
	}
	return value, err
}

func TestRichTextAuthorizedReadRechecksBodyAndCoreFacts(t *testing.T) {
	for _, mode := range []string{"body", "owner", "publication", "trash", "credential"} {
		t.Run(mode, func(t *testing.T) {
			s, repo := newTestService(true)
			draft := createAttachmentDraft(t, s)
			if mode == "publication" {
				if _, err := s.Publish(context.Background(), draft.ThreadID, "1001"); err != nil {
					t.Fatal(err)
				}
			}
			base := s.store
			store := &richReadStore{Store: base}
			store.afterGet = func(n int) {
				if n != 1 || mode == "credential" {
					return
				}
				if mode == "body" {
					a, err := base.GetArticleByThreadID(context.Background(), draft.ThreadID)
					if err != nil {
						t.Fatal(err)
					}
					a.SanitizedHTML = "<p>changed-body</p>"
					if err := base.UpdateArticle(context.Background(), a); err != nil {
						t.Fatal(err)
					}
					return
				}
				thread, _ := repo.GetByID(context.Background(), draft.ThreadID)
				switch mode {
				case "owner":
					thread.AuthorID = "1002"
				case "publication":
					thread.PublicationStatus = domain.PublicationStatusPrivate
				case "trash":
					thread.DeletionStatus = domain.DeletionStatusTrashed
				}
				if err := repo.Update(context.Background(), thread); err != nil {
					t.Fatal(err)
				}
			}
			s.store = store
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, _ := richReadProof("1001")(ctx)
				calls++
				if mode == "credential" && calls > 2 {
					p.CredentialID = "changed-session"
				}
				return p, nil
			}
			if article, err := s.GetArticleForPrincipal(context.Background(), draft.ThreadID, proof); err == nil || article != nil {
				t.Fatalf("stale article=%v err=%v", article, err)
			}
		})
	}
}

type richReadObjects struct {
	corestorage.ObjectPort
	afterOpen func()
	closes    int
}
type richReadCloser struct {
	io.ReadCloser
	close func()
}

func (r richReadCloser) Close() error { r.close(); return r.ReadCloser.Close() }
func (r *richReadObjects) Open(ctx context.Context, owner, id string) (corestorage.ObjectReader, error) {
	object, err := r.ObjectPort.Open(ctx, owner, id)
	if object.Reader != nil {
		object.Reader = richReadCloser{ReadCloser: object.Reader, close: func() { r.closes++ }}
	}
	if r.afterOpen != nil {
		r.afterOpen()
	}
	return object, err
}

func TestRichTextAuthorizedAttachmentAndAssetCloseOnRevocation(t *testing.T) {
	for _, mode := range []string{"stable-owner", "stable-public-other", "session-revoked", "asset-quarantined", "object-deleted", "attachment-removed"} {
		t.Run(mode, func(t *testing.T) {
			s := newAttachmentTestService(t)
			draft := createAttachmentDraft(t, s)
			pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
			attachment, err := s.UploadAttachment(context.Background(), "1001", draft.ThreadID, "guide.pdf", "application/pdf", int64(len(pdf)), bytes.NewReader(pdf))
			if err != nil {
				t.Fatal(err)
			}
			owner := "1001"
			if mode == "stable-public-other" {
				if _, err := s.Publish(context.Background(), draft.ThreadID, "1001"); err != nil {
					t.Fatal(err)
				}
				owner = "1002"
			}
			revoked := false
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				if revoked {
					return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
				}
				return richReadProof(owner)(ctx)
			}
			objects := &richReadObjects{ObjectPort: s.objects}
			objects.afterOpen = func() {
				switch mode {
				case "session-revoked":
					revoked = true
				case "asset-quarantined":
					if err := s.AdminQuarantineAsset(context.Background(), "admin", attachment.AssetID, "test quarantine"); err != nil {
						t.Fatal(err)
					}
				case "object-deleted":
					if err := objects.ObjectPort.Delete(context.Background(), "1001", attachment.Asset.StorageObjectID, 1); err != nil {
						t.Fatal(err)
					}
				case "attachment-removed":
					if err := s.store.RemoveAttachment(context.Background(), draft.Article.ID, attachment.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			s.objects = objects
			opened, err := s.OpenAttachmentForPrincipal(context.Background(), draft.ThreadID, attachment.ID, proof)
			allowed := mode == "stable-owner" || mode == "stable-public-other"
			if (err == nil) != allowed || (!allowed && opened.Reader != nil) {
				t.Fatalf("result=%v err=%v", opened, err)
			}
			if allowed {
				raw, err := io.ReadAll(opened.Reader)
				if err != nil || !bytes.Equal(raw, pdf) {
					t.Fatal("wrong attachment bytes")
				}
				_ = opened.Reader.Close()
			}
			if objects.closes != 1 {
				t.Fatalf("reader closes=%d", objects.closes)
			}
			if mode == "stable-owner" {
				if _, err := s.OpenUserAssetForPrincipal(context.Background(), attachment.AssetID, richReadProof("1002")); err == nil {
					t.Fatal("private asset cross-owner read")
				}
			}
		})
	}
}

func TestRichTextAuthorizedAssetPageAndMissingProof(t *testing.T) {
	s := newAttachmentTestService(t)
	draft := createAttachmentDraft(t, s)
	if _, err := s.GetArticleForPrincipal(context.Background(), draft.ThreadID, nil); err == nil {
		t.Fatal("anonymous private article")
	}
	if _, err := s.UserAssetPageForPrincipal(context.Background(), "", "", 1, nil); !errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable) {
		t.Fatal(err)
	}
	page, err := s.UserAssetPageForPrincipal(context.Background(), "", "", 1, richReadProof("1001"))
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("empty page: %#v %v", page, err)
	}
	calls := 0
	proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
		calls++
		if calls > 1 {
			return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
		}
		return richReadProof("1001")(ctx)
	}
	if page, err := s.UserAssetPageForPrincipal(context.Background(), "", "", 1, proof); err == nil || page.Items != nil {
		t.Fatalf("revoked empty page: %#v %v", page, err)
	}
}
