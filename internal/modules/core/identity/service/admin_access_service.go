package service

import (
	"context"
	"errors"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
)

var ErrAdminAccessDenied = errors.New("administrator account is not active")

// AdminAccessService is the management-plane admission boundary. It is
// intentionally separate from RBAC: an active admin account and the requested
// permission are both required for an Admin route.
type AdminAccessService struct {
	repo repository.AdminAccountRepository
}

func NewAdminAccessService(repo repository.AdminAccountRepository) *AdminAccessService {
	return &AdminAccessService{repo: repo}
}

func (s *AdminAccessService) CheckAdminAccess(ctx context.Context, userID string) (bool, error) {
	if s == nil || s.repo == nil || userID == "" {
		return false, nil
	}
	return s.repo.IsActive(ctx, userID)
}

func (s *AdminAccessService) Require(ctx context.Context, userID string) error {
	allowed, err := s.CheckAdminAccess(ctx, userID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrAdminAccessDenied
	}
	return nil
}

func (s *AdminAccessService) RecordAuthentication(ctx context.Context, userID string) error {
	if err := s.Require(ctx, userID); err != nil {
		return err
	}
	return s.repo.MarkAuthenticated(ctx, userID, time.Now().UTC())
}
