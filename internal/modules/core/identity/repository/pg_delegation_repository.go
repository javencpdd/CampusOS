package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Delegation kinds for the v1.2 identity delegation baseline. Management rows
// witness an admin's identity.role.assign authority, bounds are delegable
// per-board ceilings and grants are the delegated execution rights of users.
const (
	DelegationKindManagement = "management"
	DelegationKindBound      = "bound"
	DelegationKindGrant      = "grant"
)

const (
	DelegationStatusActive    = "active"
	DelegationStatusSuspended = "suspended"
	DelegationStatusRevoked   = "revoked"
)

var (
	ErrDelegationNotFound         = errors.New("identity delegation not found")
	ErrDelegationVersionConflict  = errors.New("identity delegation version conflict")
	ErrDelegationInvalidShape     = errors.New("identity delegation shape is invalid")
	ErrDelegationRevokedPermanent = errors.New("identity delegation revocation is permanent")
)

// Delegation is one row of identity_delegations. Windows are finite half-open
// intervals; the policy layer compares them in whole Unix seconds.
type Delegation struct {
	ID               string    `json:"id"`
	Kind             string    `json:"kind"`
	SubjectKind      string    `json:"subject_kind"`
	SubjectID        string    `json:"subject_id"`
	Action           string    `json:"action"`
	BoardID          string    `json:"board_id,omitempty"`
	NotBefore        time.Time `json:"not_before"`
	ExpiresAt        time.Time `json:"expires_at"`
	RequiredStrength string    `json:"required_strength,omitempty"`
	Delegable        bool      `json:"delegable"`
	Status           string    `json:"status"`
	Version          int64     `json:"version"`
	CreatedBy        string    `json:"created_by,omitempty"`
}

// Validate mirrors the identity_delegations CHECK constraints so invalid
// shapes fail before the database is touched.
func (d Delegation) Validate() error {
	if !validDelegationOpaqueID(d.ID) || !validDelegationOpaqueID(d.SubjectID) || d.Version < 1 || !d.NotBefore.Before(d.ExpiresAt) {
		return ErrDelegationInvalidShape
	}
	switch d.Kind {
	case DelegationKindManagement:
		if d.SubjectKind != "admin" || d.Action != "identity.role.assign" || d.BoardID != "" || d.RequiredStrength != "" || d.Delegable {
			return ErrDelegationInvalidShape
		}
	case DelegationKindBound:
		if d.SubjectKind != "admin" || !isGovernanceDelegationAction(d.Action) || !validDelegationOpaqueID(d.BoardID) ||
			(d.RequiredStrength != "password" && d.RequiredStrength != "mfa") || !d.Delegable {
			return ErrDelegationInvalidShape
		}
	case DelegationKindGrant:
		if d.SubjectKind != "user" || !isGovernanceDelegationAction(d.Action) || !validDelegationOpaqueID(d.BoardID) ||
			(d.RequiredStrength != "password" && d.RequiredStrength != "mfa") || d.Delegable {
			return ErrDelegationInvalidShape
		}
	default:
		return ErrDelegationInvalidShape
	}
	switch d.Status {
	case DelegationStatusActive, DelegationStatusSuspended, DelegationStatusRevoked:
		return nil
	default:
		return ErrDelegationInvalidShape
	}
}

func isGovernanceDelegationAction(action string) bool {
	return action == "community.thread.take_down" || action == "community.post.delete"
}

func validDelegationOpaqueID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
			continue
		}
		if i > 0 && (ch == '.' || ch == '_' || ch == ':' || ch == '-') {
			continue
		}
		return false
	}
	return true
}

// DelegationRepository reads current delegation facts and writes grants under
// caller transactions. Window and authorization semantics stay with the
// service/policy layer; the repository only persists validated shapes.
type DelegationRepository interface {
	ListByKindSubject(ctx context.Context, kind, subjectID string) ([]Delegation, error)
	GetDelegation(ctx context.Context, id string) (Delegation, error)
	InsertDelegations(ctx context.Context, items []Delegation) error
	// SetStatusCAS transitions one row when the expected version matches.
	// Revoked is terminal: a revoked row never transitions again.
	SetStatusCAS(ctx context.Context, id string, expectedVersion int64, status string, at time.Time) (Delegation, bool, error)
}

