package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func TestAdminLocalRecoveryRepositoryRequiresTransaction(t *testing.T) {
	ctx := context.Background()
	a, m := NewMemoryAdminDomainRepository(), NewMemoryAdminSecurityRepository()
	if _, err := a.GetCredentialForUpdate(ctx, "absent"); !errors.Is(err, ErrAdminAuthorityLockUnavailable) {
		t.Fatal(err)
	}
	if _, err := m.GetTOTPForUpdate(ctx, "absent"); !errors.Is(err, ErrAdminAuthorityLockUnavailable) {
		t.Fatal(err)
	}
	if err := m.InvalidateLocalRecoveryTickets(ctx, "absent", time.Now()); !errors.Is(err, ErrAdminAuthorityLockUnavailable) {
		t.Fatal(err)
	}
}

func TestAdminLocalRecoveryRepositoryInvalidatesOnlyOwnedOutstandingTickets(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryAdminSecurityRepository()
	now := time.Now().UTC().Truncate(time.Microsecond)
	for i, id := range []string{"one", "two"} {
		for _, recovery := range []bool{false, true} {
			ticket := AdminSecurityTicket{ID: id, AccountID: id, Audience: "admin-api", Purpose: "login", Digest: strings.Repeat(string(rune('a'+i)), 64), ExpiresAt: now.Add(time.Minute), MaxAttempts: 5}
			var err error
			if recovery {
				ticket.Purpose, ticket.Audience = "password_reset", ""
				err = m.CreateRecoveryTicket(ctx, ticket)
			} else {
				err = m.CreateAdminTicket(ctx, ticket)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	tx := transaction.NewMemory(m)
	if err := tx.Within(ctx, func(ctx context.Context) error { return m.InvalidateLocalRecoveryTickets(ctx, "one", now) }); err != nil {
		t.Fatal(err)
	}
	for _, recovery := range []bool{false, true} {
		get := m.GetAdminTicketForUpdate
		if recovery {
			get = m.GetRecoveryTicketForUpdate
		}
		owned, err := get(ctx, strings.Repeat("a", 64))
		if err != nil || owned.ConsumedAt == nil || !owned.ConsumedAt.Equal(now) {
			t.Fatal("owned outstanding ticket survived", owned, err)
		}
		other, err := get(ctx, strings.Repeat("b", 64))
		if err != nil || other.ConsumedAt != nil {
			t.Fatal("other account ticket changed", other, err)
		}
	}
	if err := tx.Within(ctx, func(ctx context.Context) error {
		return m.InvalidateLocalRecoveryTickets(ctx, "one", now.Add(time.Minute))
	}); err != nil {
		t.Fatal(err)
	}
	owned, _ := m.GetRecoveryTicketForUpdate(ctx, strings.Repeat("a", 64))
	if !owned.ConsumedAt.Equal(now) {
		t.Fatal("consumption evidence was rewritten")
	}
}
