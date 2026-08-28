package schedule

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TermReferenceRepository records the durable relationship between a user's
// schedule JSON and an AcademicTerm.  The JSON stays readable for legacy
// compatibility, while the relational reference protects a term from being
// deleted after users started using it.
type TermReferenceRepository interface {
	Upsert(context.Context, TermReference) error
	SetPreference(context.Context, string, string) error
}

type TermReference struct {
	UserID          string
	AcademicTermID  string
	CurrentObjectID string
	FirstWeekStart  string
}

type MemoryTermReferenceRepository struct {
	mu          sync.Mutex
	items       map[string]time.Time
	preferences map[string]string
}

func NewMemoryTermReferenceRepository() *MemoryTermReferenceRepository {
	return &MemoryTermReferenceRepository{items: make(map[string]time.Time), preferences: make(map[string]string)}
}

func (r *MemoryTermReferenceRepository) Upsert(_ context.Context, item TermReference) error {
	if item.UserID == "" || item.AcademicTermID == "" {
		return errors.New("schedule term reference is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[item.UserID+":"+item.AcademicTermID] = time.Now().UTC()
	return nil
}

func (r *MemoryTermReferenceRepository) SetPreference(_ context.Context, userID, termID string) error {
	if userID == "" || termID == "" {
		return errors.New("schedule preference is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.preferences[userID] = termID
	return nil
}

type PgTermReferenceRepository struct{ pool *pgxpool.Pool }

func NewPgTermReferenceRepository(pool *pgxpool.Pool) *PgTermReferenceRepository {
	return &PgTermReferenceRepository{pool: pool}
}

func (r *PgTermReferenceRepository) Upsert(ctx context.Context, item TermReference) error {
	if item.UserID == "" || item.AcademicTermID == "" {
		return errors.New("schedule term reference is invalid")
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO user_schedule_terms (user_id, academic_term_id, current_object_id, first_week_start, version, created_at, updated_at)
		VALUES ($1::bigint, $2::bigint, NULLIF($3, '')::bigint, NULLIF($4, '')::date, 1, NOW(), NOW())
		ON CONFLICT (user_id, academic_term_id) DO UPDATE SET
			current_object_id=COALESCE(EXCLUDED.current_object_id,user_schedule_terms.current_object_id),
			first_week_start=COALESCE(EXCLUDED.first_week_start,user_schedule_terms.first_week_start),
			version=user_schedule_terms.version+1,updated_at=EXCLUDED.updated_at`, item.UserID, item.AcademicTermID, item.CurrentObjectID, item.FirstWeekStart)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return errors.New("academic term reference is unavailable")
	}
	return err
}

func (r *PgTermReferenceRepository) SetPreference(ctx context.Context, userID, termID string) error {
	if userID == "" || termID == "" {
		return errors.New("schedule preference is invalid")
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO user_schedule_preferences (user_id,academic_term_id,updated_at)
		VALUES ($1::bigint,$2::bigint,NOW())
		ON CONFLICT (user_id) DO UPDATE SET academic_term_id=EXCLUDED.academic_term_id,updated_at=EXCLUDED.updated_at`, userID, termID)
	return err
}
