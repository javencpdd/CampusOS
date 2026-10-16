package space

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

func avatarUploadFixture(t *testing.T, existing bool, mode string) (*Service, *MemoryRepository, *corestorage.ObjectService, *corestorage.MemoryQuotaRepository) {
	t.Helper()
	s, repo, _ := spaceOwnerFixture(t, existing, mode)
	quota := corestorage.NewMemoryQuotaRepository()
	adapter, err := corestorage.NewLocalAdapterWithQuotaRepository(t.TempDir(), 1024*1024, quota)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewLocalFileStoreWithStorage(FileStorageConfig{AvatarKeepLimit: 3}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFileStore(store)
	s.SetQuotaManager(adapter)
	for i := 0; i < 2; i++ {
		if _, _, err := store.SaveAvatar("1001", "old.png", bytes.NewReader(tinyPNG)); err != nil {
			t.Fatal(err)
		}
	}
	objects, err := corestorage.NewObjectService(adapter, adapter, corestorage.NewMemoryObjectRepository(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAvatarStorage(adapter, objects)
	return s, repo, objects, quota
}
func avatarUploadFileBytes(t *testing.T, s *Service, owner string) map[string][]byte {
	t.Helper()
	files, err := s.fileStore.avatarFiles(owner)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string][]byte{}
	for _, file := range files {
		path, err := s.fileStore.AvatarPath(owner, file.name)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result[file.name] = data
	}
	return result
}
func TestSpaceAvatarUploadActualBytesQuotaDefaultExistingAndRollback(t *testing.T) {
	ctx := context.Background()
	for _, existing := range []bool{false, true} {
		for _, mode := range []string{"current", "retention_pending", "audit", "outbox", "quota", "oversized", "unsupported", "disabled", "admin", "missing_account", "custom_root"} {
			t.Run(map[bool]string{false: "default", true: "existing"}[existing]+"/"+mode, func(t *testing.T) {
				s, repo, objects, quota := avatarUploadFixture(t, existing, mode)
				proof := identityport.SessionPrincipalResolver(spaceOwnerProof)
				reader := io.Reader(bytes.NewReader(tinyPNG))
				switch mode {
				case "retention_pending":
					s.fileStore.cfg.AvatarKeepLimit = 2
				case "quota":
					if _, err := quota.Upsert(ctx, corestorage.QuotaRecord{UserID: "1001", QuotaBytes: 1}); err != nil {
						t.Fatal(err)
					}
				case "oversized":
					s.fileStore.cfg.MaxAvatarBytes = 1
				case "unsupported":
					reader = bytes.NewReader([]byte("not an image"))
				case "disabled":
					if !existing {
						return
					}
					at := time.Now()
					if err := repo.SetDisabled(ctx, "1001", &at, "", "disabled"); err != nil {
						t.Fatal(err)
					}
				case "admin":
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, e := spaceOwnerProof(ctx)
						p.Actor.Kind, p.Audience = "admin", "admin"
						return p, e
					}
				case "missing_account":
					s.avatarAccount = nil
				case "custom_root":
					s.fileStore.cfg.RootDir = t.TempDir()
				}
				before := repo.Snapshot()
				files := avatarUploadFileBytes(t, s, "1001")
				usage, err := objects.Usage(ctx, "1001")
				if err != nil {
					t.Fatal(err)
				}
				ack, err := s.UploadAvatarForPrincipal(ctx, reader, proof)
				current := avatarUploadFileBytes(t, s, "1001")
				after, e := objects.Usage(ctx, "1001")
				if e != nil {
					t.Fatal(e)
				}
				if mode != "current" && mode != "retention_pending" {
					if err == nil || ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(files, current) || usage.UsedBytes != after.UsedBytes {
						t.Fatal("upload denial/rollback failed", ack, err, usage, after)
					}
				} else {
					if err != nil || ack == nil || ack.OwnerID != "1001" || ack.CleanupPending != (mode == "retention_pending") || len(current) != len(files)+1 {
						t.Fatal(ack, err, len(files), len(current))
					}
					var newSize int64
					for name, data := range current {
						if previous, ok := files[name]; ok {
							if !bytes.Equal(previous, data) {
								t.Fatal("old avatar overwritten")
							}
						} else {
							newSize += int64(len(data))
						}
					}
					if after.UsedBytes-usage.UsedBytes != newSize {
						t.Fatal("ledger did not allocate full new bytes", usage, after, newSize)
					}
					space, e := repo.GetByUserID(ctx, "1001")
					if e != nil || space.Avatar == "" {
						t.Fatal(space, e)
					}
				}
			})
		}
	}
}

func TestSpaceAvatarUploadPostWriteCurrentChangesExactCleanupAndPending(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"extra_space", "credential", "config", "cancel", "replacement_pending"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, objects, _ := avatarUploadFixture(t, true, "")
			files := avatarUploadFileBytes(t, s, "1001")
			before := repo.Snapshot()
			usage, err := objects.Usage(ctx, "1001")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			ran := false
			newName := ""
			s.repo = &spaceOwnerRepoHook{MemoryRepository: repo, after: func(tx context.Context, space *Space) {
				ran = true
				current, e := s.fileStore.avatarFiles("1001")
				if e != nil {
					t.Fatal(e)
				}
				for _, f := range current {
					if _, ok := files[f.name]; !ok {
						newName = f.name
					}
				}
				if newName == "" {
					t.Fatal("actual file not written")
				}
				switch mode {
				case "extra_space":
					space.Bio = "extra"
					if err := repo.Upsert(tx, space); err != nil {
						t.Fatal(err)
					}
				case "config":
					s.fileStore.cfg.MaxAvatarBytes++
				case "cancel":
					cancel()
				case "replacement_pending":
					mutateSelectedAvatar(t, s, "1001", newName, "inode")
				}
			}}
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := spaceOwnerProof(ctx)
				if ran && mode == "credential" {
					p.CredentialID = "revoked"
				}
				return p, e
			}
			ack, err := s.UploadAvatarForPrincipal(ctx, bytes.NewReader(tinyPNG), proof)
			if !ran || err == nil || ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) {
				t.Fatal("actual changed upload accepted", ack, err, ran)
			}
			current := avatarUploadFileBytes(t, s, "1001")
			for name, data := range files {
				if !bytes.Equal(current[name], data) {
					t.Fatal("failed upload pruned old bytes", name)
				}
			}
			if mode == "replacement_pending" {
				if !errors.Is(err, corestorage.ErrInlineImageCleanupPending) || current[newName] == nil {
					t.Fatal("changed inode incorrectly discarded", err)
				}
			} else if len(current) != len(files) {
				t.Fatal("exact new upload not discarded", mode)
			}
			after, e := objects.Usage(context.Background(), "1001")
			if e != nil || after.UsedBytes != usage.UsedBytes {
				t.Fatal("Memory delta not undone", usage, after, e)
			}
		})
	}
}

