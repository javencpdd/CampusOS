package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Admin-domain identity stores (migration 000009). These tables are the only
// authority for admin accounts, credentials and role bindings; they never
// reference users/accounts by foreign key, so a same-valued ID or email in the
// user domain can never merge subjects.
var (
	ErrAdminDomainAccountNotFound   = errors.New("admin account not found")
	ErrAdminDomainAccountConflict   = errors.New("admin account username or email already exists")
	ErrAdminDomainCredentialMissing = errors.New("admin credential not found")
	ErrAdminDomainRoleNotFound      = errors.New("admin role not found")
	ErrAdminDomainBindingNotFound   = errors.New("admin role binding not found")
	ErrAdminDomainBindingConflict   = errors.New("active admin role binding already exists")
	ErrAdminDomainConflict          = errors.New("admin domain version conflict")
	ErrAdminSessionNotFound         = errors.New("admin session not found")
	ErrAdminDomainTOTPNotFound      = errors.New("admin TOTP method not found")
	ErrAdminDomainTicketNotFound    = errors.New("admin ticket not found")
	ErrAdminDomainTicketConflict    = errors.New("admin ticket digest already exists")
)

type AdminDomainAccount struct {
	ID              string     `json:"id"`
	Username        string     `json:"username"`
	Email           string     `json:"email"`
	DisplayName     string     `json:"display_name"`
	Status          string     `json:"status"`
	StatusReason    string     `json:"status_reason,omitempty"`
	StatusChangedBy string     `json:"status_changed_by,omitempty"`
	Version         int64      `json:"version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

type AdminDomainCredential struct {
	AccountID         string    `json:"account_id"`
	CredentialHash    string    `json:"credential_hash"`
	CredentialVersion int64     `json:"credential_version"`
	PasswordChangedAt time.Time `json:"password_changed_at"`
}

type AdminDomainRole struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsSystem    bool   `json:"is_system"`
}

type AdminDomainRoleBinding struct {
	ID        string     `json:"id"`
	AccountID string     `json:"account_id"`
	RoleID    string     `json:"role_id"`
	GrantedBy string     `json:"granted_by"`
	Status    string     `json:"status"`
	Version   int64      `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// AdminDomainRepository is the storage Port of the independent admin identity
// domain. Lifecycle rules (MFA, last security admin, bootstrap gate) stay with
// the service layer; the repository persists validated shapes and CAS only.
type AdminDomainRepository interface {
	CreateAccount(ctx context.Context, account AdminDomainAccount) error
	GetAccountByID(ctx context.Context, id string) (AdminDomainAccount, error)
	GetAccountByUsername(ctx context.Context, username string) (AdminDomainAccount, error)
	SetAccountStatusCAS(ctx context.Context, id string, expectedVersion int64, status, reason, changedBy string, at time.Time) (AdminDomainAccount, bool, error)
	SetCredentialCAS(ctx context.Context, accountID, hash string, expectedVersion int64, at time.Time) (AdminDomainCredential, bool, error)
	GetCredential(ctx context.Context, accountID string) (AdminDomainCredential, error)
	GetRole(ctx context.Context, id string) (AdminDomainRole, error)
	BindRole(ctx context.Context, binding AdminDomainRoleBinding) error
	RevokeBindingCAS(ctx context.Context, id string, expectedVersion int64, at time.Time) (AdminDomainRoleBinding, bool, error)
	ListActiveBindings(ctx context.Context, accountID string) ([]AdminDomainRoleBinding, error)
	CountActiveSecurityAdmins(ctx context.Context) (int, error)
}

func validAdminAccountID(id string) bool { return validDelegationOpaqueID(id) }

func validAdminUsername(name string) bool {
	if len(name) < 2 || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || i > 0 && (ch == '.' || ch == '_' || ch == '-') {
			continue
		}
		return false
	}
	return true
}

func (a AdminDomainAccount) validateShape() error {
	if !validAdminAccountID(a.ID) || !validAdminUsername(a.Username) || !strings.Contains(a.Email, "@") || len(a.Email) > 320 {
		return ErrAdminDomainConflict
	}
	switch a.Status {
	case "active", "suspended", "revoked":
	default:
		return ErrAdminDomainConflict
	}
	if a.Status == "revoked" && a.RevokedAt == nil || a.Status != "revoked" && a.RevokedAt != nil {
		return ErrAdminDomainConflict
	}
	return nil
}

const adminAccountColumns = `id, username, email, display_name, status, status_reason, status_changed_by, status_changed_at, version, created_at, updated_at, revoked_at`

func scanAdminDomainAccount(row pgx.Row) (AdminDomainAccount, error) {
	var a AdminDomainAccount
	var changedBy *string
	var statusChangedAt *time.Time
	err := row.Scan(&a.ID, &a.Username, &a.Email, &a.DisplayName, &a.Status, &a.StatusReason, &changedBy, &statusChangedAt, &a.Version, &a.CreatedAt, &a.UpdatedAt, &a.RevokedAt)
	if err != nil {
		return AdminDomainAccount{}, err
	}
	if changedBy != nil {
		a.StatusChangedBy = *changedBy
	}
	return a, nil
}

// PgAdminDomainRepository is the PostgreSQL adapter of AdminDomainRepository.
type PgAdminDomainRepository struct {
	pool *pgxpool.Pool
}

func NewPgAdminDomainRepository(pool *pgxpool.Pool) *PgAdminDomainRepository {
	return &PgAdminDomainRepository{pool: pool}
}

func (r *PgAdminDomainRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func isAdminUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *PgAdminDomainRepository) CreateAccount(ctx context.Context, account AdminDomainAccount) error {
	account.Status = "active"
	account.Version = 1
	account.RevokedAt = nil
	if err := account.validateShape(); err != nil {
		return err
	}
	_, err := r.db(ctx).Exec(ctx,
		`INSERT INTO admin_accounts (id, username, email, display_name) VALUES ($1,$2,$3,$4)`,
		account.ID, account.Username, account.Email, account.DisplayName)
	if isAdminUniqueViolation(err) {
		return ErrAdminDomainAccountConflict
	}
	return err
}

