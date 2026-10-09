package security

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"time"
)

var (
	ErrBudgetInvalid             = errors.New("plugin.budget_invalid")
	ErrBudgetInsufficient        = errors.New("plugin.budget_insufficient")
	ErrBudgetConcurrencyExceeded = errors.New("plugin.budget_concurrency_exceeded")
	ErrBudgetExpired             = errors.New("plugin.budget_expired")
	ErrBudgetRevoked             = errors.New("plugin.budget_revoked")
	ErrBudgetLeaseClosed         = errors.New("plugin.budget_lease_closed")
)

type PurposeBudgetLimits struct {
	MaxUnits       int64
	MaxConcurrency int
	MaxDuration    time.Duration
	ExpiresAt      time.Time
}

type PurposeBudgetSnapshot struct {
	Purpose       string
	MaxUnits      int64
	ConsumedUnits int64
	ReservedUnits int64
	InFlight      int
	Revoked       bool
	Expired       bool
}

// PurposeBudget is a process-local, purpose-bound limit. The caller must
// create a distinct budget per approved purpose and never share one across
// plugin instances or remote hosts as though it were a distributed ledger.
type PurposeBudget struct {
	mu       sync.Mutex
	purpose  string
	limits   PurposeBudgetLimits
	consumed int64
	reserved int64
	leases   map[uint64]*budgetReservation
	nextID   uint64
	revoked  bool
}

type budgetReservation struct {
	units  int64
	cancel context.CancelFunc
}

type BudgetLease struct {
	budget *PurposeBudget
	id     uint64
	ctx    context.Context
}

var purposeCode = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

func NewPurposeBudget(purpose string, limits PurposeBudgetLimits) (*PurposeBudget, error) {
	if !purposeCode.MatchString(purpose) || limits.MaxUnits < 1 || limits.MaxConcurrency < 1 ||
		limits.MaxConcurrency > 1024 || limits.MaxDuration <= 0 || limits.MaxDuration > 5*time.Minute ||
		limits.ExpiresAt.IsZero() || !limits.ExpiresAt.After(time.Now()) {
		return nil, ErrBudgetInvalid
	}
	return &PurposeBudget{
		purpose: purpose, limits: limits, leases: make(map[uint64]*budgetReservation),
	}, nil
}

func (b *PurposeBudget) Purpose() string {
	if b == nil {
		return ""
	}
	return b.purpose
}

// Reserve atomically holds units and one concurrency slot before any external
// side effect. Its Context has the strictest caller, requested, policy and
// grant expiry deadline. A timeout cancels execution but deliberately does
// not refund units: the remote result may be unknown. The caller must Settle,
// CommitUnknown, or Cancel only when it can prove nothing was dispatched.
func (b *PurposeBudget) Reserve(parent context.Context, units int64, requestedTimeout time.Duration) (*BudgetLease, error) {
	if b == nil || parent == nil || units <= 0 || requestedTimeout <= 0 {
		return nil, ErrBudgetInvalid
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.revoked {
		return nil, ErrBudgetRevoked
	}
	now := time.Now()
	if !now.Before(b.limits.ExpiresAt) {
		return nil, ErrBudgetExpired
	}
	if len(b.leases) >= b.limits.MaxConcurrency {
		return nil, ErrBudgetConcurrencyExceeded
	}
	// Subtraction avoids overflow even when the configured unit budget is large.
	if units > b.limits.MaxUnits-b.consumed-b.reserved {
		return nil, ErrBudgetInsufficient
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	deadline := now.Add(min(requestedTimeout, b.limits.MaxDuration))
	if b.limits.ExpiresAt.Before(deadline) {
		deadline = b.limits.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	b.nextID++
	id := b.nextID
	b.leases[id] = &budgetReservation{units: units, cancel: cancel}
	b.reserved += units
	return &BudgetLease{budget: b, id: id, ctx: ctx}, nil
}

func (l *BudgetLease) Context() context.Context {
	if l == nil || l.ctx == nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return l.ctx
}

// Settle charges actual usage, which must fit inside the reservation. A failed
// settlement leaves the full reservation held so the caller cannot overspend.
func (l *BudgetLease) Settle(actualUnits int64) error {
	if l == nil || l.budget == nil {
		return ErrBudgetInvalid
	}
	return l.budget.finish(l.id, actualUnits, false)
}

// CommitUnknown charges the entire reservation when a dispatched operation
// times out or its remote billing outcome cannot be proven.
func (l *BudgetLease) CommitUnknown() error {
	if l == nil || l.budget == nil {
		return ErrBudgetInvalid
	}
	return l.budget.finish(l.id, 0, true)
}

// Cancel refunds only a reservation known to have caused no external effect.
// It must not be used for a sent request with an unknown billing result.
func (l *BudgetLease) Cancel() error {
	if l == nil || l.budget == nil {
		return ErrBudgetInvalid
	}
	return l.budget.finish(l.id, 0, false)
}

func (b *PurposeBudget) finish(id uint64, actualUnits int64, chargeFull bool) error {
	b.mu.Lock()
	reservation, exists := b.leases[id]
	if !exists {
		b.mu.Unlock()
		return ErrBudgetLeaseClosed
	}
	if actualUnits < 0 || actualUnits > reservation.units {
		b.mu.Unlock()
		return ErrBudgetInvalid
	}
	if chargeFull {
		actualUnits = reservation.units
	}
	delete(b.leases, id)
	b.reserved -= reservation.units
	b.consumed += actualUnits
	b.mu.Unlock()
	reservation.cancel()
	return nil
}

// Revoke rejects new reservations and cancels every in-flight execution
// context immediately. Existing reservations stay held until their caller
// records known usage or commits the full unknown amount.
func (b *PurposeBudget) Revoke() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.revoked = true
	cancels := make([]context.CancelFunc, 0, len(b.leases))
	for _, reservation := range b.leases {
		cancels = append(cancels, reservation.cancel)
	}
	b.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (b *PurposeBudget) Snapshot() PurposeBudgetSnapshot {
	if b == nil {
		return PurposeBudgetSnapshot{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return PurposeBudgetSnapshot{
		Purpose: b.purpose, MaxUnits: b.limits.MaxUnits, ConsumedUnits: b.consumed,
		ReservedUnits: b.reserved, InFlight: len(b.leases), Revoked: b.revoked,
		Expired: !time.Now().Before(b.limits.ExpiresAt),
	}
}
