package repository

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrAdminAccountNotFound = errors.New("administrator account not found")

// AdminAccountRepository owns the management-plane admission record. Passwords
// remain inside the credential repository; callers can only check admission,
// synchronize it with an admin role transition, or record a successful login.
type AdminAccountRepository interface {
	IsActive(context.Context, string) (bool, error)
	EnsureActive(context.Context, string, string) (bool, error)
	Revoke(context.Context, string) (bool, error)
	MarkAuthenticated(context.Context, string, time.Time) error
}

type memoryAdminAccount struct {
	Status              string
	ActivationSource    string
	LastAuthenticatedAt *time.Time
}

type MemoryAdminAccountRepository struct {
	mu       sync.RWMutex
	accounts map[string]memoryAdminAccount
}

func NewMemoryAdminAccountRepository() *MemoryAdminAccountRepository {
	return &MemoryAdminAccountRepository{accounts: make(map[string]memoryAdminAccount)}
}

func (r *MemoryAdminAccountRepository) IsActive(_ context.Context, userID string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	account, ok := r.accounts[userID]
	return ok && account.Status == "active", nil
}

func (r *MemoryAdminAccountRepository) EnsureActive(_ context.Context, userID, source string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[userID]
	if exists && account.Status == "suspended" {
		return false, nil
	}
	changed := !exists || account.Status != "active"
	account.Status = "active"
	account.ActivationSource = source
	r.accounts[userID] = account
	return changed, nil
}

func (r *MemoryAdminAccountRepository) Revoke(_ context.Context, userID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[userID]
	if !exists || account.Status == "revoked" {
		return false, nil
	}
	account.Status = "revoked"
	r.accounts[userID] = account
	return true, nil
}

func (r *MemoryAdminAccountRepository) MarkAuthenticated(_ context.Context, userID string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[userID]
	if !exists || account.Status != "active" {
		return ErrAdminAccountNotFound
	}
	at = at.UTC()
	account.LastAuthenticatedAt = &at
	r.accounts[userID] = account
	return nil
}

func (r *MemoryAdminAccountRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copyAccounts := make(map[string]memoryAdminAccount, len(r.accounts))
	for userID, account := range r.accounts {
		copyAccounts[userID] = account
	}
	return copyAccounts
}

func (r *MemoryAdminAccountRepository) Restore(snapshot any) {
	accounts, ok := snapshot.(map[string]memoryAdminAccount)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts = make(map[string]memoryAdminAccount, len(accounts))
	for userID, account := range accounts {
		r.accounts[userID] = account
	}
}
