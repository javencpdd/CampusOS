package plugin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"sync"
)

const v5WasmLeaderNamespace int32 = 0x43414d50
const v5WasmLeaderID int32 = 0x5741534d

func (s *PgV5StateStore) AcquireRuntimeLeader(ctx context.Context) (V5RuntimeLeader, error) {
	if s == nil || s.pool == nil {
		return nil, ErrV5RuntimeMissing
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, ErrV5RuntimeMissing
	}
	var held bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1::integer,$2::integer)`, v5WasmLeaderNamespace, v5WasmLeaderID).Scan(&held); err != nil || !held {
		_ = conn.Conn().Close(context.Background())
		conn.Release()
		return nil, ErrV5RuntimeMissing
	}
	return &v5RuntimeLeader{conn: conn}, nil
}

type v5RuntimeLeader struct {
	mu     sync.Mutex
	conn   *pgxpool.Conn
	closed bool
}

func (l *v5RuntimeLeader) Check(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrV5RuntimeMissing
	}
	var held bool
	if err := l.conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND granted AND classid=$1::oid AND objid=$2::oid AND objsubid=2)`, v5WasmLeaderNamespace, v5WasmLeaderID).Scan(&held); err != nil || !held {
		return ErrV5RuntimeMissing
	}
	return nil
}
func (l *v5RuntimeLeader) Release(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	defer l.conn.Release()
	if l.conn.Conn().IsClosed() {
		return nil
	}
	var unlocked bool
	if err := l.conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1::integer,$2::integer)`, v5WasmLeaderNamespace, v5WasmLeaderID).Scan(&unlocked); err != nil || !unlocked {
		_ = l.conn.Conn().Close(context.Background())
		return ErrV5RuntimeMissing
	}
	return nil
}

// Stable per-database scope, containing no DSN/password. Separate databases on
// one Docker host must never reap each other's reserved worker namespace.
func (s *PgV5StateStore) RuntimeNamespace() string {
	if s == nil || s.pool == nil {
		return ""
	}
	c := s.pool.Config().ConnConfig
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", c.Host, c.Port, c.Database))))
}
