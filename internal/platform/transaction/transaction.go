// Package transaction provides the small transaction boundary used by CampusOS
// application services. Repositories may obtain an executor from the context,
// but services never receive a database handle directly.
package transaction

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Manager executes one application command atomically where the configured
// persistence implementation supports transactions.
type Manager interface {
	Within(context.Context, func(context.Context) error) error
}

// Executor is the subset of pgx used by PostgreSQL repositories. Keeping this
// interface here lets a repository transparently use either a pool or the
// transaction placed into its context.
type Executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type contextKey struct{}
type activeKey struct{}

// PostgreSQL is the production transaction manager.
type PostgreSQL struct {
	pool *pgxpool.Pool
}

func NewPostgreSQL(pool *pgxpool.Pool) *PostgreSQL {
	return &PostgreSQL{pool: pool}
}

func (m *PostgreSQL) Within(ctx context.Context, fn func(context.Context) error) error {
	if _, ok := FromContext(ctx); ok {
		return fn(ctx)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	txCtx := context.WithValue(ctx, contextKey{}, tx)
	txCtx = context.WithValue(txCtx, activeKey{}, true)
	if err := fn(txCtx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// FromContext returns the current PostgreSQL transaction without exposing a
// database connection to application services.
func FromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(contextKey{}).(pgx.Tx)
	return tx, ok
}

// ExecutorFor returns the context transaction when present, otherwise the
// repository's normal pool. It is intended for repository implementations.
func ExecutorFor(ctx context.Context, fallback *pgxpool.Pool) Executor {
	if tx, ok := FromContext(ctx); ok {
		return tx
	}
	return fallback
}

// Active reports whether code is executing through a CampusOS command
// transaction. It is useful when legacy event publishers need to defer to the
// durable outbox rather than publish before the transaction commits.
func Active(ctx context.Context) bool {
	active, _ := ctx.Value(activeKey{}).(bool)
	return active
}

// Snapshotter gives memory-backed repositories an optional rollback contract.
// Production persistence always uses PostgreSQL transactions; this exists to
// make the memory profile deterministic in tests and local development.
type Snapshotter interface {
	Snapshot() any
	Restore(any)
}

// Memory serializes commands and restores registered snapshots after a failed
// command. It intentionally has no SQL executor and must not be used to mask a
// missing production transaction boundary.
type Memory struct {
	mu           sync.Mutex
	snapshotters []Snapshotter
}

func NewMemory(snapshotters ...Snapshotter) *Memory {
	return &Memory{snapshotters: snapshotters}
}

// AddSnapshotters extends the local-profile rollback set during bootstrap.
// It is intentionally only supported by the memory adapter: PostgreSQL owns
// rollback through the database transaction itself.
func (m *Memory) AddSnapshotters(snapshotters ...Snapshotter) {
	if m == nil || len(snapshotters) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, snapshotter := range snapshotters {
		if snapshotter == nil {
			continue
		}
		duplicate := false
		for _, existing := range m.snapshotters {
			if existing == snapshotter {
				duplicate = true
				break
			}
		}
		if !duplicate {
			m.snapshotters = append(m.snapshotters, snapshotter)
		}
	}
}

func (m *Memory) Within(ctx context.Context, fn func(context.Context) error) error {
	if Active(ctx) {
		return fn(ctx)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	snapshots := make([]any, len(m.snapshotters))
	for i, snapshotter := range m.snapshotters {
		snapshots[i] = snapshotter.Snapshot()
	}
	restore := func() {
		for i := len(m.snapshotters) - 1; i >= 0; i-- {
			m.snapshotters[i].Restore(snapshots[i])
		}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			restore()
			panic(recovered)
		}
	}()

	txCtx := context.WithValue(ctx, activeKey{}, true)
	if err := fn(txCtx); err != nil {
		restore()
		return err
	}
	return nil
}