func (r *PgAdminDomainRepository) GetAccountByID(ctx context.Context, id string) (AdminDomainAccount, error) {
	a, err := scanAdminDomainAccount(r.db(ctx).QueryRow(ctx,
		`SELECT `+adminAccountColumns+` FROM admin_accounts WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminDomainAccount{}, ErrAdminDomainAccountNotFound
		}
		return AdminDomainAccount{}, err
	}
	return a, nil
}

func (r *PgAdminDomainRepository) GetAccountByUsername(ctx context.Context, username string) (AdminDomainAccount, error) {
	a, err := scanAdminDomainAccount(r.db(ctx).QueryRow(ctx,
		`SELECT `+adminAccountColumns+` FROM admin_accounts WHERE lower(username) = lower($1)`, username))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminDomainAccount{}, ErrAdminDomainAccountNotFound
		}
		return AdminDomainAccount{}, err
	}
	return a, nil
}

func (r *PgAdminDomainRepository) SetAccountStatusCAS(ctx context.Context, id string, expectedVersion int64, status, reason, changedBy string, at time.Time) (AdminDomainAccount, bool, error) {
	a, err := scanAdminDomainAccount(r.db(ctx).QueryRow(ctx,
		`UPDATE admin_accounts
		    SET status = $2, version = version + 1, status_reason = $3, status_changed_by = NULLIF($4, ''),
		        status_changed_at = $5, updated_at = $5,
		        revoked_at = CASE WHEN $6 THEN $5 ELSE revoked_at END
		  WHERE id = $1 AND version = $7 AND status <> 'revoked'
		  RETURNING `+adminAccountColumns,
		id, status, reason, changedBy, at, status == "revoked", expectedVersion))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, lookupErr := r.GetAccountByID(ctx, id); errors.Is(lookupErr, ErrAdminDomainAccountNotFound) {
				return AdminDomainAccount{}, false, ErrAdminDomainAccountNotFound
			}
			return AdminDomainAccount{}, false, nil
		}
		return AdminDomainAccount{}, false, err
	}
	return a, true, nil
}

func (r *PgAdminDomainRepository) SetCredentialCAS(ctx context.Context, accountID, hash string, expectedVersion int64, at time.Time) (AdminDomainCredential, bool, error) {
	var c AdminDomainCredential
	err := r.db(ctx).QueryRow(ctx,
		`INSERT INTO admin_credentials (account_id, credential_hash, credential_version, password_changed_at, created_at, updated_at)
		 VALUES ($1,$2,1,$3,$3,$3)
		 ON CONFLICT (account_id) DO UPDATE
		    SET credential_hash = EXCLUDED.credential_hash,
		        credential_version = admin_credentials.credential_version + 1,
		        password_changed_at = EXCLUDED.password_changed_at,
		        updated_at = EXCLUDED.updated_at
		  WHERE admin_credentials.credential_version = $4
		 RETURNING account_id, credential_hash, credential_version, password_changed_at`,
		accountID, hash, at, expectedVersion).Scan(&c.AccountID, &c.CredentialHash, &c.CredentialVersion, &c.PasswordChangedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, lookupErr := r.GetCredential(ctx, accountID); errors.Is(lookupErr, ErrAdminDomainCredentialMissing) {
				return AdminDomainCredential{}, false, ErrAdminDomainCredentialMissing
			}
			return AdminDomainCredential{}, false, nil
		}
		return AdminDomainCredential{}, false, err
	}
	return c, true, nil
}

func (r *PgAdminDomainRepository) GetCredential(ctx context.Context, accountID string) (AdminDomainCredential, error) {
	var c AdminDomainCredential
	err := r.db(ctx).QueryRow(ctx,
		`SELECT account_id, credential_hash, credential_version, password_changed_at FROM admin_credentials WHERE account_id = $1`, accountID).
		Scan(&c.AccountID, &c.CredentialHash, &c.CredentialVersion, &c.PasswordChangedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminDomainCredential{}, ErrAdminDomainCredentialMissing
		}
		return AdminDomainCredential{}, err
	}
	return c, nil
}

func (r *PgAdminDomainRepository) GetRole(ctx context.Context, id string) (AdminDomainRole, error) {
	var role AdminDomainRole
	err := r.db(ctx).QueryRow(ctx,
		`SELECT id, name, description, is_system FROM admin_roles WHERE id = $1`, id).
		Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminDomainRole{}, ErrAdminDomainRoleNotFound
		}
		return AdminDomainRole{}, err
	}
	return role, nil
}

func (r *PgAdminDomainRepository) BindRole(ctx context.Context, binding AdminDomainRoleBinding) error {
	binding.Status = "active"
	binding.Version = 1
	_, err := r.db(ctx).Exec(ctx,
		`INSERT INTO admin_role_bindings (id, account_id, role_id, granted_by) VALUES ($1,$2,$3,$4)`,
		binding.ID, binding.AccountID, binding.RoleID, binding.GrantedBy)
	if isAdminUniqueViolation(err) {
		return ErrAdminDomainBindingConflict
	}
	return err
}

func (r *PgAdminDomainRepository) RevokeBindingCAS(ctx context.Context, id string, expectedVersion int64, at time.Time) (AdminDomainRoleBinding, bool, error) {
	var b AdminDomainRoleBinding
	err := r.db(ctx).QueryRow(ctx,
		`UPDATE admin_role_bindings
		    SET status = 'revoked', version = version + 1, revoked_at = $2
		  WHERE id = $1 AND version = $3 AND status = 'active'
		  RETURNING id, account_id, role_id, granted_by, status, version, created_at, revoked_at`,
		id, at, expectedVersion).
		Scan(&b.ID, &b.AccountID, &b.RoleID, &b.GrantedBy, &b.Status, &b.Version, &b.CreatedAt, &b.RevokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			if lookupErr := r.db(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM admin_role_bindings WHERE id = $1)`, id).Scan(&exists); lookupErr != nil {
				return AdminDomainRoleBinding{}, false, lookupErr
			}
			if !exists {
				return AdminDomainRoleBinding{}, false, ErrAdminDomainBindingNotFound
			}
			return AdminDomainRoleBinding{}, false, nil
		}
		return AdminDomainRoleBinding{}, false, err
	}
	return b, true, nil
}

