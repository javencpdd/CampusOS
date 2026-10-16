package secondhand

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"testing"
)

func typedReadProof(owner string) identityport.SessionPrincipalResolver {
	return func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: owner}, Audience: "user", AuthenticationStrength: "password", CredentialID: "typed-feature-session"}, nil
	}
}
func TestSecondhandAuthorizedStateMatrix(t *testing.T) {
	ctx := context.Background()
	for _, pub := range []domain.PublicationStatus{domain.PublicationStatusPublished, domain.PublicationStatusPrivate, domain.PublicationStatusDraft} {
		for _, mod := range []domain.ModerationStatus{domain.ModerationStatusClear, domain.ModerationStatusPending, domain.ModerationStatusRejected, domain.ModerationStatusTakenDown} {
			for _, del := range []domain.DeletionStatus{domain.DeletionStatusActive, domain.DeletionStatusTrashed, domain.DeletionStatusPurged} {
				t.Run(string(pub)+"/"+string(mod)+"/"+string(del), func(t *testing.T) {
					s, repo, _ := newTestService(t, true)
					created, err := s.Create(ctx, "1001", "Owner", validCreateRequest())
					if err != nil {
						t.Fatal(err)
					}
					thread, _ := repo.GetByID(ctx, created.Thread.ID)
					thread.PublicationStatus, thread.ModerationStatus, thread.DeletionStatus = pub, mod, del
					if err := repo.Update(ctx, thread); err != nil {
						t.Fatal(err)
					}
					result, err := s.GetForPrincipal(ctx, thread.ID, typedReadProof("1001"))
					if (err == nil) != (del == domain.DeletionStatusActive) || (err != nil && result != nil) {
						t.Fatalf("owner %v %v", result, err)
					}
					result, err = s.GetForPrincipal(ctx, thread.ID, nil)
					public := pub == domain.PublicationStatusPublished && mod == domain.ModerationStatusClear && del == domain.DeletionStatusActive
					if (err == nil) != public || (err != nil && result != nil) {
						t.Fatalf("public %v %v", result, err)
					}
					if result, err := s.GetForPrincipal(ctx, thread.ID, typedReadProof("1002")); err == nil || result != nil {
						t.Fatal("other owner accepted by /me")
					}
					stored, _ := repo.GetByID(ctx, thread.ID)
					if stored.ViewCount != 0 {
						t.Fatal("Feature inflated view")
					}
				})
			}
		}
	}
}

type typedReadStore struct {
	Store
	gets      int
	many      int
	afterGet  func(int)
	afterMany func(int)
}

func (r *typedReadStore) Get(ctx context.Context, id string) (*Detail, error) {
	d, err := r.Store.Get(ctx, id)
	r.gets++
	if r.afterGet != nil {
		r.afterGet(r.gets)
	}
	return d, err
}
func (r *typedReadStore) GetMany(ctx context.Context, ids []string) (map[string]*Detail, error) {
	d, err := r.Store.GetMany(ctx, ids)
	r.many++
	if r.afterMany != nil {
		r.afterMany(r.many)
	}
	return d, err
}
func TestSecondhandAuthorizedDetailRevocation(t *testing.T) {
	for _, mode := range []string{"detail", "owner", "trash", "session", "credential"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, repo, _ := newTestService(t, true)
			created, err := s.Create(ctx, "1001", "Owner", validCreateRequest())
			if err != nil {
				t.Fatal(err)
			}
			base := s.store
			store := &typedReadStore{Store: base}
			store.afterGet = func(n int) {
				if n != 1 {
					return
				}
				switch mode {
				case "detail":
					d, _ := base.Get(ctx, created.Thread.ID)
					d.LocationScope = "changed-location"
					if err := base.Update(ctx, d, d.Version); err != nil {
						t.Fatal(err)
					}
				case "owner", "trash":
					thread, _ := repo.GetByID(ctx, created.Thread.ID)
					if mode == "owner" {
						thread.AuthorID = "1002"
					} else {
						thread.DeletionStatus = domain.DeletionStatusTrashed
					}
					if err := repo.Update(ctx, thread); err != nil {
						t.Fatal(err)
					}
				}
			}
			s.store = store
			calls := 0
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, _ := typedReadProof("1001")(ctx)
				calls++
				if calls > 2 {
					if mode == "session" {
						return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
					}
					if mode == "credential" {
						p.CredentialID = "replacement"
					}
				}
				return p, nil
			}
			if result, err := s.GetForPrincipal(ctx, created.Thread.ID, proof); err == nil || result != nil {
				t.Fatalf("stale projection %v %v", result, err)
			}
		})
	}
}
func TestSecondhandAuthorizedWholeListAndEmptyPage(t *testing.T) {
	for _, mode := range []string{"detail", "membership", "empty-membership"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, repo, _ := newTestService(t, true)
			created, err := s.Create(ctx, "1001", "Owner", validCreateRequest())
			if err != nil {
				t.Fatal(err)
			}
			if mode == "empty-membership" {
				thread, _ := repo.GetByID(ctx, created.Thread.ID)
				thread.PublicationStatus = domain.PublicationStatusPrivate
				if err := repo.Update(ctx, thread); err != nil {
					t.Fatal(err)
				}
			}
			base := s.store
			store := &typedReadStore{Store: base}
			store.afterMany = func(n int) {
				if n != 1 {
					return
				}
				if mode == "detail" {
					d, _ := base.Get(ctx, created.Thread.ID)
					d.LocationScope = "changed"
					if err := base.Update(ctx, d, d.Version); err != nil {
						t.Fatal(err)
					}
				} else {
					thread, _ := repo.GetByID(ctx, created.Thread.ID)
					if mode == "membership" {
						thread.PublicationStatus = domain.PublicationStatusPrivate
					} else {
						thread.PublicationStatus = domain.PublicationStatusPublished
					}
					if err := repo.Update(ctx, thread); err != nil {
						t.Fatal(err)
					}
				}
			}
			s.store = store
			if rows, total, err := s.ListPublicChecked(ctx, domain.ThreadListFilter{Page: 1, PageSize: 1}); err == nil || rows != nil || total != 0 {
				t.Fatalf("mixed list %v %d %v", rows, total, err)
			}
		})
	}
}
func TestSecondhandAuthorizedMissingPortsAndDomain(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newTestService(t, true)
	created, err := s.Create(ctx, "1001", "Owner", validCreateRequest())
	if err != nil {
		t.Fatal(err)
	}
	admin := func(ctx context.Context) (identityport.PrincipalContext, error) {
		p, _ := typedReadProof("1001")(ctx)
		p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
		return p, nil
	}
	if _, err := s.GetForPrincipal(ctx, created.Thread.ID, admin); !errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable) {
		t.Fatal(err)
	}
	s.query = nil
	if result, err := s.GetForPrincipal(ctx, created.Thread.ID, typedReadProof("1001")); err == nil || result != nil {
		t.Fatal("missing authenticated query fallback")
	}
}
