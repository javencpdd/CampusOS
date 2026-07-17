package repository

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrEmailExists        = errors.New("email already exists")
	ErrUsernameExists     = errors.New("username already exists")
	ErrAccountNotEligible = errors.New("email account is not eligible for this identity transition")
)

// UserRepository 用户仓储接口
type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	GetByID(ctx context.Context, id string) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	Update(ctx context.Context, user *domain.User) error
	List(ctx context.Context, page, pageSize int) ([]*domain.User, int64, error)
}

// EmailAccountReader is intentionally credential-free. It is the repository
// side of Identity's public account-state Port and must not grow password or
// refresh-token fields.
type EmailAccountReader interface {
	GetEmailAccount(context.Context, string) (*domain.EmailAccount, error)
}

// AuthVersionWriter is a narrow credential-boundary capability used by the
// session and recovery commands. It prevents a caller from writing arbitrary
// user fields merely to invalidate active access JWTs.
type AuthVersionWriter interface {
	BumpAuthVersion(context.Context, string) (*domain.User, error)
}

// AccountCredentialMutator is the narrow write boundary used by the v12
// recovery commands. It intentionally contains only audited identity
// transitions; generic callers cannot update an arbitrary credential or email.
type AccountCredentialMutator interface {
	EmailAccountReader
	UpdatePasswordForVerifiedEmail(context.Context, string, string, string) error
	BindVerifiedEmail(context.Context, string, string, string) error
	RecoverAccountWithEmailAndPassword(context.Context, string, string, string, string, string) error
	UpdatePasswordForSystemManagedEmail(context.Context, string, string, string) error
}

// MemoryUserRepository 内存用户仓储（Demo 用）
type MemoryUserRepository struct {
	mu       sync.RWMutex
	users    map[string]*domain.User
	accounts map[string]memoryEmailAccount
}

type memoryEmailAccount struct {
	UserID             string
	Credential         string
	VerificationState  domain.VerificationState
	VerifiedAt         *time.Time
	VerificationSource string
	CredentialVersion  int64
	PasswordChangedAt  *time.Time
}

// NewMemoryUserRepository 创建内存用户仓储
func NewMemoryUserRepository() *MemoryUserRepository {
	return &MemoryUserRepository{
		users:    make(map[string]*domain.User),
		accounts: make(map[string]memoryEmailAccount),
	}
}

func (r *MemoryUserRepository) Create(_ context.Context, user *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查用户名唯一性
	user.Email = domain.NormalizeEmail(user.Email)
	if user.AuthVersion < 1 {
		user.AuthVersion = 1
	}
	for _, u := range r.users {
		if u.Username == user.Username {
			return ErrUsernameExists
		}
		if domain.NormalizeEmail(u.Email) == user.Email {
			return ErrEmailExists
		}
	}

	r.users[user.ID] = cloneUser(user)
	return nil
}

func (r *MemoryUserRepository) GetByID(_ context.Context, id string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, ok := r.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return cloneUser(user), nil
}

func (r *MemoryUserRepository) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	email = domain.NormalizeEmail(email)
	for _, u := range r.users {
		if domain.NormalizeEmail(u.Email) == email {
			return cloneUser(u), nil
		}
	}
	return nil, ErrUserNotFound
}

// CreateVerifiedAccount keeps the local profile subject to the same verified
// registration invariant as PostgreSQL. It is intentionally not a general
// account-management API.
func (r *MemoryUserRepository) CreateVerifiedAccount(_ context.Context, userID, email, credential string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	email = domain.NormalizeEmail(email)
	if _, exists := r.users[userID]; !exists {
		return ErrUserNotFound
	}
	if _, exists := r.accounts[email]; exists {
		return ErrEmailExists
	}
	now := time.Now().UTC()
	r.accounts[email] = memoryEmailAccount{
		UserID:             userID,
		Credential:         credential,
		VerificationState:  domain.VerificationStateVerified,
		VerifiedAt:         &now,
		VerificationSource: "registration_challenge",
		CredentialVersion:  1,
		PasswordChangedAt:  &now,
	}
	return nil
}

func (r *MemoryUserRepository) GetCredentialByEmail(_ context.Context, email string) (string, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	account, exists := r.accounts[domain.NormalizeEmail(email)]
	if !exists {
		return "", "", ErrUserNotFound
	}
	return account.UserID, account.Credential, nil
}

