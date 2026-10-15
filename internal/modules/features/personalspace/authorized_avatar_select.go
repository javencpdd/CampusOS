package space

import (
	"context"
	"errors"
	"fmt"
	identitypolicy "github.com/campusos/CampusOS/internal/modules/core/identity/policy"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/campusos/CampusOS/pkg/idgen"
	"os"
	"reflect"
	"strings"
)

type avatarSelectionFacts struct {
	Inventory []avatarFile
	Image     corestorage.ImageBytes
	Info      os.FileInfo
}

func avatarSelectionSnapshot(ctx context.Context, store *LocalFileStore, owner, name string) (*avatarSelectionFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files, err := store.avatarFiles(owner)
	if err != nil {
		return nil, errors.Join(ErrSpaceFactsUnavailable, err)
	}
	var selected *avatarFile
	for i := range files {
		if files[i].name == name {
			selected = &files[i]
			break
		}
	}
	if selected == nil {
		return nil, ErrSpaceFileNotFound
	}
	path, err := store.AvatarPath(owner, name)
	if err != nil {
		return nil, errors.Join(ErrSpaceFileNotFound, err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrSpaceFileNotFound
	}
	image, err := corestorage.ReadImageBytes(path, store.MaxAvatarBytes())
	if err != nil {
		return nil, errors.Join(ErrSpaceFileNotFound, err)
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) || info.Size() != selected.size || int64(len(image.Data)) != info.Size() || !info.ModTime().Equal(image.ModifiedAt) || !info.ModTime().Equal(selected.modTime) {
		return nil, identityport.ErrPolicyFactsChanged
	}
	return &avatarSelectionFacts{Inventory: files, Image: image, Info: info}, nil
}
func checkAvatarSelectionFacts(initial, current *avatarSelectionFacts) error {
	if initial == nil || current == nil || !reflect.DeepEqual(initial.Inventory, current.Inventory) || !reflect.DeepEqual(initial.Image, current.Image) || !os.SameFile(initial.Info, current.Info) || initial.Info.Mode() != current.Info.Mode() {
		return identityport.ErrPolicyFactsChanged
	}
	return nil
}
func (s *Service) SelectAvatarForPrincipal(ctx context.Context, name string, resolve identityport.SessionPrincipalResolver) (*SpaceMutationAck, error) {
	if s == nil || s.fileStore == nil || s.reliable == nil || transaction.Active(ctx) {
		return nil, ErrSpaceFactsUnavailable
	}
	name = strings.TrimSpace(name)
	if validateStorageSegment(name) != nil {
		return nil, ErrSpaceFileInvalidName
	}
	p, err := spaceOwnerPrincipal(ctx, resolve)
	if err != nil {
		return nil, err
	}
	initial, err := s.loadSpaceOwnerFacts(ctx, p.Actor.ID, false)
	if err != nil {
		return nil, err
	}
	r := spaceOwnerRequest(initial, p, "avatar_select", fmt.Sprint(idgen.New()))
	d, err := (identitypolicy.PersonalSpaceOwnerPolicy{}).DecidePersonalSpaceOwner(r)
	if err != nil {
		return nil, err
	}
	if d.Effect != "allow" {
		return nil, ErrSpaceOwnerDenied
	}
	store := s.fileStore
	cfg := store.cfg
	store.mu.Lock()
	defer store.mu.Unlock()
	files, err := avatarSelectionSnapshot(ctx, store, p.Actor.ID, name)
	if err != nil {
		return nil, err
	}
	check := func(ctx context.Context) error {
		if s.fileStore != store || !reflect.DeepEqual(cfg, store.cfg) {
			return identityport.ErrPolicyFactsChanged
		}
		current, err := avatarSelectionSnapshot(ctx, store, p.Actor.ID, name)
		if err != nil {
			return err
		}
		return checkAvatarSelectionFacts(files, current)
	}
	bound := func(ctx context.Context) (identityport.PrincipalContext, error) {
		current, err := spaceOwnerPrincipal(ctx, resolve)
		if err != nil {
			return current, err
		}
		if !reflect.DeepEqual(p, current) {
			return identityport.PrincipalContext{}, identityport.ErrPolicyFactsChanged
		}
		return current, nil
	}
	url := store.AvatarURL(p.Actor.ID, name)
	return s.executeSpaceOwnerMutation(ctx, UpsertSpaceRequest{Avatar: &url}, "avatar_select", bound, initial, check, nil)
}
