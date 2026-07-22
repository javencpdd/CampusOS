package repository

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/domain"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var (
	ErrMFAMethodNotFound = errors.New("identity MFA method not found")
	ErrMFATicketNotFound = errors.New("identity MFA ticket not found")
	ErrMFAPolicyNotFound = errors.New("identity MFA policy not found")
	ErrMFAPolicyConflict = errors.New("identity MFA policy version conflict")
)

const (
	MFAMethodPending  = "pending"
	MFAMethodActive   = "active"
	MFAMethodDisabled = "disabled"
)

// MFATOTPMethod is intentionally repository-internal. Ciphertext is the only
// persisted TOTP material; plaintext keys never cross this boundary.
type MFATOTPMethod struct {
	ID                  string
	UserID              string
	State               string
	KeyID               string
	Nonce               string
	Ciphertext          string
	LastAcceptedStep    int64
	EnrollmentExpiresAt time.Time
	ConfirmedAt         *time.Time
	DisabledAt          *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type MFATicket struct {
	ID           string
	UserID       string
	Audience     domain.MFAAudience
	Purpose      domain.MFATicketPurpose
	TicketDigest string
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
	Attempts     int
	MaxAttempts  int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type MFARecoveryCode struct {
	ID        string
	UserID    string
	MethodID  string
	Digest    string
	UsedAt    *time.Time
	CreatedAt time.Time
}

// MFARepository owns encrypted factor material, one-time ticket digests and
// recovery-code digests. Raw TOTP keys, raw tickets and raw recovery codes are
// never accepted by this contract.
type MFARepository interface {
	CreatePendingTOTP(context.Context, *MFATOTPMethod) error
	GetPendingTOTPForUpdate(context.Context, string) (*MFATOTPMethod, error)
	GetActiveTOTP(context.Context, string) (*MFATOTPMethod, error)
	GetActiveTOTPForUpdate(context.Context, string) (*MFATOTPMethod, error)
	ActivatePendingTOTP(context.Context, string, string, int64, time.Time) error
	DisableActiveTOTP(context.Context, string, time.Time) error
	AcceptTOTPStep(context.Context, string, int64, time.Time) (bool, error)
	CreateTicket(context.Context, *MFATicket) error
	GetTicketForUpdate(context.Context, string) (*MFATicket, error)
	MarkTicketConsumed(context.Context, string, time.Time) (bool, error)
	RecordTicketFailure(context.Context, string, time.Time) (bool, error)
	ReplaceRecoveryCodes(context.Context, string, string, []MFARecoveryCode) error
	ConsumeRecoveryCode(context.Context, string, string, time.Time) (bool, error)
	CountRecoveryCodes(context.Context, string) (int, error)
	GetPolicy(context.Context) (*domain.MFAPolicy, error)
	UpdatePolicy(context.Context, domain.MFAPolicy, int64) (*domain.MFAPolicy, error)
	AdminCoverage(context.Context) (domain.MFAAdminCoverage, error)
}

// MemoryMFARepository is an isolated compatibility adapter for memory
// profiles and unit tests. Its mutation methods perform their own guarded
// updates so ticket consumption and TOTP-step replay remain deterministic even
// when callers run without PostgreSQL row locks.
type MemoryMFARepository struct {
	mu            sync.RWMutex
	methods       map[string]*MFATOTPMethod
	tickets       map[string]*MFATicket
	recoveryCodes map[string]*MFARecoveryCode
	policy        domain.MFAPolicy
	coverage      domain.MFAAdminCoverage
}

func NewMemoryMFARepository() *MemoryMFARepository {
	return &MemoryMFARepository{
		methods:       make(map[string]*MFATOTPMethod),
		tickets:       make(map[string]*MFATicket),
		recoveryCodes: make(map[string]*MFARecoveryCode),
		policy:        domain.MFAPolicy{ID: "admin", Mode: domain.MFAPolicyOff, Version: 1, UpdatedAt: time.Now().UTC()},
	}
}

func (r *MemoryMFARepository) CreatePendingTOTP(_ context.Context, method *MFATOTPMethod) error {
	if method == nil || method.ID == "" || method.UserID == "" || method.KeyID == "" || method.Nonce == "" || method.Ciphertext == "" {
		return errors.New("identity pending MFA method is incomplete")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, item := range r.methods {
		if item.UserID == method.UserID && item.State == MFAMethodPending {
			delete(r.methods, id)
		}
	}
	r.methods[method.ID] = cloneMFATOTPMethod(method)
	return nil
}

func (r *MemoryMFARepository) GetPendingTOTPForUpdate(_ context.Context, userID string) (*MFATOTPMethod, error) {
	return r.methodByUser(userID, MFAMethodPending)
}

func (r *MemoryMFARepository) GetActiveTOTP(_ context.Context, userID string) (*MFATOTPMethod, error) {
	return r.methodByUser(userID, MFAMethodActive)
}

func (r *MemoryMFARepository) GetActiveTOTPForUpdate(_ context.Context, userID string) (*MFATOTPMethod, error) {
	return r.methodByUser(userID, MFAMethodActive)
}

func (r *MemoryMFARepository) methodByUser(userID, state string) (*MFATOTPMethod, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, item := range r.methods {
		if item.UserID == userID && item.State == state {
			return cloneMFATOTPMethod(item), nil
		}
	}
	return nil, ErrMFAMethodNotFound
}

func (r *MemoryMFARepository) ActivatePendingTOTP(_ context.Context, userID, methodID string, step int64, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	pending, ok := r.methods[methodID]
	if !ok || pending.UserID != userID || pending.State != MFAMethodPending || !at.Before(pending.EnrollmentExpiresAt) {
		return ErrMFAMethodNotFound
	}
	for _, item := range r.methods {
		if item.UserID == userID && item.State == MFAMethodActive {
			stamp := at.UTC()
			item.State = MFAMethodDisabled
			item.DisabledAt = &stamp
			item.UpdatedAt = stamp
		}
	}
	stamp := at.UTC()
	pending.State = MFAMethodActive
	pending.LastAcceptedStep = step
	pending.ConfirmedAt = &stamp
	pending.UpdatedAt = stamp
	return nil
}

func (r *MemoryMFARepository) DisableActiveTOTP(_ context.Context, userID string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.methods {
		if item.UserID == userID && item.State == MFAMethodActive {
			stamp := at.UTC()
			item.State = MFAMethodDisabled
			item.DisabledAt = &stamp
			item.UpdatedAt = stamp
			return nil
		}
	}
	return ErrMFAMethodNotFound
}

func (r *MemoryMFARepository) AcceptTOTPStep(_ context.Context, methodID string, step int64, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	method, ok := r.methods[methodID]
	if !ok || method.State != MFAMethodActive {
		return false, ErrMFAMethodNotFound
	}
	if step <= method.LastAcceptedStep {
		return false, nil
	}
	method.LastAcceptedStep = step
	method.UpdatedAt = at.UTC()
	return true, nil
}

func (r *MemoryMFARepository) CreateTicket(_ context.Context, ticket *MFATicket) error {
	if ticket == nil || ticket.ID == "" || ticket.UserID == "" || ticket.TicketDigest == "" || ticket.MaxAttempts < 1 {
		return errors.New("identity MFA ticket is incomplete")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tickets[ticket.TicketDigest]; exists {
		return errors.New("identity MFA ticket digest already exists")
	}
	r.tickets[ticket.TicketDigest] = cloneMFATicket(ticket)
	return nil
}

func (r *MemoryMFARepository) GetTicketForUpdate(_ context.Context, digest string) (*MFATicket, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ticket, ok := r.tickets[digest]
	if !ok {
		return nil, ErrMFATicketNotFound
	}
	return cloneMFATicket(ticket), nil
}

func (r *MemoryMFARepository) MarkTicketConsumed(_ context.Context, digest string, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ticket, ok := r.tickets[digest]
	if !ok {
		return false, ErrMFATicketNotFound
	}
	if ticket.ConsumedAt != nil {
		return false, nil
	}
	stamp := at.UTC()
	ticket.ConsumedAt = &stamp
	ticket.UpdatedAt = stamp
	return true, nil
}

func (r *MemoryMFARepository) RecordTicketFailure(_ context.Context, digest string, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ticket, ok := r.tickets[digest]
	if !ok {
		return true, ErrMFATicketNotFound
	}
	if ticket.ConsumedAt != nil {
		return true, nil
	}
	ticket.Attempts++
	ticket.UpdatedAt = at.UTC()
	blocked := ticket.Attempts >= ticket.MaxAttempts
	if blocked {
		stamp := at.UTC()
		ticket.ConsumedAt = &stamp
	}
	return blocked, nil
}

func (r *MemoryMFARepository) ReplaceRecoveryCodes(_ context.Context, userID, methodID string, codes []MFARecoveryCode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, code := range r.recoveryCodes {
		if code.UserID == userID {
			delete(r.recoveryCodes, id)
		}
	}
	for _, code := range codes {
		if code.ID == "" || code.UserID != userID || code.MethodID != methodID || code.Digest == "" {
			return errors.New("identity MFA recovery code is incomplete")
		}
		r.recoveryCodes[code.ID] = cloneMFARecoveryCode(&code)
	}
	return nil
}

func (r *MemoryMFARepository) ConsumeRecoveryCode(_ context.Context, userID, digest string, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, code := range r.recoveryCodes {
		if code.UserID == userID && code.Digest == digest && code.UsedAt == nil {
			stamp := at.UTC()
			code.UsedAt = &stamp
			return true, nil
		}
	}
	return false, nil
}

func (r *MemoryMFARepository) CountRecoveryCodes(_ context.Context, userID string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, code := range r.recoveryCodes {
		if code.UserID == userID && code.UsedAt == nil {
			count++
		}
	}
	return count, nil
}

func (r *MemoryMFARepository) GetPolicy(_ context.Context) (*domain.MFAPolicy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneMFAPolicy(&r.policy), nil
}

func (r *MemoryMFARepository) UpdatePolicy(_ context.Context, policy domain.MFAPolicy, expectedVersion int64) (*domain.MFAPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.policy.Version != expectedVersion {
		return nil, ErrMFAPolicyConflict
	}
	policy.Version = r.policy.Version + 1
	policy.ID = "admin"
	policy.UpdatedAt = policy.UpdatedAt.UTC()
	r.policy = *cloneMFAPolicy(&policy)
	return cloneMFAPolicy(&r.policy), nil
}

func (r *MemoryMFARepository) AdminCoverage(_ context.Context) (domain.MFAAdminCoverage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.coverage, nil
}

// SetAdminCoverage is intentionally test/profile-only. PostgreSQL derives this
// aggregate from active admission records and active global admin roles.
func (r *MemoryMFARepository) SetAdminCoverage(coverage domain.MFAAdminCoverage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.coverage = coverage
}

func (r *MemoryMFARepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := memoryMFASnapshot{
		methods: make(map[string]*MFATOTPMethod, len(r.methods)), tickets: make(map[string]*MFATicket, len(r.tickets)),
		recoveryCodes: make(map[string]*MFARecoveryCode, len(r.recoveryCodes)), policy: *cloneMFAPolicy(&r.policy), coverage: r.coverage,
	}
	for id, method := range r.methods {
		state.methods[id] = cloneMFATOTPMethod(method)
	}
	for digest, ticket := range r.tickets {
		state.tickets[digest] = cloneMFATicket(ticket)
	}
	for id, code := range r.recoveryCodes {
		state.recoveryCodes[id] = cloneMFARecoveryCode(code)
	}
	return state
}

func (r *MemoryMFARepository) Restore(value any) {
	state, ok := value.(memoryMFASnapshot)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methods = make(map[string]*MFATOTPMethod, len(state.methods))
	r.tickets = make(map[string]*MFATicket, len(state.tickets))
	r.recoveryCodes = make(map[string]*MFARecoveryCode, len(state.recoveryCodes))
	for id, method := range state.methods {
		r.methods[id] = cloneMFATOTPMethod(method)
	}
	for digest, ticket := range state.tickets {
		r.tickets[digest] = cloneMFATicket(ticket)
	}
	for id, code := range state.recoveryCodes {
		r.recoveryCodes[id] = cloneMFARecoveryCode(code)
	}
	r.policy = *cloneMFAPolicy(&state.policy)
	r.coverage = state.coverage
}

type memoryMFASnapshot struct {
	methods       map[string]*MFATOTPMethod
	tickets       map[string]*MFATicket
	recoveryCodes map[string]*MFARecoveryCode
	policy        domain.MFAPolicy
	coverage      domain.MFAAdminCoverage
}

func cloneMFATOTPMethod(value *MFATOTPMethod) *MFATOTPMethod {
	if value == nil {
		return nil
	}
	copy := *value
	if value.ConfirmedAt != nil {
		stamp := *value.ConfirmedAt
		copy.ConfirmedAt = &stamp
	}
	if value.DisabledAt != nil {
		stamp := *value.DisabledAt
		copy.DisabledAt = &stamp
	}
	return &copy
}

func cloneMFATicket(value *MFATicket) *MFATicket {
	if value == nil {
		return nil
	}
	copy := *value
	if value.ConsumedAt != nil {
		stamp := *value.ConsumedAt
		copy.ConsumedAt = &stamp
	}
	return &copy
}

func cloneMFARecoveryCode(value *MFARecoveryCode) *MFARecoveryCode {
	if value == nil {
		return nil
	}
	copy := *value
	if value.UsedAt != nil {
		stamp := *value.UsedAt
		copy.UsedAt = &stamp
	}
	return &copy
}

func cloneMFAPolicy(value *domain.MFAPolicy) *domain.MFAPolicy {
	if value == nil {
		return nil
	}
	copy := *value
	if value.GraceEndsAt != nil {
		stamp := *value.GraceEndsAt
		copy.GraceEndsAt = &stamp
	}
	return &copy
}

// sortedRecoveryCodes is used only by tests and diagnostic-free in-memory
// inspection. It does not expose raw values or digests outside this package.
func sortedRecoveryCodes(values map[string]*MFARecoveryCode) []*MFARecoveryCode {
	items := make([]*MFARecoveryCode, 0, len(values))
	for _, value := range values {
		items = append(items, cloneMFARecoveryCode(value))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

var _ MFARepository = (*MemoryMFARepository)(nil)
var _ transaction.Snapshotter = (*MemoryMFARepository)(nil)
