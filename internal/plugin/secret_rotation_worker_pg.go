package plugin

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These two signed int32 values form one application-owned advisory-lock key.
const (
	secretRotationLockNamespace int32 = 0x43414d50 // CAMP
	secretRotationLockID        int32 = 0x53454352 // SECR
)

type PgSecretRotationLocker struct {
	pool *pgxpool.Pool
}

func NewPgSecretRotationLocker(pool *pgxpool.Pool) *PgSecretRotationLocker {
	return &PgSecretRotationLocker{pool: pool}
}

func (l *PgSecretRotationLocker) TryAcquire(ctx context.Context) (SecretRotationGuard, bool, error) {
	if l == nil || l.pool == nil {
		return nil, false, ErrSecretRotationLease
	}
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, false, ErrSecretRotationLease
	}
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1::integer,$2::integer)`, secretRotationLockNamespace, secretRotationLockID).Scan(&acquired); err != nil {
		_ = conn.Conn().Close(context.Background())
		conn.Release()
		return nil, false, ErrSecretRotationLease
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	return &pgSecretRotationGuard{conn: conn}, true, nil
}

type pgSecretRotationGuard struct {
	mu       sync.Mutex
	conn     *pgxpool.Conn
	released bool
}

func (g *pgSecretRotationGuard) Check(ctx context.Context) error {
	if g == nil {
		return ErrSecretRotationLease
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.released || g.conn == nil {
		return ErrSecretRotationLease
	}
	var held bool
	if err := g.conn.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND granted
		AND classid=1128353104::oid AND objid=1397048146::oid AND objsubid=2
	)`).Scan(&held); err != nil || !held {
		return ErrSecretRotationLease
	}
	return nil
}

func (g *pgSecretRotationGuard) Release(ctx context.Context) error {
	if g == nil {
		return ErrSecretRotationLease
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.released || g.conn == nil {
		return ErrSecretRotationLease
	}
	g.released = true
	defer g.conn.Release()
	var unlocked bool
	if err := g.conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1::integer,$2::integer)`, secretRotationLockNamespace, secretRotationLockID).Scan(&unlocked); err != nil || !unlocked {
		// Never return a connection with an uncertain session lock to the pool.
		_ = g.conn.Conn().Close(context.Background())
		return ErrSecretRotationLease
	}
	return nil
}

// ListActiveSecretRewrapPluginIDs scans one bounded plugin-ID page, not Secret
// names or values. A new lower ID can appear behind this round's cursor; the
// cursor wraps at the end of inventory and can discover it in a later pass.
func (s *PgAuthorizationStore) ListActiveSecretRewrapPluginIDs(ctx context.Context, activeKeyID string, afterPluginID int64, limit int) ([]int64, error) {
	if s == nil || s.pool == nil || activeKeyID == "" || afterPluginID < 0 || limit < 1 || limit > 100 {
		return nil, ErrSecretRotationInventory
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT plugin_id FROM plugin_secret_values
		WHERE status='active' AND revoked_at IS NULL AND key_version<>$1 AND plugin_id>$2
		ORDER BY plugin_id LIMIT $3`, activeKeyID, afterPluginID, limit)
	if err != nil {
		return nil, ErrSecretRotationInventory
	}
	defer rows.Close()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, ErrSecretRotationInventory
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		return nil, ErrSecretRotationInventory
	}
	return ids, nil
}

var _ SecretRotationInventory = (*PgAuthorizationStore)(nil)
var _ SecretRotationLocker = (*PgSecretRotationLocker)(nil)