func (r *PgAdminDomainRepository) ListActiveBindings(ctx context.Context, accountID string) ([]AdminDomainRoleBinding, error) {
	rows, err := r.db(ctx).Query(ctx,
		`SELECT id, account_id, role_id, granted_by, status, version, created_at, revoked_at
		   FROM admin_role_bindings WHERE account_id = $1 AND status = 'active' ORDER BY id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AdminDomainRoleBinding{}
	for rows.Next() {
		var b AdminDomainRoleBinding
		if err := rows.Scan(&b.ID, &b.AccountID, &b.RoleID, &b.GrantedBy, &b.Status, &b.Version, &b.CreatedAt, &b.RevokedAt); err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return items, rows.Err()
}

func (r *PgAdminDomainRepository) CountActiveSecurityAdmins(ctx context.Context) (int, error) {
	var count int
	err := r.db(ctx).QueryRow(ctx,
		`SELECT count(*) FROM admin_role_bindings b
		 INNER JOIN admin_accounts a ON a.id = b.account_id AND a.status = 'active'
		 WHERE b.role_id = 'security_admin' AND b.status = 'active'`).Scan(&count)
	return count, err
}

// MemoryAdminDomainRepository is the deterministic Memory profile adapter with
// the reliability snapshot contract.
type MemoryAdminDomainRepository struct {
	mu          sync.RWMutex
	accounts    map[string]AdminDomainAccount
	credentials map[string]AdminDomainCredential
	roles       map[string]AdminDomainRole
	bindings    map[string]AdminDomainRoleBinding
}

func NewMemoryAdminDomainRepository() *MemoryAdminDomainRepository {
	return &MemoryAdminDomainRepository{
		accounts:    make(map[string]AdminDomainAccount),
		credentials: make(map[string]AdminDomainCredential),
		roles: map[string]AdminDomainRole{
			"security_admin": {ID: "security_admin", Name: "安全管理员", IsSystem: true},
		},
		bindings: make(map[string]AdminDomainRoleBinding),
	}
}

type memoryAdminDomainSnapshot struct {
	Accounts    map[string]AdminDomainAccount     `json:"accounts"`
	Credentials map[string]AdminDomainCredential  `json:"credentials"`
	Roles       map[string]AdminDomainRole        `json:"roles"`
	Bindings    map[string]AdminDomainRoleBinding `json:"bindings"`
}

func (r *MemoryAdminDomainRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	payload, err := json.Marshal(memoryAdminDomainSnapshot{
		Accounts: r.accounts, Credentials: r.credentials, Roles: r.roles, Bindings: r.bindings,
	})
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (r *MemoryAdminDomainRepository) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	state := memoryAdminDomainSnapshot{}
	if err := json.Unmarshal(payload, &state); err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts, r.credentials, r.roles, r.bindings = state.Accounts, state.Credentials, state.Roles, state.Bindings
}

func (r *MemoryAdminDomainRepository) CreateAccount(_ context.Context, account AdminDomainAccount) error {
	account.Status = "active"
	account.Version = 1
	account.RevokedAt = nil
	if err := account.validateShape(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.accounts {
		if strings.EqualFold(existing.Username, account.Username) || strings.EqualFold(existing.Email, account.Email) {
			return ErrAdminDomainAccountConflict
		}
	}
	r.accounts[account.ID] = account
	return nil
}

func (r *MemoryAdminDomainRepository) GetAccountByID(_ context.Context, id string) (AdminDomainAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	account, ok := r.accounts[id]
	if !ok {
		return AdminDomainAccount{}, ErrAdminDomainAccountNotFound
	}
	return account, nil
}

func (r *MemoryAdminDomainRepository) GetAccountByUsername(_ context.Context, username string) (AdminDomainAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, account := range r.accounts {
		if strings.EqualFold(account.Username, username) {
			return account, nil
		}
	}
	return AdminDomainAccount{}, ErrAdminDomainAccountNotFound
}

func (r *MemoryAdminDomainRepository) SetAccountStatusCAS(_ context.Context, id string, expectedVersion int64, status, reason, changedBy string, at time.Time) (AdminDomainAccount, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, ok := r.accounts[id]
	if !ok {
		return AdminDomainAccount{}, false, ErrAdminDomainAccountNotFound
	}
	if account.Status == "revoked" || account.Version != expectedVersion {
		return AdminDomainAccount{}, false, nil
	}
	account.Status = status
	account.StatusReason = reason
	account.StatusChangedBy = changedBy
	account.Version++
	account.UpdatedAt = at
	if status == "revoked" {
		account.RevokedAt = &at
	}
	r.accounts[id] = account
	return account, true, nil
}

func (r *MemoryAdminDomainRepository) SetCredentialCAS(_ context.Context, accountID, hash string, expectedVersion int64, at time.Time) (AdminDomainCredential, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	credential, exists := r.credentials[accountID]
	if !exists {
		if expectedVersion != 0 {
			return AdminDomainCredential{}, false, ErrAdminDomainCredentialMissing
		}
		credential = AdminDomainCredential{AccountID: accountID, CredentialHash: hash, CredentialVersion: 1, PasswordChangedAt: at}
		r.credentials[accountID] = credential
		return credential, true, nil
	}
	if credential.CredentialVersion != expectedVersion {
		return AdminDomainCredential{}, false, nil
	}
	credential.CredentialHash = hash
	credential.CredentialVersion++
	credential.PasswordChangedAt = at
	r.credentials[accountID] = credential
	return credential, true, nil
}

func (r *MemoryAdminDomainRepository) GetCredential(_ context.Context, accountID string) (AdminDomainCredential, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	credential, ok := r.credentials[accountID]
	if !ok {
		return AdminDomainCredential{}, ErrAdminDomainCredentialMissing
	}
	return credential, nil
}

func (r *MemoryAdminDomainRepository) GetRole(_ context.Context, id string) (AdminDomainRole, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	role, ok := r.roles[id]
	if !ok {
		return AdminDomainRole{}, ErrAdminDomainRoleNotFound
	}
	return role, nil
}

func (r *MemoryAdminDomainRepository) BindRole(_ context.Context, binding AdminDomainRoleBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.roles[binding.RoleID]; !ok {
		return ErrAdminDomainRoleNotFound
	}
	for _, existing := range r.bindings {
		if existing.AccountID == binding.AccountID && existing.RoleID == binding.RoleID && existing.Status == "active" {
			return ErrAdminDomainBindingConflict
		}
	}
	binding.Status = "active"
	binding.Version = 1
	r.bindings[binding.ID] = binding
	return nil
}

func (r *MemoryAdminDomainRepository) RevokeBindingCAS(_ context.Context, id string, expectedVersion int64, at time.Time) (AdminDomainRoleBinding, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	binding, ok := r.bindings[id]
	if !ok {
		return AdminDomainRoleBinding{}, false, ErrAdminDomainBindingNotFound
	}
	if binding.Status != "active" || binding.Version != expectedVersion {
		return AdminDomainRoleBinding{}, false, nil
	}
	binding.Status = "revoked"
	binding.Version++
	binding.RevokedAt = &at
	r.bindings[id] = binding
	return binding, true, nil
}

func (r *MemoryAdminDomainRepository) ListActiveBindings(_ context.Context, accountID string) ([]AdminDomainRoleBinding, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := []AdminDomainRoleBinding{}
	for _, binding := range r.bindings {
		if binding.AccountID == accountID && binding.Status == "active" {
			items = append(items, binding)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *MemoryAdminDomainRepository) CountActiveSecurityAdmins(_ context.Context) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, binding := range r.bindings {
		account, ok := r.accounts[binding.AccountID]
		if ok && account.Status == "active" && binding.RoleID == "security_admin" && binding.Status == "active" {
			count++
		}
	}
	return count, nil
}

// AdminSession is one row of admin_sessions: the admin domain's own session
// and refresh family fact.
type AdminSession struct {
	ID                     string     `json:"id"`
	AccountID              string     `json:"account_id"`
	RefreshTokenDigest     string     `json:"refresh_token_digest"`
	TokenFamilyID          string     `json:"token_family_id"`
	RotatedFromID          string     `json:"rotated_from_id,omitempty"`
	RotatedToID            string     `json:"rotated_to_id,omitempty"`
	DeviceName             string     `json:"device_name,omitempty"`
	UserAgent              string     `json:"user_agent,omitempty"`
	IPHash                 string     `json:"ip_hash,omitempty"`
	AuthenticationStrength string     `json:"authentication_strength"`
	MFAAuthenticatedAt     *time.Time `json:"mfa_authenticated_at,omitempty"`
	LastActiveAt           time.Time  `json:"last_active_at"`
	ExpiresAt              time.Time  `json:"expires_at"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	RevokedAt              *time.Time `json:"revoked_at,omitempty"`
	RevokeReason           string     `json:"revoke_reason,omitempty"`
	Version                int64      `json:"version"`
}

// AdminSessionRepository is the storage Port of the admin session store.
type AdminSessionRepository interface {
	CreateSession(ctx context.Context, session AdminSession) error
	GetSessionByID(ctx context.Context, id string) (AdminSession, error)
	GetSessionByRefreshDigestForUpdate(ctx context.Context, digest string) (AdminSession, error)
	RevokeSessionCAS(ctx context.Context, id string, expectedVersion int64, reason string, at time.Time) (AdminSession, bool, error)
	RevokeSessionFamily(ctx context.Context, familyID, reason string, at time.Time) error
	RevokeSessionsForAccount(ctx context.Context, accountID, reason string, at time.Time) error
}

const adminSessionColumns = `id, account_id, refresh_token_digest, token_family_id, rotated_from_id, rotated_to_id, device_name, user_agent, ip_hash, authentication_strength, mfa_authenticated_at, last_active_at, expires_at, created_at, updated_at, revoked_at, revoke_reason, version`

func scanAdminSession(row pgx.Row) (AdminSession, error) {
	var s AdminSession
	var rotatedFrom, rotatedTo *string
	err := row.Scan(&s.ID, &s.AccountID, &s.RefreshTokenDigest, &s.TokenFamilyID, &rotatedFrom, &rotatedTo,
		&s.DeviceName, &s.UserAgent, &s.IPHash, &s.AuthenticationStrength, &s.MFAAuthenticatedAt, &s.LastActiveAt,
		&s.ExpiresAt, &s.CreatedAt, &s.UpdatedAt, &s.RevokedAt, &s.RevokeReason, &s.Version)
	if err != nil {
		return AdminSession{}, err
	}
	if rotatedFrom != nil {
		s.RotatedFromID = *rotatedFrom
	}
	if rotatedTo != nil {
		s.RotatedToID = *rotatedTo
	}
	return s, nil
}

// PgAdminSessionRepository is the PostgreSQL adapter of AdminSessionRepository.
type PgAdminSessionRepository struct {
	pool *pgxpool.Pool
}

func NewPgAdminSessionRepository(pool *pgxpool.Pool) *PgAdminSessionRepository {
	return &PgAdminSessionRepository{pool: pool}
}

func (r *PgAdminSessionRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgAdminSessionRepository) CreateSession(ctx context.Context, session AdminSession) error {
	session.Version = 1
	if session.ID == "" || session.AccountID == "" || session.RefreshTokenDigest == "" || session.TokenFamilyID == "" ||
		(session.AuthenticationStrength != "password" && session.AuthenticationStrength != "mfa") || !session.ExpiresAt.After(session.CreatedAt) {
		return ErrAdminDomainConflict
	}
	_, err := r.db(ctx).Exec(ctx,
		`INSERT INTO admin_sessions (id, account_id, refresh_token_digest, token_family_id, rotated_from_id, device_name, user_agent, ip_hash,
		 authentication_strength, mfa_authenticated_at, last_active_at, expires_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		session.ID, session.AccountID, session.RefreshTokenDigest, session.TokenFamilyID, session.RotatedFromID,
		session.DeviceName, session.UserAgent, session.IPHash, session.AuthenticationStrength, session.MFAAuthenticatedAt,
		session.LastActiveAt, session.ExpiresAt, session.CreatedAt, session.UpdatedAt)
	if isAdminUniqueViolation(err) {
		return ErrAdminDomainConflict
	}
	return err
}

func (r *PgAdminSessionRepository) GetSessionByID(ctx context.Context, id string) (AdminSession, error) {
	s, err := scanAdminSession(r.db(ctx).QueryRow(ctx,
		`SELECT `+adminSessionColumns+` FROM admin_sessions WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminSession{}, ErrAdminSessionNotFound
		}
		return AdminSession{}, err
	}
	return s, nil
}

