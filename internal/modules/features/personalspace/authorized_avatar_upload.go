package space

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"

	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

// AvatarMutationAck reports a saved avatar separately from deferred retention.
type AvatarMutationAck struct {
	SpaceMutationAck
	CleanupPending bool `json:"cleanup_pending,omitempty"`
}

func (s *Service) SetAvatarStorage(storage corestorage.Port, account corestorage.InlineImageAccounting) {
	s.avatarStorage, s.avatarAccount = storage, account
}

func (s *Service) UploadAvatarForPrincipal(ctx context.Context, reader io.Reader, resolve identityport.SessionPrincipalResolver) (result *AvatarMutationAck, returnErr error) {
	if s == nil || s.reliable == nil || s.fileStore == nil || corestorage.NilImageAdapter(s.avatarStorage) || corestorage.NilImageAdapter(s.avatarAccount) || transaction.Active(ctx) {
		return nil, ErrSpaceFactsUnavailable
	}
	store, storage, account := s.fileStore, s.avatarStorage, s.avatarAccount
	if !sameStoragePath(store.cfg.RootDir, storage.Root()) {
		return nil, ErrSpaceFactsUnavailable
	}
	p, err := spaceOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	initial, err := s.loadSpaceOwnerFacts(ctx, p.Actor.ID, false)
	if err != nil {
		return nil, err
	}
	r := spaceOwnerRequest(initial, p, "avatar_upload", "space-avatar-upload")
	d, err := (identitypolicy.PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(r)
	if err != nil || d.Effect != "allow" {
		return nil, errors.Join(ErrSpaceOwnerDenied, err)
	}
	cfg := store.cfg
	file, err := corestorage.PrepareInlineImage(ctx, reader, store.MaxAvatarBytes())
	if err != nil {
		return nil, spaceAvatarUploadError(err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	before, err := store.avatarFiles(p.Actor.ID)
	if err != nil {
		return nil, errors.Join(ErrSpaceFactsUnavailable, err)
	}
	expected := append([]avatarFile{}, before...)
	written, published := false, false
	undo := func() error { return nil }
	defer func() {
		if !published {
			returnErr = errors.Join(returnErr, file.Discard(), undo())
		}
	}()
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		fresh, err := spaceOwnerPrincipal(ctx, resolve)
		if err != nil {
			return fresh, err
		}
		if !reflect.DeepEqual(p, fresh) {
			return fresh, identityport.ErrPolicyFactsChanged
		}
		return fresh, nil
	}
	check := func(ctx context.Context) error {
		if s.fileStore != store || !reflect.DeepEqual(cfg, store.cfg) || !sameAvatarAdapter(storage, s.avatarStorage) || !sameAvatarAdapter(account, s.avatarAccount) || !sameStoragePath(cfg.RootDir, storage.Root()) {
			return identityport.ErrPolicyFactsChanged
		}
		current, err := store.avatarFiles(p.Actor.ID)
		if err != nil {
			return errors.Join(ErrSpaceFactsUnavailable, err)
		}
		if !reflect.DeepEqual(expected, current) {
			return identityport.ErrPolicyFactsChanged
		}
		if written {
			return file.Check(ctx)
		}
		return ctx.Err()
	}
	write := func(tx context.Context) (string, error) {
		var err error
		undo, err = account.AccountInlineImageForCommand(tx, p.Actor.ID, file.Size())
		if err != nil {
			return "", spaceAvatarUploadError(err)
		}
		if err = file.WriteAvatarForCommand(tx, storage, p.Actor.ID, cfg.URLPrefix); err != nil {
			return "", err
		}
		image := file.Metadata()
		if image.FileURL != store.AvatarURL(p.Actor.ID, image.FileName) {
			return "", identityport.ErrPolicyFactsChanged
		}
		path, err := store.AvatarPath(p.Actor.ID, image.FileName)
		if err != nil {
			return "", err
		}
		info, err := avatarSelectionSnapshot(tx, store, p.Actor.ID, image.FileName)
		if err != nil {
			return "", err
		}
		newFile := avatarFile{dir: filepath.Dir(path), name: image.FileName, size: image.FileSize, modTime: info.Info.ModTime()}
		expected = append(expected, newFile)
		sortAvatarFiles(expected)
		written = true
		if err = check(tx); err != nil {
			return "", err
		}
		return image.FileURL, nil
	}
	ack, err := s.executeSpaceOwnerMutation(ctx, UpsertSpaceRequest{}, "avatar_upload", bound, initial, check, write)
	if err != nil {
		return nil, spaceAvatarUploadError(err)
	}
	published = true
	// The saved upload remains successful when the separate, durably prepared
	// retention lifecycle needs retry. Never count hoped reclamation as quota.
	pending, cleanupErr := s.cleanupAvatarHistoryLocked(ctx, store, p, bound)
	return &AvatarMutationAck{SpaceMutationAck: *ack, CleanupPending: pending || cleanupErr != nil}, nil
}

func sameAvatarAdapter(a, b any) bool {
	return reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.ValueOf(a).Comparable() && a == b
}
func spaceAvatarUploadError(err error) error {
	switch {
	case errors.Is(err, corestorage.ErrImageTooLarge):
		return errors.Join(ErrSpaceFileTooLarge, err)
	case errors.Is(err, corestorage.ErrImageUnsupported):
		return errors.Join(ErrSpaceFileUnsupportedType, err)
	case errors.Is(err, corestorage.ErrObjectQuota), errors.Is(err, corestorage.ErrQuotaExceeded):
		return errors.Join(ErrSpaceFileQuotaExceeded, err)
	default:
		return err
	}
}
