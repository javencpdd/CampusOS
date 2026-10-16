package space

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	identityrepo "github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

func filesProof(owner string) identityport.SessionPrincipalResolver {
	return func(context.Context) (identityport.PrincipalContext, error) {
		return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: owner}, Audience: "user", AuthenticationStrength: "password", CredentialID: "files-session"}, nil
	}
}
func fileFixture(t *testing.T, repo Repository, users UserLookup, owner string) (*Service, string, string) {
	t.Helper()
	s := NewService(repo, users)
	adapter, err := corestorage.NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewLocalFileStoreWithStorage(FileStorageConfig{}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFileStore(store)
	s.SetQuotaManager(adapter)
	old, _, err := store.SaveAvatar(owner, "old.png", bytes.NewReader(tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := store.SaveAvatar(owner, "current.png", bytes.NewReader(tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	return s, current, old
}
func memoryFileFixture(t *testing.T) (*Service, *MemoryRepository, *identityrepo.MemoryUserRepository, string, string) {
	t.Helper()
	users := identityrepo.NewMemoryUserRepository()
	owner := &identitydomain.User{ID: "1001", Username: "files-owner", Email: "files-owner@example.test", Status: identitydomain.UserStatusActive, AuthVersion: 1}
	if err := users.Create(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	repo := NewMemoryRepository()
	s, current, old := fileFixture(t, repo, users, "1001")
	space := defaultSpace(owner)
	space.ID = "100101"
	space.Avatar = s.fileStore.AvatarURL(owner.ID, current)
	if err := repo.Upsert(context.Background(), space); err != nil {
		t.Fatal(err)
	}
	return s, repo, users, current, old
}

func TestSpaceFilesCurrentAvatarAndHistoryMatrix(t *testing.T) {
	ctx := context.Background()
	s, repo, users, current, old := memoryFileFixture(t)
	for _, binding := range []string{"user-profile", "space-profile", "history"} {
		for _, visibility := range []Visibility{VisibilityPublic, VisibilityUnlisted, VisibilityPrivate} {
			for _, disabled := range []bool{false, true} {
				for _, status := range []identitydomain.UserStatus{"active", "suspended", "deactivated"} {
					t.Run(binding+"/"+string(visibility)+"/"+string(status)+"/"+map[bool]string{true: "disabled", false: "enabled"}[disabled], func(t *testing.T) {
						user, _ := users.GetByID(ctx, "1001")
						user.Avatar = ""
						user.Status = status
						if binding == "user-profile" {
							user.Avatar = s.fileStore.AvatarURL("1001", current)
						}
						if err := users.Update(ctx, user); err != nil {
							t.Fatal(err)
						}
						space, _ := repo.GetByUserID(ctx, "1001")
						space.Visibility = visibility
						space.DisabledAt = nil
						if disabled {
							now := time.Now()
							space.DisabledAt = &now
						}
						if err := repo.Upsert(ctx, space); err != nil {
							t.Fatal(err)
						}
						name := current
						if binding == "history" {
							name = old
						}
						public := status == "active" && (binding == "user-profile" || (binding == "space-profile" && visibility == VisibilityPublic && !disabled))
						admin := func(ctx context.Context) (identityport.PrincipalContext, error) {
							p, _ := filesProof("1001")(ctx)
							p.Actor.Kind, p.Audience, p.AuthenticationStrength = "admin", "admin", "mfa"
							return p, nil
						}
						for _, actor := range []struct {
							name    string
							resolve identityport.SessionPrincipalResolver
							allow   bool
						}{{"public", nil, public}, {"owner", filesProof("1001"), status == "active"}, {"other", filesProof("1002"), public}, {"same-ID-admin", admin, false}} {
							image, err := s.ReadAvatarForPrincipal(ctx, "1001", name, actor.resolve)
							if (err == nil) != actor.allow || (err != nil && len(image.Data) != 0) {
								t.Fatalf("%s bytes=%d err=%v", actor.name, len(image.Data), err)
							}
						}
					})
				}
			}
		}
	}
}

type filesRepoHook struct {
	Repository
	gets  int
	after func(int)
}

func (r *filesRepoHook) GetByUserID(ctx context.Context, id string) (*Space, error) {
	row, err := r.Repository.GetByUserID(ctx, id)
	r.gets++
	if r.after != nil {
		r.after(r.gets)
	}
	return row, err
}
func TestSpaceFilesRevocationAndWholeInventory(t *testing.T) {
	for _, mode := range []string{"avatar", "visibility", "disabled", "user-status", "bytes", "session", "credential"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, repo, users, current, _ := memoryFileFixture(t)
			hook := &filesRepoHook{Repository: repo}
			s.repo = hook
			revoked := false
			hook.after = func(n int) {
				if (mode == "bytes" && n != 2) || (mode != "bytes" && n != 1) {
					return
				}
				switch mode {
				case "session", "credential":
					revoked = true
				case "user-status":
					u, _ := users.GetByID(ctx, "1001")
					u.Status = "suspended"
					users.Update(ctx, u)
				case "bytes":
					path, _ := s.fileStore.AvatarPath("1001", current)
					data := append([]byte{}, tinyPNG...)
					data[len(data)-1] ^= 1
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
				default:
					space, _ := repo.GetByUserID(ctx, "1001")
					switch mode {
					case "avatar":
						space.Avatar = ""
					case "visibility":
						space.Visibility = VisibilityPrivate
					case "disabled":
						now := time.Now()
						space.DisabledAt = &now
					}
					repo.Upsert(ctx, space)
				}
			}
			var resolve identityport.SessionPrincipalResolver
			if mode == "session" || mode == "credential" {
				resolve = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, _ := filesProof("1001")(ctx)
					if revoked {
						if mode == "session" {
							return p, identityport.ErrUserSessionPrincipalUnavailable
						}
						p.CredentialID = "other-files-session"
					}
					return p, nil
				}
			}
			image, err := s.ReadAvatarForPrincipal(ctx, "1001", current, resolve)
			if err == nil || len(image.Data) != 0 {
				t.Fatalf("stale image %d %v", len(image.Data), err)
			}
		})
	}
	for _, mode := range []string{"history", "quota", "credential"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, repo, _, _, _ := memoryFileFixture(t)
			hook := &filesRepoHook{Repository: repo}
			s.repo = hook
			changed := false
			// One complete history load calls the owning profile twice.
			threshold := 3
			if mode == "quota" {
				threshold = 2
			}
			hook.after = func(n int) {
				if n != threshold {
					return
				}
				changed = true
				if mode == "history" {
					if _, _, err := s.fileStore.SaveAvatar("1001", "third.png", bytes.NewReader(tinyPNG)); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "quota" {
					if _, err := s.quotaManager.SetQuota(ctx, "1001", 1024*1024, "test"); err != nil {
						t.Fatal(err)
					}
				}
			}
			resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, _ := filesProof("1001")(ctx)
				if changed && mode == "credential" {
					p.CredentialID = "changed"
				}
				return p, nil
			}
			if mode == "quota" {
				status, err := s.StorageStatusForPrincipal(ctx, resolve)
				if err == nil || status != nil {
					t.Fatal("stale quota", status, err)
				}
			} else {
				history, err := s.ListAvatarsForPrincipal(ctx, resolve)
				if err == nil || history != nil {
					t.Fatal("stale history", history, err)
				}
			}
		})
	}
	s, _, _, _, _ := memoryFileFixture(t)
	if _, err := s.ListAvatarsForPrincipal(context.Background(), nil); !errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable) {
		t.Fatal(err)
	}
	if _, err := s.StorageStatusForPrincipal(context.Background(), nil); !errors.Is(err, identityport.ErrUserSessionPrincipalUnavailable) {
		t.Fatal(err)
	}
	// The real cross-module profile adapter must retain its authoritative status.
	if identityUser(identityport.User{ID: "1001", Status: "active"}).Status != "active" {
		t.Fatal("module adapter dropped profile status")
	}
}