func (r *PgAdminSessionRepository) GetSessionByRefreshDigestForUpdate(ctx context.Context, digest string) (AdminSession, error) {
	s, err := scanAdminSession(r.db(ctx).QueryRow(ctx,
		`SELECT `+adminSessionColumns+` FROM admin_sessions WHERE refresh_token_digest = $1 FOR UPDATE`, digest))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminSession{}, ErrAdminSessionNotFound
		}
		return AdminSession{}, err
	}
	return s, nil
}

func (r *PgAdminSessionRepository) RevokeSessionCAS(ctx context.Context, id string, expectedVersion int64, reason string, at time.Time) (AdminSession, bool, error) {
	s, err := scanAdminSession(r.db(ctx).QueryRow(ctx,
		`UPDATE admin_sessions
		    SET revoked_at = $2, revoke_reason = $3, version = version + 1, updated_at = $2
		  WHERE id = $1 AND version = $4 AND revoked_at IS NULL
		  RETURNING `+adminSessionColumns,
		id, at, reason, expectedVersion))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, lookupErr := r.GetSessionByID(ctx, id); errors.Is(lookupErr, ErrAdminSessionNotFound) {
				return AdminSession{}, false, ErrAdminSessionNotFound
			}
			return AdminSession{}, false, nil
		}
		return AdminSession{}, false, err
	}
	return s, true, nil
}