type avatarUploadReader struct {
	io.Reader
	after func()
	calls int
}

func (r *avatarUploadReader) Read(p []byte) (int, error) {
	r.calls++
	n, e := r.Reader.Read(p)
	if r.after != nil {
		after := r.after
		r.after = nil
		after()
	}
	return n, e
}
func TestSpaceAvatarUploadInitialAuthorityBeforeInputAndHeldPreflight(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"admin", "disabled", "credential", "space_aba"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, _, _ := avatarUploadFixture(t, true, "")
			reader := &avatarUploadReader{Reader: bytes.NewReader(tinyPNG)}
			proof := identityport.SessionPrincipalResolver(spaceOwnerProof)
			if mode == "admin" {
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := spaceOwnerProof(ctx)
					p.Actor.Kind, p.Audience = "admin", "admin"
					return p, e
				}
			}
			if mode == "disabled" {
				at := time.Now()
				if err := repo.SetDisabled(ctx, "1001", &at, "", "disabled"); err != nil {
					t.Fatal(err)
				}
			}
			changed := false
			reader.after = func() {
				changed = true
				if mode == "space_aba" {
					space, _ := repo.GetByUserID(ctx, "1001")
					old := space.Bio
					space.Bio = "ABA"
					if err := repo.Upsert(ctx, space); err != nil {
						t.Fatal(err)
					}
					space.Bio = old
					if err := repo.Upsert(ctx, space); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "credential" {
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := spaceOwnerProof(ctx)
					if changed {
						p.CredentialID = "changed"
					}
					return p, e
				}
			}
			files := avatarUploadFileBytes(t, s, "1001")
			ack, err := s.UploadAvatarForPrincipal(ctx, reader, proof)
			if err == nil || ack != nil || !reflect.DeepEqual(files, avatarUploadFileBytes(t, s, "1001")) {
				t.Fatal(ack, err)
			}
			if (mode == "admin" || mode == "disabled") && reader.calls != 0 {
				t.Fatal("unauthorized input read", reader.calls)
			}
			if (mode == "credential" || mode == "space_aba") && !changed {
				t.Fatal("held proof mutation did not execute")
			}
		})
	}
}
