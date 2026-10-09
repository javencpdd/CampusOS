package security

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testBudgetLimits() PurposeBudgetLimits {
	return PurposeBudgetLimits{
		MaxUnits: 10, MaxConcurrency: 2, MaxDuration: time.Second,
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func TestPurposeBudgetReserveSettleCancelAndUnknown(t *testing.T) {
	budget, err := NewPurposeBudget("model.chat", testBudgetLimits())
	if err != nil || budget.Purpose() != "model.chat" {
		t.Fatalf("create purpose budget: purpose=%q err=%v", budget.Purpose(), err)
	}
	first, err := budget.Reserve(context.Background(), 6, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	second, err := budget.Reserve(context.Background(), 4, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.Reserve(context.Background(), 1, time.Second); !errors.Is(err, ErrBudgetConcurrencyExceeded) {
		t.Fatalf("over-concurrency reserve returned %v", err)
	}
	if err := first.Settle(7); !errors.Is(err, ErrBudgetInvalid) {
		t.Fatalf("over-reservation settlement returned %v", err)
	}
	if got := budget.Snapshot(); got.ReservedUnits != 10 || got.InFlight != 2 || got.ConsumedUnits != 0 {
		t.Fatalf("invalid settlement released resources: %+v", got)
	}
	if err := first.Settle(3); err != nil {
		t.Fatal(err)
	}
	if err := first.Cancel(); !errors.Is(err, ErrBudgetLeaseClosed) {
		t.Fatalf("duplicate close returned %v", err)
	}
	if err := second.CommitUnknown(); err != nil {
		t.Fatal(err)
	}
	if got := budget.Snapshot(); got.ReservedUnits != 0 || got.ConsumedUnits != 7 || got.InFlight != 0 {
		t.Fatalf("settlement/unknown charge mismatch: %+v", got)
	}
	third, err := budget.Reserve(context.Background(), 3, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := third.Cancel(); err != nil {
		t.Fatal(err)
	}
	if got := budget.Snapshot(); got.ConsumedUnits != 7 || got.ReservedUnits != 0 {
		t.Fatalf("safe pre-dispatch cancel charged budget: %+v", got)
	}
	if _, err := budget.Reserve(context.Background(), 4, time.Second); !errors.Is(err, ErrBudgetInsufficient) {
		t.Fatalf("over-budget reserve returned %v", err)
	}
}

func TestPurposeBudgetConcurrentReservationsNeverOverspend(t *testing.T) {
	limits := testBudgetLimits()
	limits.MaxConcurrency = 100
	budget, err := NewPurposeBudget("model.embed", limits)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes atomic.Int64
	var failed atomic.Int64
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			lease, err := budget.Reserve(context.Background(), 1, time.Second)
			if errors.Is(err, ErrBudgetInsufficient) {
				failed.Add(1)
				return
			}
			if err != nil {
				t.Errorf("reserve failed unexpectedly: %v", err)
				return
			}
			successes.Add(1)
			if err := lease.Settle(1); err != nil {
				t.Errorf("settle failed: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if successes.Load() != 10 || failed.Load() != 90 {
		t.Fatalf("concurrent reservations: success=%d insufficient=%d", successes.Load(), failed.Load())
	}
	if got := budget.Snapshot(); got.ConsumedUnits != 10 || got.ReservedUnits != 0 || got.InFlight != 0 {
		t.Fatalf("concurrent budget overspent or leaked: %+v", got)
	}
}

func TestPurposeBudgetTimeoutKeepsUnknownReservation(t *testing.T) {
	limits := testBudgetLimits()
	limits.MaxDuration = 20 * time.Millisecond
	budget, err := NewPurposeBudget("model.chat", limits)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := budget.Reserve(context.Background(), 4, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-lease.Context().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("budget duration failed to cancel in-flight context")
	}
	if !errors.Is(lease.Context().Err(), context.DeadlineExceeded) {
		t.Fatalf("budget deadline returned %v", lease.Context().Err())
	}
	if got := budget.Snapshot(); got.ReservedUnits != 4 || got.InFlight != 1 {
		t.Fatalf("timeout silently refunded unknown usage: %+v", got)
	}
	if err := lease.CommitUnknown(); err != nil {
		t.Fatal(err)
	}
	if got := budget.Snapshot(); got.ConsumedUnits != 4 || got.InFlight != 0 {
		t.Fatalf("unknown result was not charged: %+v", got)
	}
}

func TestPurposeBudgetRevokeAndExpiryFailClosed(t *testing.T) {
	limits := testBudgetLimits()
	budget, err := NewPurposeBudget("broker.send", limits)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := budget.Reserve(context.Background(), 4, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	budget.Revoke()
	select {
	case <-lease.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("revocation did not cancel in-flight context")
	}
	if _, err := budget.Reserve(context.Background(), 1, time.Second); !errors.Is(err, ErrBudgetRevoked) {
		t.Fatalf("revoked budget accepted new reservation: %v", err)
	}
	if got := budget.Snapshot(); !got.Revoked || got.ReservedUnits != 4 {
		t.Fatalf("revoke silently refunded possible usage: %+v", got)
	}
	if err := lease.CommitUnknown(); err != nil {
		t.Fatal(err)
	}

	limits.ExpiresAt = time.Now().Add(120 * time.Millisecond)
	expiring, err := NewPurposeBudget("broker.expire", limits)
	if err != nil {
		t.Fatal(err)
	}
	lease, err = expiring.Reserve(context.Background(), 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-lease.Context().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("grant expiry did not cancel in-flight context")
	}
	if _, err := expiring.Reserve(context.Background(), 1, time.Second); !errors.Is(err, ErrBudgetExpired) {
		t.Fatalf("expired grant accepted new reservation: %v", err)
	}
	if err := lease.CommitUnknown(); err != nil {
		t.Fatal(err)
	}
}

func TestPurposeBudgetRejectsInvalidInputs(t *testing.T) {
	limits := testBudgetLimits()
	for _, purpose := range []string{"", " model.chat", "../escape", "host shell"} {
		if _, err := NewPurposeBudget(purpose, limits); !errors.Is(err, ErrBudgetInvalid) {
			t.Fatalf("accepted invalid purpose %q: %v", purpose, err)
		}
	}
	limits.MaxUnits = 0
	if _, err := NewPurposeBudget("model.chat", limits); !errors.Is(err, ErrBudgetInvalid) {
		t.Fatalf("accepted zero budget: %v", err)
	}
	budget, err := NewPurposeBudget("model.chat", testBudgetLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.Reserve(context.Background(), 0, time.Second); !errors.Is(err, ErrBudgetInvalid) {
		t.Fatalf("accepted zero reservation: %v", err)
	}
	if _, err := budget.Reserve(context.Background(), 1, 0); !errors.Is(err, ErrBudgetInvalid) {
		t.Fatalf("accepted zero timeout: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := budget.Reserve(canceled, 1, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("accepted canceled caller context: %v", err)
	}
}
