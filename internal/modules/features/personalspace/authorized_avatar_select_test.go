package space

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func avatarSelectionFixture(t *testing.T, s *Service, owner string) (string, string) {
	t.Helper()
	store, err := NewLocalFileStore(FileStorageConfig{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s.SetFileStore(store)
	old, _, err := store.SaveAvatar(owner, "old.png", bytes.NewReader(tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := store.SaveAvatar(owner, "current.png", bytes.NewReader(tinyPNG))
	if err != nil {
		t.Fatal(err)
	}
	return old, current
}

func TestSpaceAvatarSelectCurrentDefaultExistingRequiredRollback(t *testing.T) {
	ctx := context.Background()
	for _, existing := range []bool{false, true} {
		for _, mode := range []string{"current", "audit", "outbox", "disabled", "missing", "invalid", "admin", "proof", "missing_audit", "cancel", "symlink"} {
			t.Run(map[bool]string{false: "default", true: "existing"}[existing]+"/"+mode, func(t *testing.T) {
				s, repo, events := spaceOwnerFixture(t, existing, mode)
				name, _ := avatarSelectionFixture(t, s, "1001")
				proof := identityport.SessionPrincipalResolver(spaceOwnerProof)
				commandCtx := ctx
				switch mode {
				case "disabled":
					if !existing {
						return
					}
					at := time.Now()
					if err := repo.SetDisabled(ctx, "1001", &at, "", "disabled"); err != nil {
						t.Fatal(err)
					}
				case "missing":
					name = "missing.png"
				case "invalid":
					name = "../avatar.png"
				case "admin":
					proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
						p, e := spaceOwnerProof(ctx)
						p.Actor.Kind, p.Audience = "admin", "admin"
						return p, e
					}
				case "proof":
					proof = nil
				case "missing_audit":
					s.reliable = nil
				case "cancel":
					var cancel context.CancelFunc
					commandCtx, cancel = context.WithCancel(ctx)
					cancel()
				case "symlink":
					path, _ := s.fileStore.AvatarPath("1001", name)
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(filepath.Join(t.TempDir(), "outside.png"), path); err != nil {
						t.Fatal(err)
					}
				}
				before, beforeEvents := repo.Snapshot(), events.Snapshot()
				files, err := s.fileStore.avatarFiles("1001")
				if err != nil {
					t.Fatal(err)
				}
				ack, err := s.SelectAvatarForPrincipal(commandCtx, name, proof)
				if mode != "current" {
					if err == nil || ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
						t.Fatal("denial/required rollback failed", ack, err)
					}
				} else {
					if err != nil || ack == nil || ack.OwnerID != "1001" {
						t.Fatal(ack, err)
					}
					space, err := repo.GetByUserID(ctx, "1001")
					if err != nil || space.Avatar != s.fileStore.AvatarURL("1001", name) {
						t.Fatal(space, err)
					}
					raw, _ := json.Marshal(ack)
					var fields map[string]json.RawMessage
					if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 {
						t.Fatal("unsafe ack", string(raw))
					}
				}
				after, err := s.fileStore.avatarFiles("1001")
				if err != nil || !reflect.DeepEqual(files, after) {
					t.Fatal("selection changed files", err)
				}
			})
		}
	}
}

func mutateSelectedAvatar(t *testing.T, s *Service, owner, name, mode string) {
	t.Helper()
	path, err := s.fileStore.AvatarPath(owner, name)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	switch mode {
	case "bytes":
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data[len(data)-1] ^= 1
		if err := os.WriteFile(path, data, info.Mode()); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
	case "inode":
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		temp := path + ".replacement"
		if err := os.WriteFile(temp, data, info.Mode()); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(temp, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(temp, path); err != nil {
			t.Fatal(err)
		}
	case "inventory":
		if err := os.WriteFile(filepath.Join(filepath.Dir(path), "extra.png"), tinyPNG, 0600); err != nil {
			t.Fatal(err)
		}
	case "removed":
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	case "config":
		s.fileStore.cfg.AvatarKeepLimit++
	case "store":
		copy := *s.fileStore
		s.fileStore = &copy
	default:
		t.Fatal("unknown mutation", mode)
	}
}

func TestSpaceAvatarSelectActualPostWriteFileAndPrincipalChangesRollback(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"bytes", "inode", "inventory", "removed", "config", "store", "credential", "extra_space"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, events := spaceOwnerFixture(t, true, "")
			name, _ := avatarSelectionFixture(t, s, "1001")
			before, beforeEvents := repo.Snapshot(), events.Snapshot()
			ran := false
			s.repo = &spaceOwnerRepoHook{MemoryRepository: repo, after: func(tx context.Context, space *Space) {
				ran = true
				if mode == "extra_space" {
					current, _ := repo.GetByUserID(tx, "1001")
					current.Bio = "extra"
					if err := repo.Upsert(tx, current); err != nil {
						t.Fatal(err)
					}
				} else if mode != "credential" {
					mutateSelectedAvatar(t, s, "1001", name, mode)
				}
			}}
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := spaceOwnerProof(ctx)
				if ran && mode == "credential" {
					p.CredentialID = "replaced-session"
				}
				return p, e
			}
			ack, err := s.SelectAvatarForPrincipal(ctx, name, proof)
			if !ran || err == nil || ack != nil || !reflect.DeepEqual(before, repo.Snapshot()) || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal("actual changed facts escaped", mode, ack, err, ran)
			}
		})
	}
}

func TestSpaceAvatarSelectHeldPreflightAndExactFileReceipt(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"bytes", "inode", "inventory", "credential", "space_aba"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, events := spaceOwnerFixture(t, true, "")
			name, _ := avatarSelectionFixture(t, s, "1001")
			calls, ran := 0, false
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := spaceOwnerProof(ctx)
				calls++
				if calls == 2 {
					ran = true
					switch mode {
					case "space_aba":
						current, _ := repo.GetByUserID(ctx, "1001")
						old := current.Bio
						current.Bio = "ABA"
						if err := repo.Upsert(ctx, current); err != nil {
							t.Fatal(err)
						}
						current.Bio = old
						if err := repo.Upsert(ctx, current); err != nil {
							t.Fatal(err)
						}
					case "credential":
						p.CredentialID = "changed"
					default:
						mutateSelectedAvatar(t, s, "1001", name, mode)
					}
				}
				return p, e
			}
			beforeEvents := events.Snapshot()
			ack, err := s.SelectAvatarForPrincipal(ctx, name, proof)
			if !ran || err == nil || ack != nil || !reflect.DeepEqual(beforeEvents, events.Snapshot()) {
				t.Fatal("held preflight accepted", mode, ack, err, ran)
			}
			if mode != "space_aba" && !errors.Is(err, identityport.ErrPolicyFactsChanged) {
				t.Fatal("wrong conflict", err)
			}
		})
	}
}
