package plugin

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrPluginV5IdentityUnavailable = errors.New("plugin.package_identity_unavailable")

// PluginV5PackageIdentity is a write-once security fact, not an installation,
// current trust decision, grant, package byte store or runtime instance.
type PluginV5PackageIdentity struct {
	Publisher string
	Key       string
	Version   string
	Digest    string
}

// PluginV5IdentityStore must commit the first digest for a coordinate and reject
// every different digest, including across independent catalogs and restarts.
// Only the catalog's verified package path should supply records.
type PluginV5IdentityStore interface {
	RecordPluginV5Identity(context.Context, PluginV5PackageIdentity) error
}

func validV5Identity(id PluginV5PackageIdentity) bool {
	return v5Name(id.Publisher) && v5Name(id.Key) && v5Version(id.Version) && v5Digest(id.Digest)
}

type MemoryPluginV5IdentityStore struct {
	mu      sync.Mutex
	digests map[string]string
}

// NewMemoryPluginV5IdentityStore is explicit and non-durable. Production server
// assembly never falls back to this store when PostgreSQL is unavailable.
func NewMemoryPluginV5IdentityStore() *MemoryPluginV5IdentityStore {
	return &MemoryPluginV5IdentityStore{digests: map[string]string{}}
}

func (s *MemoryPluginV5IdentityStore) RecordPluginV5Identity(ctx context.Context, id PluginV5PackageIdentity) error {
	if s == nil || s.digests == nil {
		return ErrPluginV5IdentityUnavailable
	}
	if !validV5Identity(id) {
		return ErrPluginV5PackageInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ErrPluginV5IdentityUnavailable
	}
	key := id.Publisher + "/" + id.Key + "@" + id.Version
	if old, ok := s.digests[key]; ok && old != id.Digest {
		return ErrPluginV5PackageConflict
	}
	s.digests[key] = id.Digest
	return nil
}

type PgPluginV5IdentityStore struct{ pool *pgxpool.Pool }

// A nil/closed pool yields an unavailable store, never a fresh memory registry.
func NewPgPluginV5IdentityStore(pool *pgxpool.Pool) *PgPluginV5IdentityStore {
	return &PgPluginV5IdentityStore{pool: pool}
}

func (s *PgPluginV5IdentityStore) RecordPluginV5Identity(ctx context.Context, id PluginV5PackageIdentity) error {
	if s == nil || s.pool == nil || ctx.Err() != nil {
		return ErrPluginV5IdentityUnavailable
	}
	if !validV5Identity(id) {
		return ErrPluginV5PackageInvalid
	}
	// READ COMMITTED statement snapshots are intentional: ON CONFLICT waits for
	// another writer, and the following SELECT sees that committed winner. The
	// database forbids updates/deletes/truncate, so the selected fact cannot ABA.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ErrPluginV5IdentityUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `INSERT INTO plugin_package_identities
		(publisher,plugin_key,version,package_digest) VALUES($1,$2,$3,$4)
		ON CONFLICT (publisher,plugin_key,version) DO NOTHING`, id.Publisher, id.Key, id.Version, id.Digest); err != nil {
		return ErrPluginV5IdentityUnavailable
	}
	var digest string
	if err = tx.QueryRow(ctx, `SELECT package_digest FROM plugin_package_identities
		WHERE publisher=$1 AND plugin_key=$2 AND version=$3`, id.Publisher, id.Key, id.Version).Scan(&digest); err != nil {
		return ErrPluginV5IdentityUnavailable
	}
	if digest != id.Digest {
		return ErrPluginV5PackageConflict
	}
	if tx.Commit(ctx) != nil {
		return ErrPluginV5IdentityUnavailable
	}
	return nil
}
