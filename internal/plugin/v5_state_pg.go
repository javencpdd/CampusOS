package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgV5StateStore struct{ pool *pgxpool.Pool }

func (s *PgV5StateStore) referenced(ctx context.Context, c V5Coordinate) (bool, error) {
	db, err := s.db(ctx)
	if err != nil {
		return false, err
	}
	var found bool
	err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM plugin_installations WHERE publisher=$1 AND plugin_key=$2 AND (state->>'installed')::boolean AND (version=$3 OR desired_version=$3) UNION ALL SELECT 1 FROM plugin_user_installations WHERE publisher=$1 AND plugin_key=$2 AND (state->>'installed')::boolean AND (version=$3 OR desired_version=$3))`, c.Publisher, c.Key, c.Version).Scan(&found)
	if err != nil {
		return false, ErrV5PlatformUnavailable
	}
	return found, nil
}

func NewPgV5StateStore(pool *pgxpool.Pool) *PgV5StateStore { return &PgV5StateStore{pool} }
func (s *PgV5StateStore) db(ctx context.Context) (transaction.Executor, error) {
	if s == nil || s.pool == nil {
		return nil, ErrV5PlatformUnavailable
	}
	return transaction.ExecutorFor(ctx, s.pool), nil
}
func (s *PgV5StateStore) operation(ctx context.Context, kind, actor, key string) (V5Operation, error) {
	db, err := s.db(ctx)
	if err != nil {
		return V5Operation{}, err
	}
	var b []byte
	err = db.QueryRow(ctx, `SELECT state FROM plugin_operations WHERE actor_kind=$1 AND actor_id=$2 AND idempotency_key=$3`, kind, actor, key).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return V5Operation{}, ErrV5NotFound
	}
	var op V5Operation
	if err != nil || json.Unmarshal(b, &op) != nil {
		return op, ErrV5PlatformUnavailable
	}
	return op, nil
}
func (s *PgV5StateStore) lock(ctx context.Context, key string) error {
	db, err := s.db(ctx)
	if err != nil || !transaction.Active(ctx) {
		return ErrV5PlatformUnavailable
	}
	_, err = db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "plugin-v5:"+key)
	if err != nil {
		return ErrV5PlatformUnavailable
	}
	return nil
}
func (s *PgV5StateStore) next(ctx context.Context) (int64, error) {
	db, err := s.db(ctx)
	if err != nil {
		return 0, err
	}
	var n int64
	if db.QueryRow(ctx, `SELECT nextval('plugin_v5_generations')`).Scan(&n) != nil {
		return 0, ErrV5PlatformUnavailable
	}
	return n, nil
}
func (s *PgV5StateStore) load(ctx context.Context, k v5RecordKind, id string, out any) error {
	if !v5KindValid(k) {
		return ErrV5Invalid
	}
	db, err := s.db(ctx)
	if err != nil {
		return err
	}
	var raw []byte
	err = db.QueryRow(ctx, `SELECT state FROM `+string(k)+` WHERE record_id=$1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrV5NotFound
	}
	if err != nil || json.Unmarshal(raw, out) != nil {
		return ErrV5PlatformUnavailable
	}
	return nil
}
func (s *PgV5StateStore) save(ctx context.Context, k v5RecordKind, id string, v any) error {
	if !v5KindValid(k) || !transaction.Active(ctx) {
		return ErrV5Invalid
	}
	db, err := s.db(ctx)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil || v5RecordID(k, raw) != id {
		return ErrV5Invalid
	}
	if k == v5Release {
		var r V5PackageRelease
		if json.Unmarshal(raw, &r) != nil {
			return ErrV5Invalid
		}
		if _, err := db.Exec(ctx, `INSERT INTO plugin_packages(publisher,plugin_key) VALUES($1,$2) ON CONFLICT DO NOTHING`, r.Publisher, r.Key); err != nil {
			return ErrV5PlatformUnavailable
		}
	}
	_, err = db.Exec(ctx, `INSERT INTO `+string(k)+`(record_id,state) VALUES($1,$2::jsonb) ON CONFLICT(record_id) DO UPDATE SET state=EXCLUDED.state`, id, raw)
	if err != nil {
		return ErrV5PlatformUnavailable
	}
	return nil
}
func (s *PgV5StateStore) list(ctx context.Context, k v5RecordKind, q v5Query) ([]json.RawMessage, error) {
	if !v5KindValid(k) {
		return nil, ErrV5Invalid
	}
	db, err := s.db(ctx)
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit < 1 || limit > 51 {
		limit = 51
	}
	query := `SELECT r.state FROM ` + string(k) + ` r WHERE r.record_id>$1`
	args := []any{q.After, limit}
	if q.VisibleOwner != "" {
		if k != v5Global || !v5OwnerValid(q.VisibleOwner) {
			return nil, ErrV5Invalid
		}
		// Visibility is applied before pagination: current available versions
		// UNION the caller's own installed versions, including revoked releases.
		query = `SELECT r.state FROM (
		 SELECT DISTINCT ON (cursor) state,cursor FROM (
		  SELECT i.state, i.publisher||'/'||i.plugin_key||'@'||i.version AS cursor
		  FROM plugin_installations i WHERE (i.state->>'installed')::boolean AND EXISTS(
		   SELECT 1 FROM plugin_v5_admin_grants g WHERE g.publisher=i.publisher AND g.plugin_key=i.plugin_key AND g.version=i.version AND (g.state->>'approved')::boolean
		    AND (COALESCE((g.state->>'expires_at_ms')::bigint,0)=0 OR (g.state->>'expires_at_ms')::bigint>$4))
		  UNION ALL
		  SELECT i.state,i.publisher||'/'||i.plugin_key||'@'||i.version AS cursor FROM plugin_user_installations i WHERE i.state->>'owner_user_id'=$3 AND (i.state->>'installed')::boolean
		 ) visible ORDER BY cursor
		) r WHERE r.cursor>$1 ORDER BY r.cursor LIMIT $2`
		args = []any{q.After, limit, q.VisibleOwner, q.Now}
	}
	if q.Owner != "" {
		query += ` AND r.state->>'owner_user_id'=$3`
		args = append(args, q.Owner)
	}
	if q.Pending {
		if k != v5Operation || q.Owner != "" {
			return nil, ErrV5Invalid
		}
		query += ` AND r.state->>'status' IN ('queued','working','waiting') AND (r.state->>'lease_until_ms')::bigint <= $3`
		args = append(args, q.Now)
	}
	if q.Visible {
		if k != v5Global {
			return nil, ErrV5Invalid
		}
		query += ` AND (r.state->>'installed')::boolean AND EXISTS(SELECT 1 FROM plugin_v5_admin_grants g WHERE g.publisher=r.publisher AND g.plugin_key=r.plugin_key AND g.version=r.version AND (g.state->>'approved')::boolean)`
	}
	if q.Publisher != "" {
		query += fmt.Sprintf(` AND r.publisher=$%d AND r.plugin_key=$%d`, len(args)+1, len(args)+2)
		args = append(args, q.Publisher, q.Key)
	}
	if q.VisibleOwner == "" && q.Pending {
		query += ` ORDER BY CASE r.state->>'status' WHEN 'queued' THEN 0 WHEN 'working' THEN 1 ELSE 2 END, (r.state->>'lease_until_ms')::bigint, r.record_id LIMIT $2`
	} else if q.VisibleOwner == "" {
		query += ` ORDER BY r.record_id LIMIT $2`
	}
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, ErrV5PlatformUnavailable
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if rows.Scan(&b) != nil {
			return nil, ErrV5PlatformUnavailable
		}
		out = append(out, json.RawMessage(b))
	}
	if rows.Err() != nil {
		return nil, ErrV5PlatformUnavailable
	}
	return out, nil
}