func (r *PgAdminSessionRepository) RevokeSessionFamily(ctx context.Context, familyID, reason string, at time.Time) error {
	_, err := r.db(ctx).Exec(ctx,
		`UPDATE admin_sessions
		    SET revoked_at = $1, revoke_reason = $2, version = version + 1, updated_at = $1
		  WHERE token_family_id = $3 AND revoked_at IS NULL`,
		at, reason, familyID)
	return err
}

func (r *PgAdminSessionRepository) RevokeSessionsForAccount(ctx context.Context, accountID, reason string, at time.Time) error {
	_, err := r.db(ctx).Exec(ctx,
		`UPDATE admin_sessions
		    SET revoked_at = $1, revoke_reason = $2, version = version + 1, updated_at = $1
		  WHERE account_id = $3 AND revoked_at IS NULL`,
		at, reason, accountID)
	return err
}

// MemoryAdminSessionRepository is the deterministic Memory profile adapter.
type MemoryAdminSessionRepository struct {
	mu       sync.RWMutex
	sessions map[string]AdminSession
}

func NewMemoryAdminSessionRepository() *MemoryAdminSessionRepository {
	return &MemoryAdminSessionRepository{sessions: make(map[string]AdminSession)}
}

func (r *MemoryAdminSessionRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	payload, err := json.Marshal(r.sessions)
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (r *MemoryAdminSessionRepository) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	sessions := map[string]AdminSession{}
	if err := json.Unmarshal(payload, &sessions); err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = sessions
}

