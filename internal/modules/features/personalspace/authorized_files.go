package space

import (
	"context"
	"errors"

	identitydomain "github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
)

type storageOwnerSnapshot struct {
	ID, Avatar string
	Status     identitydomain.UserStatus
	Space      *Space
}

func (s *Service) storageOwnerSnapshot(ctx context.Context, owner string) (storageOwnerSnapshot, error) {
	if err := s.ensureEnabled(); err != nil {
		return storageOwnerSnapshot{}, err
	}
	if corestorage.NilImageAdapter(s.users) || corestorage.NilImageAdapter(s.repo) || s.fileStore == nil {
		return storageOwnerSnapshot{}, ErrSpaceFileStoreUnavailable
	}
	user, err := s.users.GetByID(ctx, owner)
	if err != nil || user == nil || user.ID != owner || user.Status != identitydomain.UserStatusActive {
		return storageOwnerSnapshot{}, ErrSpaceFileNotFound
	}
	space, err := s.repo.GetByUserID(ctx, owner)
	if errors.Is(err, ErrSpaceNotFound) {
		space = nil
	} else if err != nil {
		return storageOwnerSnapshot{}, err
	}
	if space != nil && space.UserID != owner {
		return storageOwnerSnapshot{}, identityport.ErrPolicyDecisionMismatch
	}
	return storageOwnerSnapshot{ID: user.ID, Avatar: user.Avatar, Status: user.Status, Space: space}, nil
}

type avatarByteSnapshot struct {
	Owner storageOwnerSnapshot
	Image corestorage.ImageBytes
}

// The only public avatar exception is an exact current profile reference.
// Retained history and a private/disabled space never become public by URL.
func (s *Service) ReadAvatarForPrincipal(ctx context.Context, owner, name string, resolve identityport.SessionPrincipalResolver) (corestorage.ImageBytes, error) {
	if !corestorage.SafeSegment(owner) || !corestorage.SafeSegment(name) {
		return corestorage.ImageBytes{}, ErrSpaceFileNotFound
	}
	return identityport.ReadBoundProjection(ctx, resolve, true,
		func(ctx context.Context, p identityport.PrincipalContext) (avatarByteSnapshot, error) {
			facts, err := s.storageOwnerSnapshot(ctx, owner)
			if err != nil {
				return avatarByteSnapshot{}, err
			}
			url := s.fileStore.AvatarURL(owner, name)
			public := facts.Avatar == url || (facts.Space != nil && facts.Space.Avatar == url && facts.Space.Visibility == VisibilityPublic && facts.Space.DisabledAt == nil)
			if !public && (p.Actor.Kind != "user" || p.Actor.ID != owner) {
				return avatarByteSnapshot{}, ErrSpaceFileNotFound
			}
			path, err := s.fileStore.AvatarPath(owner, name)
			if err != nil {
				return avatarByteSnapshot{}, ErrSpaceFileNotFound
			}
			image, err := corestorage.ReadImageBytes(path, s.fileStore.MaxAvatarBytes())
			if err != nil {
				if errors.Is(err, corestorage.ErrImageNotFound) {
					return avatarByteSnapshot{}, ErrSpaceFileNotFound
				}
				return avatarByteSnapshot{}, err
			}
			return avatarByteSnapshot{Owner: facts, Image: image}, nil
		}, func(_ context.Context, _ identityport.PrincipalContext, snap avatarByteSnapshot) (corestorage.ImageBytes, error) {
			image := snap.Image
			image.Data = append([]byte{}, image.Data...)
			return image, nil
		}, nil)
}

type avatarHistorySnapshot struct {
	Owner   storageOwnerSnapshot
	History *AvatarHistory
}

