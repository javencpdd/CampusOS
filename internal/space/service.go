package space

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	identitydomain "github.com/campusos/CampusOS/internal/core/identity/domain"
	"github.com/campusos/CampusOS/pkg/idgen"
)

var (
	ErrInvalidVisibility = errors.New("invalid space visibility")
	ErrSpaceNotPublic    = errors.New("space is not public")
)

type UserLookup interface {
	GetByID(ctx context.Context, id string) (*identitydomain.User, error)
	GetByUsername(ctx context.Context, username string) (*identitydomain.User, error)
}

type Service struct {
	repo  Repository
	users UserLookup
}

func NewService(repo Repository, users UserLookup) *Service {
	return &Service{repo: repo, users: users}
}

func (s *Service) GetPublicByUserID(ctx context.Context, userID string) (*PublicSpace, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	return s.getPublicSpace(ctx, user)
}

func (s *Service) GetPublicByUsername(ctx context.Context, username string) (*PublicSpace, error) {
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}
	return s.getPublicSpace(ctx, user)
}

func (s *Service) GetOwnSpace(ctx context.Context, userID string) (*PublicSpace, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}

	space, err := s.repo.GetByUserID(ctx, user.ID)
	if err != nil {
		if errors.Is(err, ErrSpaceNotFound) {
			return buildPublicSpace(user, defaultSpace(user)), nil
		}
		return nil, fmt.Errorf("get space: %w", err)
	}
	return buildPublicSpace(user, space), nil
}

func (s *Service) UpsertOwnSpace(ctx context.Context, userID string, req UpsertSpaceRequest) (*PublicSpace, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}

	space, err := s.repo.GetByUserID(ctx, user.ID)
	if err != nil {
		if !errors.Is(err, ErrSpaceNotFound) {
			return nil, fmt.Errorf("get space: %w", err)
		}
		space = defaultSpace(user)
		space.ID = fmt.Sprintf("%d", idgen.New())
		space.IsDefault = false
	}

	if err := applyUpdate(space, req); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if space.CreatedAt.IsZero() {
		space.CreatedAt = now
	}
	space.UpdatedAt = now
	space.UserID = user.ID
	space.IsDefault = false

	if err := s.repo.Upsert(ctx, space); err != nil {
		return nil, fmt.Errorf("save space: %w", err)
	}

	return buildPublicSpace(user, space), nil
}

func (s *Service) getPublicSpace(ctx context.Context, user *identitydomain.User) (*PublicSpace, error) {
	space, err := s.repo.GetByUserID(ctx, user.ID)
	if err != nil {
		if errors.Is(err, ErrSpaceNotFound) {
			return buildPublicSpace(user, defaultSpace(user)), nil
		}
		return nil, fmt.Errorf("get space: %w", err)
	}
	if space.Visibility != VisibilityPublic {
		return nil, ErrSpaceNotPublic
	}
	return buildPublicSpace(user, space), nil
}

func applyUpdate(space *Space, req UpsertSpaceRequest) error {
	if req.Title != nil {
		space.Title = strings.TrimSpace(*req.Title)
	}
	if req.Bio != nil {
		space.Bio = strings.TrimSpace(*req.Bio)
	}
	if req.Avatar != nil {
		space.Avatar = strings.TrimSpace(*req.Avatar)
	}
	if req.CoverImage != nil {
		space.CoverImage = strings.TrimSpace(*req.CoverImage)
	}
	if req.Theme != nil {
		space.Theme = strings.TrimSpace(*req.Theme)
	}
	if req.Layout != nil {
		space.Layout = strings.TrimSpace(*req.Layout)
	}
	if req.Visibility != nil {
		visibility := Visibility(strings.TrimSpace(*req.Visibility))
		if !validVisibility(visibility) {
			return ErrInvalidVisibility
		}
		space.Visibility = visibility
	}
	if req.SyncEnabled != nil {
		space.SyncEnabled = *req.SyncEnabled
	}
	if req.SyncCategories != nil {
		space.SyncCategories = normalizeList(req.SyncCategories, 20)
	}
	if req.SyncTags != nil {
		space.SyncTags = normalizeList(req.SyncTags, 20)
	}
	ensureDefaults(space)
	return nil
}

func ensureDefaults(space *Space) {
	if strings.TrimSpace(space.Title) == "" {
		space.Title = "个人主页"
	}
	if strings.TrimSpace(space.Theme) == "" {
		space.Theme = "default"
	}
	if strings.TrimSpace(space.Layout) == "" {
		space.Layout = "blog"
	}
	if space.Visibility == "" {
		space.Visibility = VisibilityPublic
	}
}

func defaultSpace(user *identitydomain.User) *Space {
	now := time.Now().UTC()
	displayName := strings.TrimSpace(user.Nickname)
	if displayName == "" {
		displayName = user.Username
	}
	return &Space{
		UserID:         user.ID,
		Title:          displayName + "的个人主页",
		Bio:            user.Bio,
		Avatar:         user.Avatar,
		Theme:          "default",
		Layout:         "blog",
		Visibility:     VisibilityPublic,
		SyncEnabled:    true,
		SyncCategories: []string{},
		SyncTags:       []string{},
		IsDefault:      true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func buildPublicSpace(user *identitydomain.User, space *Space) *PublicSpace {
	return &PublicSpace{
		Owner: Owner{
			ID:       user.ID,
			Username: user.Username,
			Nickname: user.Nickname,
			Avatar:   user.Avatar,
			Bio:      user.Bio,
		},
		Space: cloneSpace(space),
	}
}

func validVisibility(visibility Visibility) bool {
	switch visibility {
	case VisibilityPublic, VisibilityUnlisted, VisibilityPrivate:
		return true
	default:
		return false
	}
}

func normalizeList(values []string, limit int) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
		if len(normalized) >= limit {
			break
		}
	}
	return normalized
}