func TestSpaceFilesCurrentQuotaFromRepository(t *testing.T) {
	ctx := context.Background()
	s, _, _, _, _ := memoryFileFixture(t)
	records := corestorage.NewMemoryQuotaRepository()
	adapter, err := corestorage.NewLocalAdapterWithQuotaRepository(s.fileStore.cfg.RootDir, corestorage.DefaultQuotaBytes, records)
	if err != nil {
		t.Fatal(err)
	}
	s.SetQuotaManager(adapter)
	if _, err := records.Upsert(ctx, corestorage.QuotaRecord{UserID: "1001", QuotaBytes: 1024 * 1024, UpdatedBy: "1001"}); err != nil {
		t.Fatal(err)
	}
	if adapter.QuotaBytes("1001") != corestorage.DefaultQuotaBytes {
		t.Fatal("fixture must retain stale startup cache")
	}
	status, err := s.StorageStatusForPrincipal(ctx, filesProof("1001"))
	if err != nil || status.QuotaBytes != 1024*1024 || !status.CustomQuota || status.QuotaUpdatedBy != "1001" {
		t.Fatalf("current quota %v %v", status, err)
	}
	history, err := s.ListAvatarsForPrincipal(ctx, filesProof("1001"))
	if err != nil || history.Storage.QuotaBytes != 1024*1024 {
		t.Fatalf("current history quota %v %v", history, err)
	}
	s.quotaManager = nil
	if _, err := s.StorageStatusForPrincipal(ctx, filesProof("1001")); err == nil {
		t.Fatal("missing fresh quota port")
	}
}