func (s *Service) ListAvatarsForPrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*AvatarHistory, error) {
	return identityport.ReadBoundProjection(ctx, resolve, false,
		func(ctx context.Context, p identityport.PrincipalContext) (avatarHistorySnapshot, error) {
			owner, err := s.storageOwnerSnapshot(ctx, p.Actor.ID)
			if err != nil {
				return avatarHistorySnapshot{}, err
			}
			history, err := s.ListAvatars(ctx, p.Actor.ID)
			if err != nil {
				return avatarHistorySnapshot{}, err
			}
			if history != nil {
				current, err := s.currentStorageStatus(ctx, p.Actor.ID)
				if err != nil {
					return avatarHistorySnapshot{}, err
				}
				history.Storage = *current
			}
			if history == nil || history.Storage.UserID != p.Actor.ID {
				return avatarHistorySnapshot{}, identityport.ErrPolicyDecisionMismatch
			}
			seen := map[string]bool{}
			for _, a := range history.Items {
				if !corestorage.SafeSegment(a.FileName) || a.URL != s.fileStore.AvatarURL(p.Actor.ID, a.FileName) || a.Size < 1 || seen[a.FileName] {
					return avatarHistorySnapshot{}, identityport.ErrPolicyDecisionMismatch
				}
				seen[a.FileName] = true
			}
			return avatarHistorySnapshot{Owner: owner, History: history}, nil
		}, func(_ context.Context, _ identityport.PrincipalContext, snap avatarHistorySnapshot) (*AvatarHistory, error) {
			return snap.History, nil
		}, nil)
}

type ownerStorageSnapshot struct {
	Owner  storageOwnerSnapshot
	Status *SpaceStorageStatus
}

func (s *Service) StorageStatusForPrincipal(ctx context.Context, resolve identityport.SessionPrincipalResolver) (*SpaceStorageStatus, error) {
	return identityport.ReadBoundProjection(ctx, resolve, false,
		func(ctx context.Context, p identityport.PrincipalContext) (ownerStorageSnapshot, error) {
			owner, err := s.storageOwnerSnapshot(ctx, p.Actor.ID)
			if err != nil {
				return ownerStorageSnapshot{}, err
			}
			status, err := s.currentStorageStatus(ctx, p.Actor.ID)
			if err != nil {
				return ownerStorageSnapshot{}, err
			}
			if status == nil || status.UserID != p.Actor.ID {
				return ownerStorageSnapshot{}, identityport.ErrPolicyDecisionMismatch
			}
			return ownerStorageSnapshot{Owner: owner, Status: status}, nil
		}, func(_ context.Context, _ identityport.PrincipalContext, snap ownerStorageSnapshot) (*SpaceStorageStatus, error) {
			return snap.Status, nil
		}, nil)
}

func (s *Service) currentStorageStatus(ctx context.Context, owner string) (*SpaceStorageStatus, error) {
	reader, ok := s.quotaManager.(corestorage.CurrentQuotaReader)
	if !ok || corestorage.NilImageAdapter(reader) {
		return nil, ErrStorageQuotaUnavailable
	}
	status, err := s.StorageStatus(ctx, owner)
	if err != nil {
		return nil, err
	}
	record, exists, err := reader.CurrentQuota(ctx, owner)
	if err != nil {
		return nil, err
	}
	status.QuotaBytes = s.quotaManager.DefaultQuotaBytes()
	status.DefaultQuotaBytes = status.QuotaBytes
	status.CustomQuota = false
	status.QuotaUpdatedBy = ""
	status.QuotaUpdatedAt = nil
	if exists {
		if record.UserID != owner || record.QuotaBytes < 1 {
			return nil, identityport.ErrPolicyDecisionMismatch
		}
		status.QuotaBytes = record.QuotaBytes
		status.CustomQuota = true
		status.QuotaUpdatedBy = record.UpdatedBy
		at := record.UpdatedAt
		status.QuotaUpdatedAt = &at
	}
	status.AvailableBytes = status.QuotaBytes - status.UsedBytes
	if status.AvailableBytes < 0 {
		status.AvailableBytes = 0
	}
	return status, nil
}
