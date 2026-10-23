package plugin

import (
	"context"
	"encoding/json"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgV5EventStore struct{ pool *pgxpool.Pool }

func NewPgV5EventStore(pool *pgxpool.Pool) *PgV5EventStore { return &PgV5EventStore{pool} }
func (s *PgV5EventStore) Enqueue(ctx context.Context, d V5EventDelivery) error {
	if s == nil || s.pool == nil || !transaction.Active(ctx) {
		return ErrV5PlatformUnavailable
	}
	if !validV5Delivery(d) {
		return ErrV5Invalid
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return ErrV5Invalid
	}
	_, err = transaction.ExecutorFor(ctx, s.pool).Exec(ctx, `INSERT INTO plugin_v5_event_deliveries(id,state) VALUES($1,$2) ON CONFLICT(source_event_id,instance_id,instance_generation) DO NOTHING`, d.ID, raw)
	if err != nil {
		return ErrV5PlatformUnavailable
	}
	return nil
}
func (s *PgV5EventStore) Claim(ctx context.Context, now int64, limit int) ([]V5EventDelivery, error) {
	limit = v5EventClaimLimit(limit)
	if s == nil || s.pool == nil {
		return nil, ErrV5PlatformUnavailable
	}
	_, err := s.pool.Exec(ctx, `UPDATE plugin_v5_event_deliveries SET state=state||jsonb_build_object('status','dead','error_code','plugin.event_dead_letter') WHERE state->>'status' IN('queued','retry','working') AND (state->>'attempts')::int>=5 AND (state->>'lease_until_ms')::bigint<=$1`, now)
	if err != nil {
		return nil, ErrV5PlatformUnavailable
	}
	rows, err := s.pool.Query(ctx, `WITH due AS (SELECT id FROM plugin_v5_event_deliveries WHERE state->>'status' IN('queued','retry','working') AND (state->>'next_at_ms')::bigint<=$1 AND (state->>'lease_until_ms')::bigint<=$1 AND (state->>'attempts')::int<5 ORDER BY (state->>'next_at_ms')::bigint,id FOR UPDATE SKIP LOCKED LIMIT $2) UPDATE plugin_v5_event_deliveries d SET state=state||jsonb_build_object('status','working','attempts',(state->>'attempts')::int+1,'lease_generation',nextval('plugin_v5_generations')::text,'lease_until_ms',$1::bigint+30000) FROM due WHERE d.id=due.id RETURNING state`, now, limit)
	if err != nil {
		return nil, ErrV5PlatformUnavailable
	}
	defer rows.Close()
	out := []V5EventDelivery{}
	for rows.Next() {
		var raw []byte
		var d V5EventDelivery
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &d) != nil {
			return nil, ErrV5PlatformUnavailable
		}
		out = append(out, d)
	}
	if rows.Err() != nil {
		return nil, ErrV5PlatformUnavailable
	}
	return out, nil
}
func (s *PgV5EventStore) Finish(ctx context.Context, d V5EventDelivery, now int64, status, code string, next int64) error {
	if !validV5EventFinish(status, code, next) {
		return ErrV5Invalid
	}
	if s == nil || s.pool == nil {
		return ErrV5PlatformUnavailable
	}
	tag, err := s.pool.Exec(ctx, `UPDATE plugin_v5_event_deliveries SET state=state||jsonb_build_object('status',$4::text,'error_code',$5::text,'next_at_ms',$6::bigint,'lease_until_ms',0) WHERE id=$1 AND state->>'status'='working' AND (state->>'lease_generation')::bigint=$2 AND (state->>'lease_until_ms')::bigint>$3`, d.ID, d.Lease, now, status, code, next)
	if err != nil {
		return ErrV5PlatformUnavailable
	}
	if tag.RowsAffected() != 1 {
		return ErrV5Conflict
	}
	return nil
}
