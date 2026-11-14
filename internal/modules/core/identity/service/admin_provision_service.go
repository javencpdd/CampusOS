package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/core/identity/repository"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var ErrAdminProvisionInvalid = errors.New("invalid administrator provisioning request")

// AdminProvisionService is a trusted deployment entry, available only to
// startup and the local operator CLI. It is never exposed over HTTP or plugins.
type AdminProvisionService struct {
	domain *AdminDomainService
}

func NewAdminProvisionService(admins repository.AdminDomainRepository, catalog repository.AuthorizationRepository, reliable *reliability.Service) *AdminProvisionService {
	domain := NewAdminDomainService(admins, catalog, DefaultAdminDomainConfig())
	domain.SetReliability(reliable)
	if reliable != nil {
		if snapshot, ok := catalog.(transaction.Snapshotter); ok {
			reliable.RegisterMemorySnapshotters(snapshot)
		}
	}
	return &AdminProvisionService{domain: domain}
}

// EnsureDefault creates the requested initial admin/admin only in a completely
// empty admin domain. Existing, suspended and revoked accounts all prevent
// reseeding, and no existing credential, MFA setting or role is overwritten.
func (s *AdminProvisionService) EnsureDefault(ctx context.Context) error {
	return s.create(ctx, "admin", SystemAdministratorEmail, "admin", true)
}

// Create adds an independent security administrator under local deployment
// custody. Duplicate usernames/emails fail; this entry never resets an account.
func (s *AdminProvisionService) Create(ctx context.Context, username, email, password string) error {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 320 || len(password) < 1 || len(password) > 72 || strings.TrimSpace(username) != username {
		return ErrAdminProvisionInvalid
	}
	return s.create(ctx, username, email, password, false)
}

func (s *AdminProvisionService) create(ctx context.Context, username, email, password string, onlyEmpty bool) error {
	if s == nil || s.domain == nil || s.domain.reliable == nil || s.domain.catalog == nil {
		return ErrAdminDomainUnavailable
	}
	store, ok := s.domain.admins.(repository.AdminProvisioningStore)
	if !ok {
		return ErrAdminDomainUnavailable
	}
	if onlyEmpty {
		count, err := store.CountAdminAccounts(ctx)
		if err != nil || count != 0 {
			return err
		}
	}
	id := newAdminID()
	reason := "local deployment administrator creation"
	if onlyEmpty {
		reason = "initial default administrator creation"
	}
	return s.domain.executeLifecycle(ctx, "system", "local-admin-provision", "identity.admin_domain.bootstrap", id, func(commandCtx context.Context) error {
		if err := store.LockAdminProvisioning(commandCtx); err != nil {
			return err
		}
		if onlyEmpty {
			count, err := store.CountAdminAccounts(commandCtx)
			if err != nil || count != 0 {
				return err
			}
		}
		return s.domain.createAccountRecord(commandCtx, id, username, email, password, "security_admin", "system", "local-admin-provision", "identity.admin_domain.bootstrap", reason)
	})
}
