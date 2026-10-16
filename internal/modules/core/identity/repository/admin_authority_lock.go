package repository

import (
	"context"
	"errors"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

var ErrAdminAuthorityLockUnavailable = errors.New("admin authority transaction lock is unavailable")

type AdminAccountAuthorityLocker interface {
	GetAccountByIDForUpdate(context.Context, string) (AdminDomainAccount, error)
}
type AdminSessionAuthorityLocker interface {
	GetSessionByIDForUpdate(context.Context, string) (AdminSession, error)
}

func (r *MemoryAdminDomainRepository) GetAccountByIDForUpdate(ctx context.Context, id string) (AdminDomainAccount, error) {
	if !transaction.Active(ctx) {
		return AdminDomainAccount{}, ErrAdminAuthorityLockUnavailable
	}
	return r.GetAccountByID(ctx, id)
}
func (r *MemoryAdminSessionRepository) GetSessionByIDForUpdate(ctx context.Context, id string) (AdminSession, error) {
	if !transaction.Active(ctx) {
		return AdminSession{}, ErrAdminAuthorityLockUnavailable
	}
	return r.GetSessionByID(ctx, id)
}
func (r *PgAdminDomainRepository) GetAccountByIDForUpdate(ctx context.Context, id string) (AdminDomainAccount, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return AdminDomainAccount{}, ErrAdminAuthorityLockUnavailable
	}
	a, err := scanAdminDomainAccount(r.db(ctx).QueryRow(ctx, `SELECT `+adminAccountColumns+` FROM admin_accounts WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminDomainAccount{}, ErrAdminDomainAccountNotFound
	}
	return a, err
}
func (r *PgAdminSessionRepository) GetSessionByIDForUpdate(ctx context.Context, id string) (AdminSession, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return AdminSession{}, ErrAdminAuthorityLockUnavailable
	}
	s, err := scanAdminSession(r.db(ctx).QueryRow(ctx, `SELECT `+adminSessionColumns+` FROM admin_sessions WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminSession{}, ErrAdminSessionNotFound
	}
	return s, err
}
