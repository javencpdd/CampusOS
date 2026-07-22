package repository

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrAdminAccountNotFound          = errors.New("administrator account not found")
	ErrAdminAccountVersionConflict   = errors.New("administrator account version conflict")
	ErrAdminAccountInvalidTransition = errors.New("administrator account status transition is invalid")
	ErrLastActiveAdministrator       = errors.New("cannot suspend the last active administrator account")
)

const (
	AdminAccountStatusActive    = "active"
	AdminAccountStatusSuspended = "suspended"
	AdminAccountStatusRevoked   = "revoked"
)

// AdminAccount is the credential-free management-plane admission projection.
// It deliberately does not expose password, session, MFA, or account-secret
// material. User profile information is joined by the service layer.
type AdminAccount struct {
	ID                  string     `json:"id"`
	UserID              string     `json:"user_id"`
	CredentialAccountID string     `json:"credential_account_id"`
	Status              string     `json:"status"`
	ActivationSource    string     `json:"activation_source"`
	ActivatedAt         *time.Time `json:"activated_at,omitempty"`
	RevokedAt           *time.Time `json:"revoked_at,omitempty"`
	LastAuthenticatedAt *time.Time `json:"last_authenticated_at,omitempty"`
	StatusReason        string     `json:"status_reason,omitempty"`
	StatusChangedBy     string     `json:"status_changed_by,omitempty"`
	StatusChangedAt     *time.Time `json:"status_changed_at,omitempty"`
	Version             int64      `json:"version"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type AdminAccountListFilter struct {
	Status   string
	Page     int
	PageSize int
}

// AdminAccountRepository owns the management-plane admission record. Passwords
// remain inside the credential repository; callers can only check admission,
// synchronize it with an admin role transition, record authentication, or make
// an audited optimistic-concurrency state transition.
type AdminAccountRepository interface {
	IsActive(context.Context, string) (bool, error)
	EnsureActive(context.Context, string, string) (bool, error)
	Revoke(context.Context, string) (bool, error)
	MarkAuthenticated(context.Context, string, time.Time) error
	List(context.Context, AdminAccountListFilter) ([]AdminAccount, int64, error)
	Get(context.Context, string) (*AdminAccount, error)
	Suspend(context.Context, string, int64, string, string, time.Time) (*AdminAccount, error)
	RestoreAdmission(context.Context, string, int64, string, string, time.Time) (*AdminAccount, error)
}

type memoryAdminAccount struct {
	ID                  string
	CredentialAccountID string
	Status              string
	ActivationSource    string
	ActivatedAt         *time.Time
	RevokedAt           *time.Time
	LastAuthenticatedAt *time.Time
	StatusReason        string
	StatusChangedBy     string
	StatusChangedAt     *time.Time
	Version             int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
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
	return ok && account.Status == AdminAccountStatusActive, nil
}

func (r *MemoryAdminAccountRepository) EnsureActive(_ context.Context, userID, source string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	account, exists := r.accounts[userID]
	if exists && account.Status == AdminAccountStatusSuspended {
		return false, nil
	}
	changed := !exists || account.Status != AdminAccountStatusActive
	if !exists {
		account.ID = userID
		account.CredentialAccountID = "memory:" + userID
		account.CreatedAt = now
		account.ActivatedAt = cloneAdminAccountTime(&now)
		account.Version = 1
	}
	account.Status = AdminAccountStatusActive
	account.ActivationSource = strings.TrimSpace(source)
	account.RevokedAt = nil
	account.UpdatedAt = now
	if exists {
		account.Version++
	}
	r.accounts[userID] = account
	return changed, nil
}

func (r *MemoryAdminAccountRepository) Revoke(_ context.Context, userID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[userID]
	if !exists || account.Status == AdminAccountStatusRevoked {
		return false, nil
	}
	now := time.Now().UTC()
	account.Status = AdminAccountStatusRevoked
	account.RevokedAt = cloneAdminAccountTime(&now)
	account.StatusChangedAt = cloneAdminAccountTime(&now)
	account.StatusReason = "admin_role_revoked"
	account.UpdatedAt = now
	account.Version++
	r.accounts[userID] = account
	return true, nil
}

func (r *MemoryAdminAccountRepository) MarkAuthenticated(_ context.Context, userID string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[userID]
	if !exists || account.Status != AdminAccountStatusActive {
		return ErrAdminAccountNotFound
	}
	at = at.UTC()
	account.LastAuthenticatedAt = &at
	account.UpdatedAt = at
	r.accounts[userID] = account
	return nil
}

func (r *MemoryAdminAccountRepository) List(_ context.Context, filter AdminAccountListFilter) ([]AdminAccount, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	status := strings.TrimSpace(filter.Status)
	items := make([]AdminAccount, 0, len(r.accounts))
	for userID, account := range r.accounts {
		if status != "" && account.Status != status {
			continue
		}
		items = append(items, account.memoryView(userID))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].UserID < items[j].UserID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	total := int64(len(items))
	page, pageSize := normalizeAdminAccountPage(filter.Page, filter.PageSize)
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []AdminAccount{}, total, nil
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return append([]AdminAccount(nil), items[start:end]...), total, nil
}

func (r *MemoryAdminAccountRepository) Get(_ context.Context, userID string) (*AdminAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	account, exists := r.accounts[userID]
	if !exists {
		return nil, ErrAdminAccountNotFound
	}
	view := account.memoryView(userID)
	return &view, nil
}

func (r *MemoryAdminAccountRepository) Suspend(_ context.Context, userID string, expectedVersion int64, actorID, reason string, at time.Time) (*AdminAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[userID]
	if !exists {
		return nil, ErrAdminAccountNotFound
	}
	if expectedVersion < 1 || account.Version != expectedVersion {
		return nil, ErrAdminAccountVersionConflict
	}
	if account.Status != AdminAccountStatusActive {
		return nil, ErrAdminAccountInvalidTransition
	}
	active := 0
	for _, candidate := range r.accounts {
		if candidate.Status == AdminAccountStatusActive {
			active++
		}
	}
	if active <= 1 {
		return nil, ErrLastActiveAdministrator
	}
	at = at.UTC()
	account.Status = AdminAccountStatusSuspended
	account.StatusReason = strings.TrimSpace(reason)
	account.StatusChangedBy = strings.TrimSpace(actorID)
	account.StatusChangedAt = cloneAdminAccountTime(&at)
	account.UpdatedAt = at
	account.Version++
	r.accounts[userID] = account
	view := account.memoryView(userID)
	return &view, nil
}

func (r *MemoryAdminAccountRepository) RestoreAdmission(_ context.Context, userID string, expectedVersion int64, actorID, reason string, at time.Time) (*AdminAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[userID]
	if !exists {
		return nil, ErrAdminAccountNotFound
	}
	if expectedVersion < 1 || account.Version != expectedVersion {
		return nil, ErrAdminAccountVersionConflict
	}
	if account.Status != AdminAccountStatusSuspended {
		return nil, ErrAdminAccountInvalidTransition
	}
	at = at.UTC()
	account.Status = AdminAccountStatusActive
	account.ActivationSource = "admin_restore"
	account.StatusReason = strings.TrimSpace(reason)
	account.StatusChangedBy = strings.TrimSpace(actorID)
	account.StatusChangedAt = cloneAdminAccountTime(&at)
	account.UpdatedAt = at
	account.Version++
	r.accounts[userID] = account
	view := account.memoryView(userID)
	return &view, nil
}

func (r *MemoryAdminAccountRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copyAccounts := make(map[string]memoryAdminAccount, len(r.accounts))
	for userID, account := range r.accounts {
		copyAccounts[userID] = cloneMemoryAdminAccount(account)
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
		r.accounts[userID] = cloneMemoryAdminAccount(account)
	}
}

func (account memoryAdminAccount) memoryView(userID string) AdminAccount {
	return AdminAccount{
		ID:                  account.ID,
		UserID:              userID,
		CredentialAccountID: account.CredentialAccountID,
		Status:              account.Status,
		ActivationSource:    account.ActivationSource,
		ActivatedAt:         cloneAdminAccountTime(account.ActivatedAt),
		RevokedAt:           cloneAdminAccountTime(account.RevokedAt),
		LastAuthenticatedAt: cloneAdminAccountTime(account.LastAuthenticatedAt),
		StatusReason:        account.StatusReason,
		StatusChangedBy:     account.StatusChangedBy,
		StatusChangedAt:     cloneAdminAccountTime(account.StatusChangedAt),
		Version:             account.Version,
		CreatedAt:           account.CreatedAt,
		UpdatedAt:           account.UpdatedAt,
	}
}

func cloneMemoryAdminAccount(value memoryAdminAccount) memoryAdminAccount {
	value.ActivatedAt = cloneAdminAccountTime(value.ActivatedAt)
	value.RevokedAt = cloneAdminAccountTime(value.RevokedAt)
	value.LastAuthenticatedAt = cloneAdminAccountTime(value.LastAuthenticatedAt)
	value.StatusChangedAt = cloneAdminAccountTime(value.StatusChangedAt)
	return value
}

func cloneAdminAccountTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func normalizeAdminAccountPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

var _ AdminAccountRepository = (*MemoryAdminAccountRepository)(nil)
