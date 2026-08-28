package academicterm

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	List(context.Context, ListFilter) ([]Term, error)
	Get(context.Context, string) (Term, error)
	Create(context.Context, Term) (Term, error)
	UpdateFirstWeek(context.Context, string, int64, time.Time, string) (Term, error)
	Close(context.Context, string, int64, string) (Term, error)
	Open(context.Context, string, int64, string) (Term, error)
	SetDefault(context.Context, string, int64, string) (Term, error)
	Delete(context.Context, string, int64) error
}

type MemoryRepository struct {
	mu    sync.RWMutex
	items map[string]Term
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: make(map[string]Term)}
}

func (r *MemoryRepository) List(_ context.Context, filter ListFilter) ([]Term, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Term, 0, len(r.items))
	for _, item := range r.items {
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		items = append(items, item.withDerivedFields())
	}
	sortTerms(items)
	return items, nil
}

func (r *MemoryRepository) Get(_ context.Context, id string) (Term, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[id]
	if !ok {
		return Term{}, ErrNotFound
	}
	return item.withDerivedFields(), nil
}

func (r *MemoryRepository) Create(_ context.Context, item Term) (Term, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.items {
		if existing.Year == item.Year && existing.Semester == item.Semester {
			return Term{}, ErrAlreadyExists
		}
	}
	if item.IsDefault {
		for id, existing := range r.items {
			if existing.IsDefault {
				existing.IsDefault = false
				existing.Version++
				existing.UpdatedAt = item.CreatedAt
				existing.UpdatedBy = item.CreatedBy
				r.items[id] = existing
			}
		}
	}
	r.items[item.ID] = item
	return item.withDerivedFields(), nil
}

func (r *MemoryRepository) UpdateFirstWeek(_ context.Context, id string, expected int64, firstWeek time.Time, actor string) (Term, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, err := r.mutable(id, expected)
	if err != nil {
		return Term{}, err
	}
	if item.Status != StatusOpen {
		return Term{}, ErrClosed
	}
	item.FirstWeekStart = firstWeek.Format("2006-01-02")
	return r.save(item, actor), nil
}

func (r *MemoryRepository) Close(_ context.Context, id string, expected int64, actor string) (Term, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, err := r.mutable(id, expected)
	if err != nil {
		return Term{}, err
	}
	if item.Status != StatusOpen {
		return Term{}, ErrClosed
	}
	now := time.Now().UTC()
	item.Status, item.IsDefault, item.ClosedAt = StatusClosed, false, &now
	return r.save(item, actor), nil
}

func (r *MemoryRepository) Open(_ context.Context, id string, expected int64, actor string) (Term, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, err := r.mutable(id, expected)
	if err != nil {
		return Term{}, err
	}
	if item.Status != StatusClosed {
		return Term{}, ErrInvalid
	}
	item.Status, item.ClosedAt = StatusOpen, nil
	return r.save(item, actor), nil
}

func (r *MemoryRepository) SetDefault(_ context.Context, id string, expected int64, actor string) (Term, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, err := r.mutable(id, expected)
	if err != nil {
		return Term{}, err
	}
	if item.Status != StatusOpen {
		return Term{}, ErrClosed
	}
	for otherID, other := range r.items {
		if otherID != id && other.IsDefault {
			other.IsDefault = false
			r.items[otherID] = r.save(other, actor)
		}
	}
	item.IsDefault = true
	return r.save(item, actor), nil
}

func (r *MemoryRepository) Delete(_ context.Context, id string, expected int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, err := r.mutable(id, expected)
	if err != nil {
		return err
	}
	delete(r.items, item.ID)
	return nil
}

func (r *MemoryRepository) mutable(id string, expected int64) (Term, error) {
	item, ok := r.items[id]
	if !ok {
		return Term{}, ErrNotFound
	}
	if item.Version != expected {
		return Term{}, ErrVersionConflict
	}
	return item, nil
}