// DelegationAuthorityLocker extends DelegationRepository with row-locked
// reads for the in-transaction recheck. Memory adapters provide the same
// values without locks; the single-process memory profile serializes writers
// through its own mutex.
type DelegationAuthorityLocker interface {
	ListByKindSubjectForUpdate(ctx context.Context, kind, subjectID string) ([]Delegation, error)
}

const delegationColumns = `id, kind, subject_kind, subject_id, action, board_id, not_before, expires_at, required_strength, delegable, status, version, created_by`

func scanDelegation(row pgx.Row) (Delegation, error) {
	var d Delegation
	var boardID, strength *string
	err := row.Scan(&d.ID, &d.Kind, &d.SubjectKind, &d.SubjectID, &d.Action, &boardID,
		&d.NotBefore, &d.ExpiresAt, &strength, &d.Delegable, &d.Status, &d.Version, &d.CreatedBy)
	if err != nil {
		return Delegation{}, err
	}
	if boardID != nil {
		d.BoardID = *boardID
	}
	if strength != nil {
		d.RequiredStrength = *strength
	}
	return d, nil
}

func scanDelegationRows(rows pgx.Rows) ([]Delegation, error) {
	defer rows.Close()
	items := []Delegation{}
	for rows.Next() {
		d, err := scanDelegation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

// PgDelegationRepository is the PostgreSQL adapter of DelegationRepository.
type PgDelegationRepository struct {
	pool *pgxpool.Pool
}

func NewPgDelegationRepository(pool *pgxpool.Pool) *PgDelegationRepository {
	return &PgDelegationRepository{pool: pool}
}

func (r *PgDelegationRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgDelegationRepository) ListByKindSubject(ctx context.Context, kind, subjectID string) ([]Delegation, error) {
	rows, err := r.db(ctx).Query(ctx,
		`SELECT `+delegationColumns+` FROM identity_delegations WHERE kind = $1 AND subject_id = $2 ORDER BY id`, kind, subjectID)
	if err != nil {
		return nil, err
	}
	return scanDelegationRows(rows)
}

// ListByKindSubjectForUpdate locks the authority rows inside the caller's
// transaction so a concurrent revocation serializes against the recheck.
func (r *PgDelegationRepository) ListByKindSubjectForUpdate(ctx context.Context, kind, subjectID string) ([]Delegation, error) {
	rows, err := r.db(ctx).Query(ctx,
		`SELECT `+delegationColumns+` FROM identity_delegations WHERE kind = $1 AND subject_id = $2 ORDER BY id FOR UPDATE`, kind, subjectID)
	if err != nil {
		return nil, err
	}
	return scanDelegationRows(rows)
}

func (r *PgDelegationRepository) GetDelegation(ctx context.Context, id string) (Delegation, error) {
	d, err := scanDelegation(r.db(ctx).QueryRow(ctx,
		`SELECT `+delegationColumns+` FROM identity_delegations WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Delegation{}, ErrDelegationNotFound
		}
		return Delegation{}, err
	}
	return d, nil
}

func (r *PgDelegationRepository) InsertDelegations(ctx context.Context, items []Delegation) error {
	db := r.db(ctx)
	for _, item := range items {
		shape := item
		shape.Status = DelegationStatusActive
		shape.Version = 1
		if err := shape.Validate(); err != nil {
			return err
		}
		var boardID, strength *string
		if item.BoardID != "" {
			boardID = &item.BoardID
		}
		if item.RequiredStrength != "" {
			strength = &item.RequiredStrength
		}
		if _, err := db.Exec(ctx,
			`INSERT INTO identity_delegations (id, kind, subject_kind, subject_id, action, board_id, not_before, expires_at, required_strength, delegable, status, version, created_by)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'active',1,$11)`,
			item.ID, item.Kind, item.SubjectKind, item.SubjectID, item.Action, boardID,
			item.NotBefore, item.ExpiresAt, strength, item.Delegable, item.CreatedBy); err != nil {
			return err
		}
	}
	return nil
}

func (r *PgDelegationRepository) SetStatusCAS(ctx context.Context, id string, expectedVersion int64, status string, at time.Time) (Delegation, bool, error) {
	if status != DelegationStatusActive && status != DelegationStatusSuspended && status != DelegationStatusRevoked {
		return Delegation{}, false, ErrDelegationInvalidShape
	}
	d, err := scanDelegation(r.db(ctx).QueryRow(ctx,
		`UPDATE identity_delegations
		    SET status = $2, version = version + 1, updated_at = $3,
		        revoked_at = CASE WHEN $5 THEN $3 ELSE revoked_at END
		  WHERE id = $1 AND version = $4 AND status <> 'revoked'
		  RETURNING `+delegationColumns, id, status, at, expectedVersion, status == DelegationStatusRevoked))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, lookupErr := r.GetDelegation(ctx, id)
			if errors.Is(lookupErr, ErrDelegationNotFound) {
				return Delegation{}, false, ErrDelegationNotFound
			}
			if lookupErr != nil {
				return Delegation{}, false, lookupErr
			}
			if existing.Status == DelegationStatusRevoked {
				return Delegation{}, false, ErrDelegationRevokedPermanent
			}
			return Delegation{}, false, nil
		}
		return Delegation{}, false, err
	}
	return d, true, nil
}

// MemoryDelegationRepository is the deterministic Memory profile adapter used
// by tests and local development; it supports the reliability snapshot
// contract like the other memory repositories.
type MemoryDelegationRepository struct {
	mu    sync.RWMutex
	items map[string]Delegation
}

func NewMemoryDelegationRepository() *MemoryDelegationRepository {
	return &MemoryDelegationRepository{items: make(map[string]Delegation)}
}

func (r *MemoryDelegationRepository) Snapshot() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	payload, err := json.Marshal(r.items)
	if err != nil {
		return []byte(nil)
	}
	return append([]byte(nil), payload...)
}

func (r *MemoryDelegationRepository) Restore(value any) {
	payload, ok := value.([]byte)
	if !ok || len(payload) == 0 {
		return
	}
	items := map[string]Delegation{}
	if err := json.Unmarshal(payload, &items); err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = items
}

func (r *MemoryDelegationRepository) ListByKindSubject(_ context.Context, kind, subjectID string) ([]Delegation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := []Delegation{}
	for _, d := range r.items {
		if d.Kind == kind && d.SubjectID == subjectID {
			items = append(items, d)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// ListByKindSubjectForUpdate mirrors the PostgreSQL row-lock variant; the
// memory profile already serializes writers through its mutex.
func (r *MemoryDelegationRepository) ListByKindSubjectForUpdate(ctx context.Context, kind, subjectID string) ([]Delegation, error) {
	return r.ListByKindSubject(ctx, kind, subjectID)
}

func (r *MemoryDelegationRepository) GetDelegation(_ context.Context, id string) (Delegation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.items[id]
	if !ok {
		return Delegation{}, ErrDelegationNotFound
	}
	return d, nil
}

func (r *MemoryDelegationRepository) InsertDelegations(_ context.Context, items []Delegation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range items {
		shape := item
		shape.Status = DelegationStatusActive
		shape.Version = 1
		if err := shape.Validate(); err != nil {
			return err
		}
		if _, exists := r.items[item.ID]; exists {
			return ErrDelegationVersionConflict
		}
	}
	for _, item := range items {
		item.Status = DelegationStatusActive
		item.Version = 1
		r.items[item.ID] = item
	}
	return nil
}

func (r *MemoryDelegationRepository) SetStatusCAS(_ context.Context, id string, expectedVersion int64, status string, _ time.Time) (Delegation, bool, error) {
	if status != DelegationStatusActive && status != DelegationStatusSuspended && status != DelegationStatusRevoked {
		return Delegation{}, false, ErrDelegationInvalidShape
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.items[id]
	if !ok {
		return Delegation{}, false, ErrDelegationNotFound
	}
	if d.Status == DelegationStatusRevoked {
		return Delegation{}, false, ErrDelegationRevokedPermanent
	}
	if d.Version != expectedVersion {
		return Delegation{}, false, nil
	}
	d.Status = status
	d.Version++
	r.items[id] = d
	return d, true, nil
}
