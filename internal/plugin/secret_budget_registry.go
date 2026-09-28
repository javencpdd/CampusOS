package plugin

import (
	"context"
	"sync"

	"github.com/campusos/CampusOS/internal/platform/security"
)

type secretBudgetKey struct {
	versionID int64
	purpose   string
}

// SecretPurposeBudgetRegistry binds process-local budget leases to one
// immutable plugin version and approved purpose. Replacing or removing a
// budget revokes the previous instance immediately. A distributed Runner will
// need a durable shared budget adapter in V12-05/06.
type SecretPurposeBudgetRegistry struct {
	mu      sync.RWMutex
	budgets map[secretBudgetKey]*security.PurposeBudget
}

func NewSecretPurposeBudgetRegistry() *SecretPurposeBudgetRegistry {
	return &SecretPurposeBudgetRegistry{budgets: make(map[secretBudgetKey]*security.PurposeBudget)}
}

func (r *SecretPurposeBudgetRegistry) Register(versionID int64, purpose string, budget *security.PurposeBudget) error {
	if r == nil || versionID <= 0 || budget == nil || budget.Purpose() != purpose {
		return ErrSecretUseDenied
	}
	key := secretBudgetKey{versionID: versionID, purpose: purpose}
	r.mu.Lock()
	if r.budgets == nil {
		r.budgets = make(map[secretBudgetKey]*security.PurposeBudget)
	}
	previous := r.budgets[key]
	r.budgets[key] = budget
	r.mu.Unlock()
	if previous != nil && previous != budget {
		previous.Revoke()
	}
	return nil
}

func (r *SecretPurposeBudgetRegistry) CurrentSecretPurposeBudget(_ context.Context, versionID int64, purpose string) (*security.PurposeBudget, error) {
	if r == nil || versionID <= 0 || purpose == "" {
		return nil, ErrSecretUseDenied
	}
	r.mu.RLock()
	budget := r.budgets[secretBudgetKey{versionID: versionID, purpose: purpose}]
	r.mu.RUnlock()
	if budget == nil || budget.Purpose() != purpose || budget.Snapshot().Revoked {
		return nil, ErrSecretUseDenied
	}
	return budget, nil
}

func (r *SecretPurposeBudgetRegistry) Revoke(versionID int64, purpose string) {
	if r == nil {
		return
	}
	key := secretBudgetKey{versionID: versionID, purpose: purpose}
	r.mu.Lock()
	budget := r.budgets[key]
	delete(r.budgets, key)
	r.mu.Unlock()
	if budget != nil {
		budget.Revoke()
	}
}

var _ SecretPurposeBudgetProvider = (*SecretPurposeBudgetRegistry)(nil)
