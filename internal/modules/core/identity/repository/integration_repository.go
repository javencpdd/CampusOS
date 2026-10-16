package repository

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/modules/core/identity/integrationmodel"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var (
	ErrIntegrationNotFound         = errors.New("integration record not found")
	ErrIntegrationConflict         = errors.New("integration record conflict")
	ErrIntegrationStoreUnavailable = errors.New("integration store unavailable")
)

// These records belong exclusively to Identity. Digest never participates in
// JSON projections. IDs do not reference users, admin sessions or plugin keys.
type IntegrationPrincipalRecord struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	Version          int64     `json:"version,string"`
	CreatedByAdminID string    `json:"created_by_admin_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type IntegrationCredentialRecord struct {
	ID               string                  `json:"id"`
	PrincipalID      string                  `json:"principal_id"`
	Prefix           string                  `json:"prefix"`
	Digest           [32]byte                `json:"-"`
	Purpose          string                  `json:"purpose"`
	Status           string                  `json:"status"`
	Rules            []integrationmodel.Rule `json:"rules"`
	NotBeforeMS      int64                   `json:"not_before_ms"`
	ExpiresAtMS      int64                   `json:"expires_at_ms"`
	Version          int64                   `json:"version,string"`
	CreatedByAdminID string                  `json:"created_by_admin_id"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
}

type IntegrationRepository interface {
	CreateIntegrationPrincipal(context.Context, *IntegrationPrincipalRecord) error
	GetIntegrationPrincipal(context.Context, string, bool) (IntegrationPrincipalRecord, error)
	ListIntegrationPrincipals(context.Context, string, int) ([]IntegrationPrincipalRecord, error)
	UpdateIntegrationPrincipal(context.Context, *IntegrationPrincipalRecord, int64) (bool, error)
	CreateIntegrationCredential(context.Context, *IntegrationCredentialRecord) error
	GetIntegrationCredential(context.Context, string, bool) (IntegrationCredentialRecord, error)
	ListIntegrationCredentials(context.Context, string, string, int) ([]IntegrationCredentialRecord, error)
	UpdateIntegrationCredential(context.Context, *IntegrationCredentialRecord, int64) (bool, error)
}

func CloneIntegrationCredential(c IntegrationCredentialRecord) IntegrationCredentialRecord {
	if c.Rules != nil {
		rules := make([]integrationmodel.Rule, len(c.Rules))
		copy(rules, c.Rules)
		for i := range rules {
			rules[i].ResourceIDs = append([]string(nil), rules[i].ResourceIDs...)
		}
		c.Rules = rules
	}
	return c
}
func validIntegrationPrincipal(p IntegrationPrincipalRecord) bool {
	return validDelegationOpaqueID(p.ID) && len(p.Name) > 0 && len(p.Name) <= 120 && validDelegationOpaqueID(p.CreatedByAdminID) && (p.Status == "active" || p.Status == "suspended" || p.Status == "revoked")
}
func validIntegrationCredential(c IntegrationCredentialRecord) bool {
	if !validDelegationOpaqueID(c.ID) || !validDelegationOpaqueID(c.PrincipalID) || !validDelegationOpaqueID(c.CreatedByAdminID) || c.Prefix != "cint1_"+c.ID || len(c.Prefix) != 22 || c.Purpose != "integration_api" || (c.Status != "active" && c.Status != "revoked") || c.Digest == ([32]byte{}) || c.NotBeforeMS < 0 || c.ExpiresAtMS <= c.NotBeforeMS || c.ExpiresAtMS > 9007199254740991 {
		return false
	}
	return integrationmodel.ValidateRules(c.Rules) == nil
}
func integrationLock(ctx context.Context, lock bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if lock && !transaction.Active(ctx) {
		return ErrIntegrationStoreUnavailable
	}
	return nil
}
func integrationLimit(n int) int {
	if n < 1 || n > 100 {
		return 100
	}
	return n
}

type MemoryIntegrationRepository struct {
	mu          sync.RWMutex
	principals  map[string]IntegrationPrincipalRecord
	credentials map[string]IntegrationCredentialRecord
	highWater   int64
}

