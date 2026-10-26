package repository

import (
	"context"
	"errors"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

// These primitives are used only by trusted local Admin recovery. The caller
// locks the Admin account first; neither primitive reads the User domain.
type AdminLocalRecoveryCredentialLocker interface {
	GetCredentialForUpdate(context.Context, string) (AdminDomainCredential, error)
}

type AdminLocalRecoverySecurityStore interface {
	GetTOTPForUpdate(context.Context, string) (AdminTOTPMethod, error)
	InvalidateLocalRecoveryTickets(context.Context, string, time.Time) error
}

func (r *PgAdminDomainRepository) GetCredentialForUpdate(ctx context.Context, id string) (AdminDomainCredential, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return AdminDomainCredential{}, ErrAdminAuthorityLockUnavailable
	}
	var c AdminDomainCredential
	err := r.db(ctx).QueryRow(ctx, `SELECT account_id,credential_hash,credential_version,password_changed_at FROM admin_credentials WHERE account_id=$1 FOR UPDATE`, id).
		Scan(&c.AccountID, &c.CredentialHash, &c.CredentialVersion, &c.PasswordChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrAdminDomainCredentialMissing
	}
	return c, err
}

func (r *MemoryAdminDomainRepository) GetCredentialForUpdate(ctx context.Context, id string) (AdminDomainCredential, error) {
	if !transaction.Active(ctx) {
		return AdminDomainCredential{}, ErrAdminAuthorityLockUnavailable
	}
	return r.GetCredential(ctx, id)
}

func (r *PgAdminSecurityRepository) GetTOTPForUpdate(ctx context.Context, id string) (AdminTOTPMethod, error) {
	if _, ok := transaction.FromContext(ctx); !ok {
		return AdminTOTPMethod{}, ErrAdminAuthorityLockUnavailable
	}
	m, err := scanAdminTOTP(r.db(ctx).QueryRow(ctx, `SELECT `+adminTOTPColumns+` FROM admin_mfa_totp WHERE account_id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrAdminDomainTOTPNotFound
	}
	return m, err
}

func (r *MemoryAdminSecurityRepository) GetTOTPForUpdate(ctx context.Context, id string) (AdminTOTPMethod, error) {
	if !transaction.Active(ctx) {
		return AdminTOTPMethod{}, ErrAdminAuthorityLockUnavailable
	}
	return r.GetTOTP(ctx, id)
}

func (r *PgAdminSecurityRepository) InvalidateLocalRecoveryTickets(ctx context.Context, id string, at time.Time) error {
	if _, ok := transaction.FromContext(ctx); !ok {
		return ErrAdminAuthorityLockUnavailable
	}
	// Preserve tickets as evidence, including expired and exhausted tickets.
	// The fixed table order and row order precede Admin session revocation.
	for _, table := range []string{"admin_mfa_tickets", "admin_recovery_tickets"} {
		rows, err := r.db(ctx).Query(ctx, `SELECT id FROM `+table+` WHERE account_id=$1 AND consumed_at IS NULL ORDER BY id FOR UPDATE`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var ticketID string
			if err = rows.Scan(&ticketID); err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if _, err = r.db(ctx).Exec(ctx, `UPDATE `+table+` SET consumed_at=$2 WHERE account_id=$1 AND consumed_at IS NULL`, id, at); err != nil {
			return err
		}
	}
	return nil
}

func (r *MemoryAdminSecurityRepository) InvalidateLocalRecoveryTickets(ctx context.Context, id string, at time.Time) error {
	if !transaction.Active(ctx) {
		return ErrAdminAuthorityLockUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, tickets := range []map[string]AdminSecurityTicket{r.tickets, r.recovery} {
		for key, ticket := range tickets {
			if ticket.AccountID == id && ticket.ConsumedAt == nil {
				stamp := at
				ticket.ConsumedAt = &stamp
				tickets[key] = ticket
			}
		}
	}
	return nil
}
