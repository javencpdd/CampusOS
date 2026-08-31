package schedule

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrTermReferenceNotFound = errors.New("schedule term reference not found")
	ErrTermReferenceConflict = errors.New("schedule term reference version conflict")
)

// TermReferenceRepository is the sole mutable binding between a user's
// schedule and the AcademicTerm catalogue. Switch performs both the
// expected-version comparison and preference update in one transaction.
type TermReferenceRepository interface {
	Get(context.Context, string, string) (TermReference, error)
	List(context.Context, string) ([]TermReference, error)
	Preference(context.Context, string) (string, error)
	Switch(context.Context, TermReference, int64) (TermReference, error)
	SetPreference(context.Context, string, string) error
}

type TermReference struct {
	UserID          string
	AcademicTermID  string
	CurrentObjectID string
	FirstWeekStart  string
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type MemoryTermReferenceRepository struct {
	mu          sync.RWMutex
	items       map[string]TermReference
	preferences map[string]string
}

func NewMemoryTermReferenceRepository() *MemoryTermReferenceRepository {
	return &MemoryTermReferenceRepository{items: make(map[string]TermReference), preferences: make(map[string]string)}
}

func (r *MemoryTermReferenceRepository) Get(_ context.Context, userID, termID string) (TermReference, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[termReferenceKey(userID, termID)]
	if !ok {
		return TermReference{}, ErrTermReferenceNotFound
	}
	return item, nil
}

func (r *MemoryTermReferenceRepository) List(_ context.Context, userID string) ([]TermReference, error) {
	if userID == "" {
		return nil, errors.New("schedule term reference user is invalid")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]TermReference, 0)
	for _, item := range r.items {
		if item.UserID == userID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items, nil
}

func (r *MemoryTermReferenceRepository) Preference(_ context.Context, userID string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	termID, ok := r.preferences[userID]
	if !ok {
		return "", ErrTermReferenceNotFound
	}
	return termID, nil
}

func (r *MemoryTermReferenceRepository) Switch(_ context.Context, item TermReference, expectedVersion int64) (TermReference, error) {
	if item.UserID == "" || item.AcademicTermID == "" || item.CurrentObjectID == "" || expectedVersion < 0 {
		return TermReference{}, errors.New("schedule term reference is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := termReferenceKey(item.UserID, item.AcademicTermID)
	existing, exists := r.items[key]
	if !exists {
		if expectedVersion != 0 {
			return TermReference{}, ErrTermReferenceConflict
		}
		now := time.Now().UTC()
		item.Version, item.CreatedAt, item.UpdatedAt = 1, now, now
	} else {
		if existing.Version != expectedVersion {
			return TermReference{}, ErrTermReferenceConflict
		}
		item.Version = existing.Version + 1
		item.CreatedAt = existing.CreatedAt
		item.UpdatedAt = time.Now().UTC()
	}
	r.items[key] = item
	r.preferences[item.UserID] = item.AcademicTermID
	return item, nil
}

func (r *MemoryTermReferenceRepository) SetPreference(_ context.Context, userID, termID string) error {
	if userID == "" || termID == "" {
		return errors.New("schedule preference is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[termReferenceKey(userID, termID)]; !ok {
		return ErrTermReferenceNotFound
	}
	r.preferences[userID] = termID
	return nil
}

type PgTermReferenceRepository struct {
	pool         *pgxpool.Pool
	transactions transaction.Manager
}

func NewPgTermReferenceRepository(pool *pgxpool.Pool) *PgTermReferenceRepository {
	return &PgTermReferenceRepository{pool: pool, transactions: transaction.NewPostgreSQL(pool)}
}

func (r *PgTermReferenceRepository) db(ctx context.Context) transaction.Executor {
	return transaction.ExecutorFor(ctx, r.pool)
}

func (r *PgTermReferenceRepository) Get(ctx context.Context, userID, termID string) (TermReference, error) {
	item, err := scanTermReference(r.db(ctx).QueryRow(ctx, `SELECT user_id::text, academic_term_id::text,
		COALESCE(current_object_id::text, ''), COALESCE(first_week_start::text, ''), version, created_at, updated_at
		FROM user_schedule_terms WHERE user_id=$1::bigint AND academic_term_id=$2::bigint`, userID, termID))
	if errors.Is(err, pgx.ErrNoRows) {
		return TermReference{}, ErrTermReferenceNotFound
	}
	return item, err
}

func (r *PgTermReferenceRepository) List(ctx context.Context, userID string) ([]TermReference, error) {
	if userID == "" {
		return nil, errors.New("schedule term reference user is invalid")
	}
	rows, err := r.db(ctx).Query(ctx, `SELECT user_id::text, academic_term_id::text,
		COALESCE(current_object_id::text, ''), COALESCE(first_week_start::text, ''), version, created_at, updated_at
		FROM user_schedule_terms WHERE user_id=$1::bigint ORDER BY updated_at DESC, academic_term_id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TermReference, 0)
	for rows.Next() {
		item, scanErr := scanTermReference(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgTermReferenceRepository) Preference(ctx context.Context, userID string) (string, error) {
	var termID string
	err := r.db(ctx).QueryRow(ctx, `SELECT academic_term_id::text FROM user_schedule_preferences WHERE user_id=$1::bigint`, userID).Scan(&termID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTermReferenceNotFound
	}
	return termID, err
}

func (r *PgTermReferenceRepository) Switch(ctx context.Context, item TermReference, expectedVersion int64) (TermReference, error) {
	if item.UserID == "" || item.AcademicTermID == "" || item.CurrentObjectID == "" || expectedVersion < 0 {
		return TermReference{}, errors.New("schedule term reference is invalid")
	}
	var result TermReference
	err := r.transactions.Within(ctx, func(txCtx context.Context) error {
		db := r.db(txCtx)
		var currentVersion int64
		err := db.QueryRow(txCtx, `SELECT version FROM user_schedule_terms
			WHERE user_id=$1::bigint AND academic_term_id=$2::bigint FOR UPDATE`, item.UserID, item.AcademicTermID).Scan(&currentVersion)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if expectedVersion != 0 {
				return ErrTermReferenceConflict
			}
			result, err = scanTermReference(db.QueryRow(txCtx, `INSERT INTO user_schedule_terms
				(user_id, academic_term_id, current_object_id, first_week_start, version, created_at, updated_at)
				VALUES ($1::bigint,$2::bigint,$3::bigint,NULLIF($4,'')::date,1,NOW(),NOW())
				RETURNING user_id::text, academic_term_id::text, current_object_id::text,
				COALESCE(first_week_start::text,''), version, created_at, updated_at`,
				item.UserID, item.AcademicTermID, item.CurrentObjectID, item.FirstWeekStart))
		case err != nil:
			return err
		default:
			if currentVersion != expectedVersion {
				return ErrTermReferenceConflict
			}
			result, err = scanTermReference(db.QueryRow(txCtx, `UPDATE user_schedule_terms SET
				current_object_id=$3::bigint, first_week_start=NULLIF($4,'')::date,
				version=version+1, updated_at=NOW()
				WHERE user_id=$1::bigint AND academic_term_id=$2::bigint AND version=$5
				RETURNING user_id::text, academic_term_id::text, current_object_id::text,
				COALESCE(first_week_start::text,''), version, created_at, updated_at`,
				item.UserID, item.AcademicTermID, item.CurrentObjectID, item.FirstWeekStart, expectedVersion))
		}
		if err != nil {
			return err
		}
		_, err = db.Exec(txCtx, `INSERT INTO user_schedule_preferences (user_id, academic_term_id, updated_at)
			VALUES ($1::bigint,$2::bigint,NOW())
			ON CONFLICT (user_id) DO UPDATE SET academic_term_id=EXCLUDED.academic_term_id, updated_at=EXCLUDED.updated_at`, item.UserID, item.AcademicTermID)
		return err
	})
	if err != nil {
		return TermReference{}, err
	}
	return result, nil
}

func (r *PgTermReferenceRepository) SetPreference(ctx context.Context, userID, termID string) error {
	if userID == "" || termID == "" {
		return errors.New("schedule preference is invalid")
	}
	command, err := r.db(ctx).Exec(ctx, `INSERT INTO user_schedule_preferences (user_id, academic_term_id, updated_at)
		SELECT user_id, academic_term_id, NOW() FROM user_schedule_terms
		WHERE user_id=$1::bigint AND academic_term_id=$2::bigint
		ON CONFLICT (user_id) DO UPDATE SET academic_term_id=EXCLUDED.academic_term_id, updated_at=EXCLUDED.updated_at`, userID, termID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrTermReferenceNotFound
	}
	return nil
}

func scanTermReference(row pgx.Row) (TermReference, error) {
	var item TermReference
	err := row.Scan(&item.UserID, &item.AcademicTermID, &item.CurrentObjectID, &item.FirstWeekStart, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func termReferenceKey(userID, termID string) string { return userID + ":" + termID }