func NewMemoryIntegrationRepository() *MemoryIntegrationRepository {
	return &MemoryIntegrationRepository{principals: map[string]IntegrationPrincipalRecord{}, credentials: map[string]IntegrationCredentialRecord{}}
}
func (r *MemoryIntegrationRepository) advance() error {
	if r.highWater == math.MaxInt64 {
		return ErrIntegrationStoreUnavailable
	}
	r.highWater++
	return nil
}
func (r *MemoryIntegrationRepository) CreateIntegrationPrincipal(ctx context.Context, p *IntegrationPrincipalRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || !validIntegrationPrincipal(*p) {
		return ErrIntegrationConflict
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.principals[p.ID]; ok {
		return ErrIntegrationConflict
	}
	if err := r.advance(); err != nil {
		return err
	}
	p.Version = r.highWater
	p.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	p.UpdatedAt = p.CreatedAt
	r.principals[p.ID] = *p
	return nil
}
func (r *MemoryIntegrationRepository) GetIntegrationPrincipal(ctx context.Context, id string, lock bool) (IntegrationPrincipalRecord, error) {
	if err := integrationLock(ctx, lock); err != nil {
		return IntegrationPrincipalRecord{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.principals[id]
	if !ok {
		return p, ErrIntegrationNotFound
	}
	return p, nil
}
func (r *MemoryIntegrationRepository) ListIntegrationPrincipals(ctx context.Context, after string, n int) ([]IntegrationPrincipalRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []IntegrationPrincipalRecord{}
	for _, p := range r.principals {
		if p.ID > after {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > integrationLimit(n) {
		out = out[:integrationLimit(n)]
	}
	return out, nil
}
func (r *MemoryIntegrationRepository) UpdateIntegrationPrincipal(ctx context.Context, p *IntegrationPrincipalRecord, version int64) (bool, error) {
	if p == nil || !validIntegrationPrincipal(*p) {
		return false, ErrIntegrationConflict
	}
	if err := integrationLock(ctx, true); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.principals[p.ID]
	if !ok || old.Version != version {
		return false, nil
	}
	if old.Status == "revoked" || p.CreatedByAdminID != old.CreatedByAdminID || !p.CreatedAt.Equal(old.CreatedAt) {
		return false, ErrIntegrationConflict
	}
	if err := r.advance(); err != nil {
		return false, err
	}
	p.Version = r.highWater
	p.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	r.principals[p.ID] = *p
	return true, nil
}
func (r *MemoryIntegrationRepository) CreateIntegrationCredential(ctx context.Context, c *IntegrationCredentialRecord) error {
	if c == nil || !validIntegrationCredential(*c) {
		return ErrIntegrationConflict
	}
	if err := integrationLock(ctx, true); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.credentials[c.ID]; ok {
		return ErrIntegrationConflict
	}
	for _, existing := range r.credentials {
		if existing.Digest == c.Digest {
			return ErrIntegrationConflict
		}
	}
	p, ok := r.principals[c.PrincipalID]
	if !ok || p.Status != "active" {
		return ErrIntegrationConflict
	}
	if err := r.advance(); err != nil {
		return err
	}
	c.Version = r.highWater
	c.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	c.UpdatedAt = c.CreatedAt
	r.credentials[c.ID] = CloneIntegrationCredential(*c)
	return nil
}
func (r *MemoryIntegrationRepository) GetIntegrationCredential(ctx context.Context, id string, lock bool) (IntegrationCredentialRecord, error) {
	if err := integrationLock(ctx, lock); err != nil {
		return IntegrationCredentialRecord{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.credentials[id]
	if !ok {
		return c, ErrIntegrationNotFound
	}
	return CloneIntegrationCredential(c), nil
}
func (r *MemoryIntegrationRepository) ListIntegrationCredentials(ctx context.Context, pid, after string, n int) ([]IntegrationCredentialRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []IntegrationCredentialRecord{}
	for _, c := range r.credentials {
		if c.PrincipalID == pid && c.ID > after {
			out = append(out, CloneIntegrationCredential(c))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > integrationLimit(n) {
		out = out[:integrationLimit(n)]
	}
	return out, nil
}
func (r *MemoryIntegrationRepository) UpdateIntegrationCredential(ctx context.Context, c *IntegrationCredentialRecord, version int64) (bool, error) {
	if c == nil || !validIntegrationCredential(*c) {
		return false, ErrIntegrationConflict
	}
	if err := integrationLock(ctx, true); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.credentials[c.ID]
	if !ok || old.Version != version {
		return false, nil
	}
	if old.Status != "active" || c.PrincipalID != old.PrincipalID || c.Prefix != old.Prefix || c.Digest != old.Digest || c.CreatedByAdminID != old.CreatedByAdminID || !c.CreatedAt.Equal(old.CreatedAt) || c.NotBeforeMS != old.NotBeforeMS || c.ExpiresAtMS != old.ExpiresAtMS {
		return false, ErrIntegrationConflict
	}
	if err := r.advance(); err != nil {
		return false, err
	}
	c.Version = r.highWater
	c.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	r.credentials[c.ID] = CloneIntegrationCredential(*c)
	return true, nil
}

type integrationSnapshot struct {
	Principals  map[string]IntegrationPrincipalRecord
	Credentials map[string]IntegrationCredentialRecord
}

func (r *MemoryIntegrationRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := integrationSnapshot{map[string]IntegrationPrincipalRecord{}, map[string]IntegrationCredentialRecord{}}
	for id, p := range r.principals {
		s.Principals[id] = p
	}
	for id, c := range r.credentials {
		s.Credentials[id] = CloneIntegrationCredential(c)
	}
	return s
}
func (r *MemoryIntegrationRepository) Restore(v any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := v.(integrationSnapshot)
	r.principals = map[string]IntegrationPrincipalRecord{}
	r.credentials = map[string]IntegrationCredentialRecord{}
	for id, p := range s.Principals {
		r.principals[id] = p
	}
	for id, c := range s.Credentials {
		r.credentials[id] = CloneIntegrationCredential(c)
	}
}