func (r *MemoryRepository) save(item Term, actor string) Term {
	item.Version++
	item.UpdatedAt = time.Now().UTC()
	item.UpdatedBy = actor
	r.items[item.ID] = item
	return item.withDerivedFields()
}

type PgRepository struct{ pool *pgxpool.Pool }

func NewPgRepository(pool *pgxpool.Pool) *PgRepository { return &PgRepository{pool: pool} }

func (r *PgRepository) List(ctx context.Context, filter ListFilter) ([]Term, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, year, semester, first_week_start::text, status, is_default,
		version, COALESCE(created_by::text, ''), COALESCE(updated_by::text, ''), created_at, updated_at, closed_at
		FROM academic_terms WHERE ($1='' OR status=$1)
		ORDER BY year DESC, CASE semester WHEN 'fall' THEN 0 ELSE 1 END, created_at DESC`, filter.Status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Term, 0)
	for rows.Next() {
		item, scanErr := scanTerm(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PgRepository) Get(ctx context.Context, id string) (Term, error) {
	return queryTerm(ctx, r.pool, `SELECT id::text, year, semester, first_week_start::text, status, is_default,
		version, COALESCE(created_by::text, ''), COALESCE(updated_by::text, ''), created_at, updated_at, closed_at
		FROM academic_terms WHERE id=$1::bigint`, id)
}

func (r *PgRepository) Create(ctx context.Context, item Term) (Term, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Term{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if item.IsDefault {
		if _, err = tx.Exec(ctx, `UPDATE academic_terms SET is_default=FALSE, version=version+1,
			updated_by=NULLIF($1, '')::bigint, updated_at=NOW() WHERE is_default=TRUE`, item.CreatedBy); err != nil {
			return Term{}, err
		}
	}
	created, err := queryTerm(ctx, tx, `INSERT INTO academic_terms
		(id, year, semester, first_week_start, status, is_default, version, created_by, updated_by, created_at, updated_at, closed_at)
		VALUES ($1::bigint, $2, $3, $4::date, $5, $6, 1, NULLIF($7, '')::bigint, NULLIF($7, '')::bigint, NOW(), NOW(),
			CASE WHEN $5='closed' THEN NOW() ELSE NULL END)
		RETURNING id::text, year, semester, first_week_start::text, status, is_default, version,
			COALESCE(created_by::text, ''), COALESCE(updated_by::text, ''), created_at, updated_at, closed_at`,
		item.ID, item.Year, item.Semester, item.FirstWeekStart, item.Status, item.IsDefault, item.CreatedBy)
	if err != nil {
		if isUniqueViolation(err) {
			return Term{}, ErrAlreadyExists
		}
		return Term{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Term{}, err
	}
	return created, nil
}

func (r *PgRepository) UpdateFirstWeek(ctx context.Context, id string, expected int64, firstWeek time.Time, actor string) (Term, error) {
	item, err := queryTerm(ctx, r.pool, `UPDATE academic_terms SET first_week_start=$1::date, version=version+1,
		updated_by=NULLIF($2, '')::bigint, updated_at=NOW()
		WHERE id=$3::bigint AND version=$4 AND status='open'
		RETURNING id::text, year, semester, first_week_start::text, status, is_default, version,
			COALESCE(created_by::text, ''), COALESCE(updated_by::text, ''), created_at, updated_at, closed_at`, firstWeek.Format("2006-01-02"), actor, id, expected)
	if errors.Is(err, pgx.ErrNoRows) {
		return Term{}, r.classifyMutation(ctx, id, expected, StatusOpen)
	}
	return item, err
}

func (r *PgRepository) Close(ctx context.Context, id string, expected int64, actor string) (Term, error) {
	item, err := queryTerm(ctx, r.pool, `UPDATE academic_terms SET status='closed', is_default=FALSE, closed_at=NOW(),
		version=version+1, updated_by=NULLIF($1, '')::bigint, updated_at=NOW()
		WHERE id=$2::bigint AND version=$3 AND status='open'
		RETURNING id::text, year, semester, first_week_start::text, status, is_default, version,
			COALESCE(created_by::text, ''), COALESCE(updated_by::text, ''), created_at, updated_at, closed_at`, actor, id, expected)
	if errors.Is(err, pgx.ErrNoRows) {
		return Term{}, r.classifyMutation(ctx, id, expected, StatusOpen)
	}
	return item, err
}

func (r *PgRepository) Open(ctx context.Context, id string, expected int64, actor string) (Term, error) {
	item, err := queryTerm(ctx, r.pool, `UPDATE academic_terms SET status='open', closed_at=NULL,
		version=version+1, updated_by=NULLIF($1, '')::bigint, updated_at=NOW()
		WHERE id=$2::bigint AND version=$3 AND status='closed'
		RETURNING id::text, year, semester, first_week_start::text, status, is_default, version,
			COALESCE(created_by::text, ''), COALESCE(updated_by::text, ''), created_at, updated_at, closed_at`, actor, id, expected)
	if errors.Is(err, pgx.ErrNoRows) {
		return Term{}, r.classifyMutation(ctx, id, expected, StatusClosed)
	}
	return item, err
}

func (r *PgRepository) SetDefault(ctx context.Context, id string, expected int64, actor string) (Term, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Term{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	target, err := queryTerm(ctx, tx, `SELECT id::text, year, semester, first_week_start::text, status, is_default, version,
		COALESCE(created_by::text, ''), COALESCE(updated_by::text, ''), created_at, updated_at, closed_at
		FROM academic_terms WHERE id=$1::bigint FOR UPDATE`, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Term{}, ErrNotFound
	}
	if err != nil {
		return Term{}, err
	}
	if target.Version != expected {
		return Term{}, ErrVersionConflict
	}
	if target.Status != StatusOpen {
		return Term{}, ErrClosed
	}
	if _, err = tx.Exec(ctx, `UPDATE academic_terms SET is_default=FALSE, version=version+1,
		updated_by=NULLIF($1, '')::bigint, updated_at=NOW() WHERE is_default=TRUE AND id<>$2::bigint`, actor, id); err != nil {
		return Term{}, err
	}
	item, err := queryTerm(ctx, tx, `UPDATE academic_terms SET is_default=TRUE, version=version+1,
		updated_by=NULLIF($1, '')::bigint, updated_at=NOW() WHERE id=$2::bigint
		RETURNING id::text, year, semester, first_week_start::text, status, is_default, version,
			COALESCE(created_by::text, ''), COALESCE(updated_by::text, ''), created_at, updated_at, closed_at`, actor, id)
	if err != nil {
		return Term{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Term{}, err
	}
	return item, nil
}

func (r *PgRepository) Delete(ctx context.Context, id string, expected int64) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM academic_terms WHERE id=$1::bigint AND version=$2`, id, expected)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrHasSchedules
		}
		return err
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	return r.classifyMutation(ctx, id, expected, "")
}

func (r *PgRepository) classifyMutation(ctx context.Context, id string, expected int64, expectedStatus string) error {
	item, err := r.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if item.Version != expected {
		return ErrVersionConflict
	}
	if expectedStatus != "" && item.Status != expectedStatus {
		return ErrClosed
	}
	return ErrVersionConflict
}

type rowScanner interface{ Scan(...any) error }

func queryTerm(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, query string, args ...any) (Term, error) {
	return scanTerm(queryer.QueryRow(ctx, query, args...))
}

func scanTerm(scanner rowScanner) (Term, error) {
	var item Term
	err := scanner.Scan(&item.ID, &item.Year, &item.Semester, &item.FirstWeekStart, &item.Status, &item.IsDefault,
		&item.Version, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt, &item.ClosedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Term{}, ErrNotFound
	}
	if err != nil {
		return Term{}, err
	}
	return item.withDerivedFields(), nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func sortTerms(items []Term) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Year != items[j].Year {
			return items[i].Year > items[j].Year
		}
		if items[i].Semester != items[j].Semester {
			return items[i].Semester == SemesterFall
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
}
