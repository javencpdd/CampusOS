package space

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type avatarRetentionStoreHook struct {
	*reliability.MemoryStore
	failPhase, failAt string
	afterPhase        string
	after             func(context.Context)
	afterList         func()
}

func (s *avatarRetentionStoreHook) ListSubjectOperations(ctx context.Context, kind, subject, owner string, limit int) ([]reliability.Operation, error) {
	items, err := s.MemoryStore.ListSubjectOperations(ctx, kind, subject, owner, limit)
	if s.afterList != nil {
		after := s.afterList
		s.afterList = nil
		after()
	}
	return items, err
}

func (s *avatarRetentionStoreHook) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	phase := strings.TrimPrefix(a.CommandCode, avatarRetentionKind+".")
	if phase == s.failPhase && s.failAt == "audit" {
		return errors.New("required avatar cleanup audit rejected")
	}
	if err := s.MemoryStore.RecordCommandAudit(ctx, a); err != nil {
		return err
	}
	if phase == s.afterPhase && s.after != nil {
		after := s.after
		s.after = nil
		after(ctx)
	}
	return nil
}
func (s *avatarRetentionStoreHook) Enqueue(ctx context.Context, e *reliability.Event) (*reliability.Event, error) {
	phase := strings.TrimPrefix(e.Type, "personal_space.avatar_retention.")
	if phase == s.failPhase && s.failAt == "outbox" {
		return nil, errors.New("required avatar cleanup outbox rejected")
	}
	return s.MemoryStore.Enqueue(ctx, e)
}
func avatarRetentionFixture(t *testing.T, protectOld bool) (*Service, *MemoryRepository, *corestorage.ObjectService, *avatarRetentionStoreHook, []avatarFile) {
	t.Helper()
	ctx := context.Background()
	s, repo, objects, _ := avatarUploadFixture(t, true, "")
	s.fileStore.cfg.AvatarKeepLimit = 10
	for i := 0; i < 3; i++ {
		if _, _, err := s.fileStore.SaveAvatar("1001", "old.png", bytes.NewReader(tinyPNG)); err != nil {
			t.Fatal(err)
		}
	}
	files, err := s.fileStore.avatarFiles("1001")
	if err != nil || len(files) != 5 {
		t.Fatal(files, err)
	}
	space, err := repo.GetByUserID(ctx, "1001")
	if err != nil {
		t.Fatal(err)
	}
	space.Avatar = s.fileStore.AvatarURL("1001", files[0].name)
	if err := repo.Upsert(ctx, space); err != nil {
		t.Fatal(err)
	}
	if protectOld {
		s.users.(*fakeUserLookup).byID["1001"].Avatar = s.fileStore.AvatarURL("1001", files[4].name)
	}
	s.fileStore.cfg.AvatarKeepLimit = 3
	store := &avatarRetentionStoreHook{MemoryStore: reliability.NewMemoryStore()}
	s.SetReliability(reliability.NewService(transaction.NewMemory(repo), store))
	if _, err := objects.Usage(ctx, "1001"); err != nil {
		t.Fatal(err)
	}
	return s, repo, objects, store, files
}

func TestSpaceAvatarRetentionCurrentProtectionAndOneCapacityRelease(t *testing.T) {
	ctx := context.Background()
	for _, protected := range []bool{false, true} {
		t.Run(map[bool]string{false: "latest", true: "protect_old_user_avatar"}[protected], func(t *testing.T) {
			s, repo, objects, _, files := avatarRetentionFixture(t, protected)
			before := repo.Snapshot()
			usage, err := objects.Usage(ctx, "1001")
			if err != nil {
				t.Fatal(err)
			}
			ack, err := s.CleanupAvatarHistoryForPrincipal(ctx, spaceOwnerProof)
			if err != nil || ack == nil || ack.CleanupPending {
				t.Fatal(ack, err)
			}
			current := avatarUploadFileBytes(t, s, "1001")
			want := 3
			if protected {
				want = 4
			}
			if len(current) != want || current[files[0].name] == nil || protected && current[files[4].name] == nil || !reflect.DeepEqual(before, repo.Snapshot()) {
				t.Fatal("active/Space metadata not preserved", len(current), want)
			}
			after, err := objects.Usage(ctx, "1001")
			if err != nil {
				t.Fatal(err)
			}
			var released int64
			for _, file := range files {
				if current[file.name] == nil {
					released += file.size
				}
			}
			if usage.UsedBytes-after.UsedBytes != released {
				t.Fatal("wrong released bytes", usage, after, released)
			}
			for i := 0; i < 2; i++ {
				again, err := s.CleanupAvatarHistoryForPrincipal(ctx, spaceOwnerProof)
				if err != nil || again.CleanupPending {
					t.Fatal(again, err)
				}
			}
			final, err := objects.Usage(ctx, "1001")
			if err != nil || final.UsedBytes != after.UsedBytes {
				t.Fatal("capacity released twice", after, final, err)
			}
		})
	}
}