func (r *MemoryAdminSessionRepository) CreateSession(_ context.Context, session AdminSession) error {
	session.Version = 1
	if session.ID == "" || session.AccountID == "" || session.RefreshTokenDigest == "" || session.TokenFamilyID == "" ||
		(session.AuthenticationStrength != "password" && session.AuthenticationStrength != "mfa") {
		return ErrAdminDomainConflict
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.sessions {
		if existing.RefreshTokenDigest == session.RefreshTokenDigest {
			return ErrAdminDomainConflict
		}
	}
	r.sessions[session.ID] = session
	return nil
}

func (r *MemoryAdminSessionRepository) GetSessionByID(_ context.Context, id string) (AdminSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, ok := r.sessions[id]
	if !ok {
		return AdminSession{}, ErrAdminSessionNotFound
	}
	return session, nil
}

func (r *MemoryAdminSessionRepository) GetSessionByRefreshDigestForUpdate(_ context.Context, digest string) (AdminSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, session := range r.sessions {
		if session.RefreshTokenDigest == digest {
			return session, nil
		}
	}
	return AdminSession{}, ErrAdminSessionNotFound
}

func (r *MemoryAdminSessionRepository) RevokeSessionCAS(_ context.Context, id string, expectedVersion int64, reason string, at time.Time) (AdminSession, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[id]
	if !ok {
		return AdminSession{}, false, ErrAdminSessionNotFound
	}
	if session.RevokedAt != nil || session.Version != expectedVersion {
		return AdminSession{}, false, nil
	}
	session.RevokedAt = &at
	session.RevokeReason = reason
	session.Version++
	r.sessions[id] = session
	return session, true, nil
}

func (r *MemoryAdminSessionRepository) RevokeSessionFamily(_ context.Context, familyID, reason string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, session := range r.sessions {
		if session.TokenFamilyID == familyID && session.RevokedAt == nil {
			stamp := at
			session.RevokedAt = &stamp
			session.RevokeReason = reason
			session.Version++
			r.sessions[id] = session
		}
	}
	return nil
}

func (r *MemoryAdminSessionRepository) RevokeSessionsForAccount(_ context.Context, accountID, reason string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, session := range r.sessions {
		if session.AccountID == accountID && session.RevokedAt == nil {
			stamp := at
			session.RevokedAt = &stamp
			session.RevokeReason = reason
			session.Version++
			r.sessions[id] = session
		}
	}
	return nil
}

// AdminTOTPMethod is the admin domain's TOTP secret envelope. Only ciphertext
// and a nonce persist; the key id names the envelope key in deployment config.
type AdminTOTPMethod struct {
	AccountID           string     `json:"account_id"`
	Status              string     `json:"status"`
	KeyID               string     `json:"key_id"`
	Nonce               string     `json:"nonce"`
	Ciphertext          string     `json:"ciphertext"`
	LastAcceptedStep    int64      `json:"last_accepted_step"`
	EnrollmentExpiresAt time.Time  `json:"enrollment_expires_at"`
	ConfirmedAt         *time.Time `json:"confirmed_at,omitempty"`
	DisabledAt          *time.Time `json:"disabled_at,omitempty"`
	Version             int64      `json:"version"`
}

// AdminSecurityTicket is one bounded, single-use challenge or recovery ticket
// in the admin domain.
type AdminSecurityTicket struct {
	ID          string     `json:"id"`
	AccountID   string     `json:"account_id"`
	Audience    string     `json:"audience,omitempty"`
	Purpose     string     `json:"purpose"`
	Digest      string     `json:"ticket_digest"`
	ExpiresAt   time.Time  `json:"expires_at"`
	ConsumedAt  *time.Time `json:"consumed_at,omitempty"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
}

// AdminSecurityRepository is the storage Port of admin MFA and recovery
// stores. Verification and rate policy stay with services; the repository
// persists envelopes and enforces single-use consumption.
type AdminSecurityRepository interface {
	UpsertTOTP(ctx context.Context, method AdminTOTPMethod) (AdminTOTPMethod, error)
	GetTOTP(ctx context.Context, accountID string) (AdminTOTPMethod, error)
	ConfirmTOTPCAS(ctx context.Context, accountID string, expectedVersion int64, at time.Time) (AdminTOTPMethod, bool, error)
	DisableTOTPCAS(ctx context.Context, accountID string, expectedVersion int64, at time.Time) (AdminTOTPMethod, bool, error)
	CreateAdminTicket(ctx context.Context, ticket AdminSecurityTicket) error
	GetAdminTicketForUpdate(ctx context.Context, digest string) (AdminSecurityTicket, error)
	IncrementAdminTicketAttempts(ctx context.Context, id string, at time.Time) (AdminSecurityTicket, error)
	ConsumeAdminTicket(ctx context.Context, id string, at time.Time) (AdminSecurityTicket, bool, error)
	CreateRecoveryTicket(ctx context.Context, ticket AdminSecurityTicket) error
	GetRecoveryTicketForUpdate(ctx context.Context, digest string) (AdminSecurityTicket, error)
	IncrementRecoveryAttempts(ctx context.Context, id string, at time.Time) (AdminSecurityTicket, error)
	ConsumeRecoveryTicket(ctx context.Context, id string, at time.Time) (AdminSecurityTicket, bool, error)
}

func (m AdminTOTPMethod) validateShape() error {
	if m.AccountID == "" || len(m.KeyID) == 0 || len(m.Nonce) < 8 || len(m.Ciphertext) < 16 || m.LastAcceptedStep < 0 {
		return ErrAdminDomainConflict
	}
	switch m.Status {
	case "pending", "active", "disabled":
	default:
		return ErrAdminDomainConflict
	}
	if m.Status == "active" && m.ConfirmedAt == nil {
		return ErrAdminDomainConflict
	}
	return nil
}

const adminTOTPColumns = `account_id, status, key_id, nonce, ciphertext, last_accepted_step, enrollment_expires_at, confirmed_at, disabled_at, version`

func scanAdminTOTP(row pgx.Row) (AdminTOTPMethod, error) {
	var m AdminTOTPMethod
	err := row.Scan(&m.AccountID, &m.Status, &m.KeyID, &m.Nonce, &m.Ciphertext, &m.LastAcceptedStep,
		&m.EnrollmentExpiresAt, &m.ConfirmedAt, &m.DisabledAt, &m.Version)
	return m, err
}

// PgAdminSecurityRepository is the PostgreSQL adapter of AdminSecurityRepository.
type PgAdminSecurityRepository struct {
	pool *pgxpool.Pool
}

func NewPgAdminSecurityRepository(pool *pgxpool.Pool) *PgAdminSecurityRepository {
	return &PgAdminSecurityRepository{pool: pool}
}

func (r *PgAdminSecurityRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgAdminSecurityRepository) UpsertTOTP(ctx context.Context, method AdminTOTPMethod) (AdminTOTPMethod, error) {
	if method.Version == 0 {
		method.Version = 1
	}
	if err := method.validateShape(); err != nil {
		return AdminTOTPMethod{}, err
	}
	m, err := scanAdminTOTP(r.db(ctx).QueryRow(ctx,
		`INSERT INTO admin_mfa_totp (account_id, status, key_id, nonce, ciphertext, last_accepted_step, enrollment_expires_at, confirmed_at, disabled_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW(),NOW())
		 ON CONFLICT (account_id) DO UPDATE
		    SET status = EXCLUDED.status, key_id = EXCLUDED.key_id, nonce = EXCLUDED.nonce,
		        ciphertext = EXCLUDED.ciphertext, last_accepted_step = EXCLUDED.last_accepted_step,
		        enrollment_expires_at = EXCLUDED.enrollment_expires_at, confirmed_at = EXCLUDED.confirmed_at,
		        disabled_at = EXCLUDED.disabled_at, updated_at = NOW(), version = admin_mfa_totp.version + 1
		 RETURNING `+adminTOTPColumns,
		method.AccountID, method.Status, method.KeyID, method.Nonce, method.Ciphertext, method.LastAcceptedStep,
		method.EnrollmentExpiresAt, method.ConfirmedAt, method.DisabledAt))
	if err != nil {
		return AdminTOTPMethod{}, err
	}
	return m, nil
}

func (r *PgAdminSecurityRepository) GetTOTP(ctx context.Context, accountID string) (AdminTOTPMethod, error) {
	m, err := scanAdminTOTP(r.db(ctx).QueryRow(ctx,
		`SELECT `+adminTOTPColumns+` FROM admin_mfa_totp WHERE account_id = $1`, accountID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminTOTPMethod{}, ErrAdminDomainTOTPNotFound
		}
		return AdminTOTPMethod{}, err
	}
	return m, nil
}

func (r *PgAdminSecurityRepository) ConfirmTOTPCAS(ctx context.Context, accountID string, expectedVersion int64, at time.Time) (AdminTOTPMethod, bool, error) {
	m, err := scanAdminTOTP(r.db(ctx).QueryRow(ctx,
		`UPDATE admin_mfa_totp
		    SET status = 'active', confirmed_at = $2, updated_at = $2, version = version + 1
		  WHERE account_id = $1 AND version = $3 AND status = 'pending'
		  RETURNING `+adminTOTPColumns,
		accountID, at, expectedVersion))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return r.totpCASFailure(ctx, accountID)
		}
		return AdminTOTPMethod{}, false, err
	}
	return m, true, nil
}

func (r *PgAdminSecurityRepository) DisableTOTPCAS(ctx context.Context, accountID string, expectedVersion int64, at time.Time) (AdminTOTPMethod, bool, error) {
	m, err := scanAdminTOTP(r.db(ctx).QueryRow(ctx,
		`UPDATE admin_mfa_totp
		    SET status = 'disabled', disabled_at = $2, updated_at = $2, version = version + 1
		  WHERE account_id = $1 AND version = $3 AND status <> 'disabled'
		  RETURNING `+adminTOTPColumns,
		accountID, at, expectedVersion))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return r.totpCASFailure(ctx, accountID)
		}
		return AdminTOTPMethod{}, false, err
	}
	return m, true, nil
}

func (r *PgAdminSecurityRepository) totpCASFailure(ctx context.Context, accountID string) (AdminTOTPMethod, bool, error) {
	if _, err := r.GetTOTP(ctx, accountID); errors.Is(err, ErrAdminDomainTOTPNotFound) {
		return AdminTOTPMethod{}, false, ErrAdminDomainTOTPNotFound
	}
	return AdminTOTPMethod{}, false, nil
}

func adminTicketShape(ticket AdminSecurityTicket) error {
	if ticket.ID == "" || ticket.AccountID == "" || ticket.Purpose == "" ||
		!validTicketDigest(ticket.Digest) || ticket.ExpiresAt.IsZero() ||
		ticket.Attempts < 0 || ticket.MaxAttempts < 1 || ticket.MaxAttempts > 10 {
		return ErrAdminDomainConflict
	}
	return nil
}

func validTicketDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	for i := 0; i < len(digest); i++ {
		ch := digest[i]
		if ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' {
			continue
		}
		return false
	}
	return true
}

const adminTicketColumns = `id, account_id, audience, purpose, ticket_digest, expires_at, consumed_at, attempts, max_attempts`
const adminRecoveryColumns = `id, account_id, purpose, ticket_digest, expires_at, consumed_at, attempts, max_attempts`

func scanAdminTicket(row pgx.Row) (AdminSecurityTicket, error) {
	var t AdminSecurityTicket
	var audience *string
	err := row.Scan(&t.ID, &t.AccountID, &audience, &t.Purpose, &t.Digest, &t.ExpiresAt, &t.ConsumedAt, &t.Attempts, &t.MaxAttempts)
	if audience != nil {
		t.Audience = *audience
	}
	return t, err
}

func scanAdminRecoveryTicket(row pgx.Row) (AdminSecurityTicket, error) {
	var t AdminSecurityTicket
	err := row.Scan(&t.ID, &t.AccountID, &t.Purpose, &t.Digest, &t.ExpiresAt, &t.ConsumedAt, &t.Attempts, &t.MaxAttempts)
	return t, err
}

func (r *PgAdminSecurityRepository) CreateAdminTicket(ctx context.Context, ticket AdminSecurityTicket) error {
	if err := adminTicketShape(ticket); err != nil {
		return err
	}
	_, err := r.db(ctx).Exec(ctx,
		`INSERT INTO admin_mfa_tickets (id, account_id, audience, purpose, ticket_digest, expires_at, created_at, updated_at)
		 VALUES ($1,$2,'admin',$3,$4,$5,NOW(),NOW())`,
		ticket.ID, ticket.AccountID, ticket.Purpose, ticket.Digest, ticket.ExpiresAt)
	if isAdminUniqueViolation(err) {
		return ErrAdminDomainTicketConflict
	}
	return err
}

func (r *PgAdminSecurityRepository) GetAdminTicketForUpdate(ctx context.Context, digest string) (AdminSecurityTicket, error) {
	t, err := scanAdminTicket(r.db(ctx).QueryRow(ctx,
		`SELECT `+adminTicketColumns+` FROM admin_mfa_tickets WHERE ticket_digest = $1 FOR UPDATE`, digest))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminSecurityTicket{}, ErrAdminDomainTicketNotFound
		}
		return AdminSecurityTicket{}, err
	}
	return t, nil
}

func (r *PgAdminSecurityRepository) IncrementAdminTicketAttempts(ctx context.Context, id string, at time.Time) (AdminSecurityTicket, error) {
	t, err := scanAdminTicket(r.db(ctx).QueryRow(ctx,
		`UPDATE admin_mfa_tickets SET attempts = attempts + 1, updated_at = $2 WHERE id = $1
		 RETURNING `+adminTicketColumns, id, at))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminSecurityTicket{}, ErrAdminDomainTicketNotFound
		}
		return AdminSecurityTicket{}, err
	}
	return t, nil
}

func (r *PgAdminSecurityRepository) ConsumeAdminTicket(ctx context.Context, id string, at time.Time) (AdminSecurityTicket, bool, error) {
	t, err := scanAdminTicket(r.db(ctx).QueryRow(ctx,
		`UPDATE admin_mfa_tickets SET consumed_at = $2, updated_at = $2 WHERE id = $1 AND consumed_at IS NULL
		 RETURNING `+adminTicketColumns, id, at))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminSecurityTicket{}, false, nil
		}
		return AdminSecurityTicket{}, false, err
	}
	return t, true, nil
}

func (r *PgAdminSecurityRepository) CreateRecoveryTicket(ctx context.Context, ticket AdminSecurityTicket) error {
	if err := adminTicketShape(ticket); err != nil {
		return err
	}
	_, err := r.db(ctx).Exec(ctx,
		`INSERT INTO admin_recovery_tickets (id, account_id, purpose, ticket_digest, expires_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,NOW(),NOW())`,
		ticket.ID, ticket.AccountID, ticket.Purpose, ticket.Digest, ticket.ExpiresAt)
	if isAdminUniqueViolation(err) {
		return ErrAdminDomainTicketConflict
	}
	return err
}

func (r *PgAdminSecurityRepository) GetRecoveryTicketForUpdate(ctx context.Context, digest string) (AdminSecurityTicket, error) {
	t, err := scanAdminRecoveryTicket(r.db(ctx).QueryRow(ctx,
		`SELECT `+adminRecoveryColumns+` FROM admin_recovery_tickets WHERE ticket_digest = $1 FOR UPDATE`, digest))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminSecurityTicket{}, ErrAdminDomainTicketNotFound
		}
		return AdminSecurityTicket{}, err
	}
	return t, nil
}

func (r *PgAdminSecurityRepository) IncrementRecoveryAttempts(ctx context.Context, id string, at time.Time) (AdminSecurityTicket, error) {
	t, err := scanAdminRecoveryTicket(r.db(ctx).QueryRow(ctx,
		`UPDATE admin_recovery_tickets SET attempts = attempts + 1, updated_at = $2 WHERE id = $1
		 RETURNING `+adminRecoveryColumns, id, at))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminSecurityTicket{}, ErrAdminDomainTicketNotFound
		}
		return AdminSecurityTicket{}, err
	}
	return t, nil
}

func (r *PgAdminSecurityRepository) ConsumeRecoveryTicket(ctx context.Context, id string, at time.Time) (AdminSecurityTicket, bool, error) {
	t, err := scanAdminRecoveryTicket(r.db(ctx).QueryRow(ctx,
		`UPDATE admin_recovery_tickets SET consumed_at = $2, updated_at = $2 WHERE id = $1 AND consumed_at IS NULL
		 RETURNING `+adminRecoveryColumns, id, at))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminSecurityTicket{}, false, nil
		}
		return AdminSecurityTicket{}, false, err
	}
	return t, true, nil
}

// MemoryAdminSecurityRepository is the deterministic Memory profile adapter.
type MemoryAdminSecurityRepository struct {
	mu       sync.RWMutex
	totp     map[string]AdminTOTPMethod
	tickets  map[string]AdminSecurityTicket
	recovery map[string]AdminSecurityTicket
}

func NewMemoryAdminSecurityRepository() *MemoryAdminSecurityRepository {
	return &MemoryAdminSecurityRepository{
		totp: make(map[string]AdminTOTPMethod), tickets: make(map[string]AdminSecurityTicket), recovery: make(map[string]AdminSecurityTicket),
	}
}

func (r *MemoryAdminSecurityRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	payload, err := json.Marshal(struct {
		TOTP     map[string]AdminTOTPMethod
		Tickets  map[string]AdminSecurityTicket
		Recovery map[string]AdminSecurityTicket
	}{r.totp, r.tickets, r.recovery})
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (r *MemoryAdminSecurityRepository) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	state := struct {
		TOTP     map[string]AdminTOTPMethod
		Tickets  map[string]AdminSecurityTicket
		Recovery map[string]AdminSecurityTicket
	}{}
	if err := json.Unmarshal(payload, &state); err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.totp, r.tickets, r.recovery = state.TOTP, state.Tickets, state.Recovery
}

func (r *MemoryAdminSecurityRepository) UpsertTOTP(_ context.Context, method AdminTOTPMethod) (AdminTOTPMethod, error) {
	if method.Version == 0 {
		method.Version = 1
	}
	if err := method.validateShape(); err != nil {
		return AdminTOTPMethod{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.totp[method.AccountID]; ok {
		method.Version = existing.Version + 1
	}
	r.totp[method.AccountID] = method
	return method, nil
}

func (r *MemoryAdminSecurityRepository) GetTOTP(_ context.Context, accountID string) (AdminTOTPMethod, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	method, ok := r.totp[accountID]
	if !ok {
		return AdminTOTPMethod{}, ErrAdminDomainTOTPNotFound
	}
	return method, nil
}

func (r *MemoryAdminSecurityRepository) ConfirmTOTPCAS(_ context.Context, accountID string, expectedVersion int64, at time.Time) (AdminTOTPMethod, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	method, ok := r.totp[accountID]
	if !ok {
		return AdminTOTPMethod{}, false, ErrAdminDomainTOTPNotFound
	}
	if method.Status != "pending" || method.Version != expectedVersion {
		return AdminTOTPMethod{}, false, nil
	}
	method.Status = "active"
	method.ConfirmedAt = &at
	method.Version++
	r.totp[accountID] = method
	return method, true, nil
}

func (r *MemoryAdminSecurityRepository) DisableTOTPCAS(_ context.Context, accountID string, expectedVersion int64, at time.Time) (AdminTOTPMethod, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	method, ok := r.totp[accountID]
	if !ok {
		return AdminTOTPMethod{}, false, ErrAdminDomainTOTPNotFound
	}
	if method.Status == "disabled" || method.Version != expectedVersion {
		return AdminTOTPMethod{}, false, nil
	}
	method.Status = "disabled"
	method.DisabledAt = &at
	method.Version++
	r.totp[accountID] = method
	return method, true, nil
}

func (r *MemoryAdminSecurityRepository) CreateAdminTicket(_ context.Context, ticket AdminSecurityTicket) error {
	if err := adminTicketShape(ticket); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.tickets {
		if existing.Digest == ticket.Digest {
			return ErrAdminDomainTicketConflict
		}
	}
	r.tickets[ticket.ID] = ticket
	return nil
}

func (r *MemoryAdminSecurityRepository) GetAdminTicketForUpdate(_ context.Context, digest string) (AdminSecurityTicket, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ticket := range r.tickets {
		if ticket.Digest == digest {
			return ticket, nil
		}
	}
	return AdminSecurityTicket{}, ErrAdminDomainTicketNotFound
}

func (r *MemoryAdminSecurityRepository) IncrementAdminTicketAttempts(_ context.Context, id string, at time.Time) (AdminSecurityTicket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ticket, ok := r.tickets[id]
	if !ok {
		return AdminSecurityTicket{}, ErrAdminDomainTicketNotFound
	}
	ticket.Attempts++
	r.tickets[id] = ticket
	return ticket, nil
}

func (r *MemoryAdminSecurityRepository) ConsumeAdminTicket(_ context.Context, id string, at time.Time) (AdminSecurityTicket, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ticket, ok := r.tickets[id]
	if !ok {
		return AdminSecurityTicket{}, false, nil
	}
	if ticket.ConsumedAt != nil {
		return AdminSecurityTicket{}, false, nil
	}
	ticket.ConsumedAt = &at
	r.tickets[id] = ticket
	return ticket, true, nil
}

func (r *MemoryAdminSecurityRepository) CreateRecoveryTicket(_ context.Context, ticket AdminSecurityTicket) error {
	if err := adminTicketShape(ticket); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.recovery {
		if existing.Digest == ticket.Digest {
			return ErrAdminDomainTicketConflict
		}
	}
	r.recovery[ticket.ID] = ticket
	return nil
}

func (r *MemoryAdminSecurityRepository) GetRecoveryTicketForUpdate(_ context.Context, digest string) (AdminSecurityTicket, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ticket := range r.recovery {
		if ticket.Digest == digest {
			return ticket, nil
		}
	}
	return AdminSecurityTicket{}, ErrAdminDomainTicketNotFound
}

func (r *MemoryAdminSecurityRepository) IncrementRecoveryAttempts(_ context.Context, id string, at time.Time) (AdminSecurityTicket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ticket, ok := r.recovery[id]
	if !ok {
		return AdminSecurityTicket{}, ErrAdminDomainTicketNotFound
	}
	ticket.Attempts++
	r.recovery[id] = ticket
	return ticket, nil
}

func (r *MemoryAdminSecurityRepository) ConsumeRecoveryTicket(_ context.Context, id string, at time.Time) (AdminSecurityTicket, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ticket, ok := r.recovery[id]
	if !ok {
		return AdminSecurityTicket{}, false, nil
	}
	if ticket.ConsumedAt != nil {
		return AdminSecurityTicket{}, false, nil
	}
	ticket.ConsumedAt = &at
	r.recovery[id] = ticket
	return ticket, true, nil
}
