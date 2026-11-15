package repository

import (
	"context"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

// AdminProvisioningStore serializes trusted deployment provisioning. It is
// deliberately separate from the authenticated account-management interface.
type AdminProvisioningStore interface {
	CountAdminAccounts(context.Context) (int, error)
	LockAdminProvisioning(context.Context) error
}

func (r *PgAdminDomainRepository) CountAdminAccounts(ctx context.Context) (int, error) {
	var count int
	err := r.db(ctx).QueryRow(ctx, `SELECT count(*) FROM admin_accounts`).Scan(&count)
	return count, err
}

func (r *PgAdminDomainRepository) LockAdminProvisioning(ctx context.Context) error {
	if !transaction.Active(ctx) {
		return ErrAdminAuthorityLockUnavailable
	}
	_, err := r.db(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('campusos.admin.provisioning', 0))`)
	return err
}

func (r *MemoryAdminDomainRepository) CountAdminAccounts(_ context.Context) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.accounts), nil
}

func (r *MemoryAdminDomainRepository) LockAdminProvisioning(ctx context.Context) error {
	// The Memory transaction manager already serializes the registered stores.
	if !transaction.Active(ctx) {
		return ErrAdminAuthorityLockUnavailable
	}
	return nil
}