// GetEmailAccount exposes the credential-free account state for memory-mode
// tests and local development. It never makes a direct-created user appear
// verified: only CreateVerifiedAccount creates an account record.
func (r *MemoryUserRepository) GetEmailAccount(ctx context.Context, userID string) (*domain.EmailAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for email, account := range r.accounts {
		if account.UserID != userID {
			continue
		}
		return &domain.EmailAccount{
			ID:                   "memory:" + userID,
			UserID:               userID,
			IdentifierNormalized: email,
			VerificationState:    account.VerificationState,
			VerifiedAt:           cloneTime(account.VerifiedAt),
			VerificationSource:   account.VerificationSource,
			CredentialVersion:    account.CredentialVersion,
			PasswordChangedAt:    cloneTime(account.PasswordChangedAt),
		}, nil
	}
	return nil, ErrUserNotFound
}

func (r *MemoryUserRepository) UpdatePasswordForVerifiedEmail(_ context.Context, userID, email, credential string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	email = domain.NormalizeEmail(email)
	account, exists := r.accounts[email]
	if !exists || account.UserID != userID || account.VerificationState != domain.VerificationStateVerified {
		return ErrAccountNotEligible
	}
	user, exists := r.users[userID]
	if !exists {
		return ErrUserNotFound
	}
	now := time.Now().UTC()
	account.Credential = credential
	if account.CredentialVersion < 1 {
		account.CredentialVersion = 1
	}
	account.CredentialVersion++
	account.PasswordChangedAt = &now
	r.accounts[email] = account
	updated := cloneUser(user)
	updated.MustChangePassword = false
	updated.UpdatedAt = now
	r.users[userID] = updated
	return nil
}

func (r *MemoryUserRepository) BindVerifiedEmail(_ context.Context, userID, email, source string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	email = domain.NormalizeEmail(email)
	if email == "" || domain.IsReservedEmail(email) {
		return ErrAccountNotEligible
	}
	oldEmail, account, found := r.memoryAccountByUserLocked(userID)
	if !found || account.VerificationState == domain.VerificationStateSystemManaged {
		return ErrAccountNotEligible
	}
	if owner, exists := r.accounts[email]; exists && owner.UserID != userID {
		return ErrEmailExists
	}
	for id, user := range r.users {
		if id != userID && domain.NormalizeEmail(user.Email) == email {
			return ErrEmailExists
		}
	}
	user, exists := r.users[userID]
	if !exists {
		return ErrUserNotFound
	}
	now := time.Now().UTC()
	if oldEmail != email {
		delete(r.accounts, oldEmail)
	}
	account.VerificationState = domain.VerificationStateVerified
	account.VerifiedAt = &now
	account.VerificationSource = source
	r.accounts[email] = account
	updated := cloneUser(user)
	updated.Email = email
	updated.UpdatedAt = now
	r.users[userID] = updated
	return nil
}

func (r *MemoryUserRepository) RecoverAccountWithEmailAndPassword(_ context.Context, userID, accountID, email, credential, source string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	email = domain.NormalizeEmail(email)
	if email == "" || domain.IsReservedEmail(email) {
		return ErrAccountNotEligible
	}
	oldEmail, account, found := r.memoryAccountByUserLocked(userID)
	if !found || "memory:"+userID != accountID || (account.VerificationState != domain.VerificationStateLegacyAccepted && account.VerificationState != domain.VerificationStateUnverified) {
		return ErrAccountNotEligible
	}
	if owner, exists := r.accounts[email]; exists && owner.UserID != userID {
		return ErrEmailExists
	}
	for id, user := range r.users {
		if id != userID && domain.NormalizeEmail(user.Email) == email {
			return ErrEmailExists
		}
	}
	user, exists := r.users[userID]
	if !exists {
		return ErrUserNotFound
	}
	now := time.Now().UTC()
	if oldEmail != email {
		delete(r.accounts, oldEmail)
	}
	account.Credential = credential
	if account.CredentialVersion < 1 {
		account.CredentialVersion = 1
	}
	account.CredentialVersion++
	account.VerificationState = domain.VerificationStateVerified
	account.VerifiedAt = &now
	account.VerificationSource = source
	account.PasswordChangedAt = &now
	r.accounts[email] = account
	updated := cloneUser(user)
	updated.Email = email
	updated.MustChangePassword = false
	updated.UpdatedAt = now
	r.users[userID] = updated
	return nil
}

