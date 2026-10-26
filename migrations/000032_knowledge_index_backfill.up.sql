-- Durable enumeration progress only; source rows and requests remain unchanged.
CREATE TABLE knowledge_index_backfill_checkpoints (
    target_kind text PRIMARY KEY CHECK (target_kind IN ('collection','official')),
    after_id text NOT NULL DEFAULT '' CHECK (
        after_id='' OR
        (target_kind='collection' AND after_id ~ '^kc_[0-9a-f]{32}$') OR
        (target_kind='official' AND after_id ~ '^ko_[0-9a-f]{32}$')
    ),
    completed_passes bigint NOT NULL DEFAULT 0 CHECK (completed_passes>=0),
    next_scan_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
-- No seed, FK, body, permission copy, event ID cursor, lease or ready projection.
