package repository

import (
	"context"
	"errors"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

// User's legacy admin role admission remains a restriction on that User role.
// It neither authenticates an independent Admin nor grants management access.
type UserRoleAdmissionLocker interface {
	GetForUpdate(context.Context, string) (*AdminAccount, error)
}

func (r *MemoryAdminAccountRepository) GetForUpdate(ctx context.Context, user string) (*AdminAccount, error) {
	if !transaction.Active(ctx) {
		return nil, ErrUserScopeFactsUnavailable
	}
	return r.Get(ctx, user)
}

func (r *PgAdminAccountRepository) GetForUpdate(ctx context.Context, user string) (*AdminAccount, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return nil, ErrUserScopeFactsUnavailable
	}
	item, err := scanAdminAccount(r.db(ctx).QueryRow(ctx, adminAccountSelect+` WHERE aa.user_id=$1 FOR UPDATE`, user))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAdminAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}