func (r *MemoryUserRepository) UpdatePasswordForSystemManagedEmail(_ context.Context, userID, email, credential string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	email = domain.NormalizeEmail(email)
	account, exists := r.accounts[email]
	if !exists || account.UserID != userID || account.VerificationState != domain.VerificationStateSystemManaged {
		return ErrAccountNotEligible
	}
	user, exists := r.users[userID]
	if !exists {
		return ErrUserNotFound
	}
	now := time.Now().UTC()
	account.Credential = credential
	if account.CredentialVersion < 1 {
		account.CredentialVersion = 1
	}
	account.CredentialVersion++
	account.PasswordChangedAt = &now
	r.accounts[email] = account
	updated := cloneUser(user)
	updated.MustChangePassword = false
	updated.UpdatedAt = now
	r.users[userID] = updated
	return nil
}

func (r *MemoryUserRepository) memoryAccountByUserLocked(userID string) (string, memoryEmailAccount, bool) {
	for email, account := range r.accounts {
		if account.UserID == userID {
			return email, account, true
		}
	}
	return "", memoryEmailAccount{}, false
}

func (r *MemoryUserRepository) GetByUsername(_ context.Context, username string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, u := range r.users {
		if u.Username == username {
			return cloneUser(u), nil
		}
	}
	return nil, ErrUserNotFound
}

func (r *MemoryUserRepository) Update(_ context.Context, user *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.users[user.ID]; !ok {
		return ErrUserNotFound
	}
	r.users[user.ID] = cloneUser(user)
	return nil
}

func (r *MemoryUserRepository) BumpAuthVersion(_ context.Context, id string) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, ok := r.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	copy := cloneUser(user)
	if copy.AuthVersion < 1 {
		copy.AuthVersion = 1
	}
	copy.AuthVersion++
	copy.UpdatedAt = time.Now().UTC()
	r.users[id] = cloneUser(copy)
	return copy, nil
}

func (r *MemoryUserRepository) List(_ context.Context, page, pageSize int) ([]*domain.User, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := int64(len(r.users))

	// 简单分页
	all := make([]*domain.User, 0, len(r.users))
	for _, u := range r.users {
		all = append(all, cloneUser(u))
	}

	start := (page - 1) * pageSize
	if start >= len(all) {
		return []*domain.User{}, total, nil
	}

	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}

	return all[start:end], total, nil
}

// DeleteForRegistration is deliberately narrow: it only supports compensating
// a just-created user when a non-transactional local test adapter fails while
// creating the associated account. Production PostgreSQL uses TxKernel.
func (r *MemoryUserRepository) DeleteForRegistration(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.users[id]; !exists {
		return ErrUserNotFound
	}
	for email, account := range r.accounts {
		if account.UserID == id {
			delete(r.accounts, email)
		}
	}
	delete(r.users, id)
	return nil
}

func (r *MemoryUserRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make(map[string]*domain.User, len(r.users))
	for id, user := range r.users {
		items[id] = cloneUser(user)
	}
	accounts := make(map[string]memoryEmailAccount, len(r.accounts))
	for email, account := range r.accounts {
		account.VerifiedAt = cloneTime(account.VerifiedAt)
		account.PasswordChangedAt = cloneTime(account.PasswordChangedAt)
		accounts[email] = account
	}
	return memoryUserSnapshot{Users: items, Accounts: accounts}
}

func (r *MemoryUserRepository) Restore(value any) {
	snapshot, ok := value.(memoryUserSnapshot)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users = make(map[string]*domain.User, len(snapshot.Users))
	for id, user := range snapshot.Users {
		r.users[id] = cloneUser(user)
	}
	r.accounts = make(map[string]memoryEmailAccount, len(snapshot.Accounts))
	for email, account := range snapshot.Accounts {
		account.VerifiedAt = cloneTime(account.VerifiedAt)
		account.PasswordChangedAt = cloneTime(account.PasswordChangedAt)
		r.accounts[email] = account
	}
}

type memoryUserSnapshot struct {
	Users    map[string]*domain.User
	Accounts map[string]memoryEmailAccount
}

func cloneUser(user *domain.User) *domain.User {
	if user == nil {
		return nil
	}
	copyUser := *user
	return &copyUser
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

var _ AccountCredentialMutator = (*MemoryUserRepository)(nil)
