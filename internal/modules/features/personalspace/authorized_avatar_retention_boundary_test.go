package space

import (
	"bytes"
	"context"
	"errors"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

func TestSpaceAvatarRetentionEmptyAdmissionAndHeldMetadataABAClose(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"empty_current_proof", "held_space_aba"} {
		t.Run(mode, func(t *testing.T) {
			s, repo, _, store, files := avatarRetentionFixture(t, false)
			p, _ := spaceOwnerProof(ctx)
			ran := false
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				current, e := spaceOwnerProof(ctx)
				if ran && mode == "empty_current_proof" {
					current.CredentialID = "changed"
				}
				return current, e
			}
			if mode == "empty_current_proof" {
				store.afterList = func() { ran = true }
				ack, err := s.CleanupAvatarHistoryForPrincipal(ctx, proof)
				if !ran || err == nil || ack != nil || len(avatarUploadFileBytes(t, s, "1001")) != len(files) {
					t.Fatal("empty job proof skipped", ack, err, ran)
				}
				return
			}
			r := &avatarRetentionRun{s: s, store: s.fileStore, cfg: s.fileStore.cfg, reader: store, account: s.avatarAccount.(corestorage.AvatarRetentionAccounting), storage: s.avatarStorage, accountPort: s.avatarAccount, p: p, resolve: proof}
			scope, err := r.scope(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := readAvatarRetentionReceipt(ctx, s.fileStore, "1001", files[3].name)
			if err != nil {
				t.Fatal(err)
			}
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
			ran = true
			job, err := r.prepare(ctx, scope, *receipt)
			if !ran || err == nil || job != nil || len(avatarUploadFileBytes(t, s, "1001")) != len(files) {
				t.Fatal("held Space ABA accepted", job, err)
			}
		})
	}
}

func TestSpaceAvatarRetentionExplicitRecoveryBeforeFullQuotaUpload(t *testing.T) {
	ctx := context.Background()
	s, _, objects, _, files := avatarRetentionFixture(t, false)
	usage, err := objects.Usage(ctx, "1001")
	if err != nil {
		t.Fatal(err)
	}
	quota := s.avatarStorage.(*corestorage.LocalAdapter)
	if _, err := quota.SetQuota(ctx, "1001", usage.UsedBytes, ""); err != nil {
		t.Fatal(err)
	}
	ack, err := s.UploadAvatarForPrincipal(ctx, bytes.NewReader(tinyPNG), spaceOwnerProof)
	if !errors.Is(err, ErrSpaceFileQuotaExceeded) || ack != nil || len(avatarUploadFileBytes(t, s, "1001")) != len(files) {
		t.Fatal("prospective prune or quota bypass", ack, err)
	}
	cleaned, err := s.CleanupAvatarHistoryForPrincipal(ctx, spaceOwnerProof)
	if err != nil || cleaned.CleanupPending {
		t.Fatal(cleaned, err)
	}
	ack, err = s.UploadAvatarForPrincipal(ctx, bytes.NewReader(tinyPNG), spaceOwnerProof)
	if err != nil || ack == nil || ack.CleanupPending || len(avatarUploadFileBytes(t, s, "1001")) != 3 {
		t.Fatal("actual upload did not trim after commit", ack, err)
	}
	final, e := objects.Usage(ctx, "1001")
	if e != nil || final.UsedBytes >= usage.UsedBytes {
		t.Fatal(final, e)
	}
}