func TestSpaceAvatarRetentionRequiredFailuresPersistPendingAndRestartRetry(t *testing.T) {
	ctx := context.Background()
	for _, phase := range []string{"prepare", "bytes", "finalize"} {
		for _, failure := range []string{"audit", "outbox"} {
			t.Run(phase+"/"+failure, func(t *testing.T) {
				s, repo, objects, store, files := avatarRetentionFixture(t, false)
				store.failPhase, store.failAt = phase, failure
				usage, err := objects.Usage(ctx, "1001")
				if err != nil {
					t.Fatal(err)
				}
				ack, err := s.CleanupAvatarHistoryForPrincipal(ctx, spaceOwnerProof)
				if err != nil || ack == nil || !ack.CleanupPending {
					t.Fatal("required failure not pending", ack, err)
				}
				current := avatarUploadFileBytes(t, s, "1001")
				if current[files[0].name] == nil || phase == "prepare" && len(current) != 5 || phase != "prepare" && len(current) != 3 {
					t.Fatal("wrong failure byte state", phase, len(current))
				}
				after, err := objects.Usage(ctx, "1001")
				if err != nil || after.UsedBytes != usage.UsedBytes {
					t.Fatal("failed prepare/final released capacity", usage, after, err)
				}
				store.failPhase, store.failAt = "", ""
				restarted := NewService(repo, s.users)
				restarted.SetFileStore(s.fileStore)
				restarted.SetAvatarStorage(s.avatarStorage, s.avatarAccount)
				restarted.SetReliability(reliability.NewService(transaction.NewMemory(repo), store))
				ack, err = restarted.CleanupAvatarHistoryForPrincipal(ctx, spaceOwnerProof)
				if err != nil || ack.CleanupPending {
					t.Fatal("pending restart retry failed", ack, err)
				}
				final, err := objects.Usage(ctx, "1001")
				if err != nil {
					t.Fatal(err)
				}
				var actual int64
				for _, data := range avatarUploadFileBytes(t, s, "1001") {
					actual += int64(len(data))
				}
				if final.UsedBytes != actual || len(avatarUploadFileBytes(t, s, "1001")) != 3 {
					t.Fatal("retry capacity not exact", final, actual)
				}
			})
		}
	}
}

func TestSpaceAvatarRetentionExactInodeCurrentProofAndCancelledFinalize(t *testing.T) {
	for _, mode := range []string{"inode", "credential", "cancel_after_bytes"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, _, objects, store, files := avatarRetentionFixture(t, false)
			ran := false
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := spaceOwnerProof(ctx)
				if ran && mode == "credential" {
					p.CredentialID = "changed"
				}
				return p, e
			}
			store.afterPhase = "prepare"
			if mode == "cancel_after_bytes" {
				store.afterPhase = "bytes"
			}
			store.after = func(context.Context) {
				ran = true
				switch mode {
				case "inode":
					mutateSelectedAvatar(t, s, "1001", files[3].name, "inode")
				case "cancel_after_bytes":
					cancel()
				}
			}
			usage, err := objects.Usage(context.Background(), "1001")
			if err != nil {
				t.Fatal(err)
			}
			ack, err := s.CleanupAvatarHistoryForPrincipal(ctx, proof)
			if !ran || ack == nil {
				t.Fatal("actual phase hook did not execute", ack, err, ran)
			}
			current := avatarUploadFileBytes(t, s, "1001")
			if mode == "inode" {
				if !ack.CleanupPending || current[files[3].name] == nil {
					t.Fatal("replacement inode deleted", ack, err)
				}
			}
			if mode == "credential" {
				if !ack.CleanupPending || len(current) != 5 {
					t.Fatal("revocation did not close bytes", ack, err)
				}
			}
			if mode == "cancel_after_bytes" {
				after, e := objects.Usage(context.Background(), "1001")
				if e != nil || after.UsedBytes >= usage.UsedBytes {
					t.Fatal("cancelled finalization did not release completed bytes", usage, after, e)
				}
			}
		})
	}
}
