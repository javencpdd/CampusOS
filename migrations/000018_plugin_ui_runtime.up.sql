ALTER TABLE plugins
    ADD COLUMN IF NOT EXISTS backend_state VARCHAR(32) NOT NULL DEFAULT 'installed',
    ADD COLUMN IF NOT EXISTS frontend_state VARCHAR(32) NOT NULL DEFAULT 'unloaded',
    ADD COLUMN IF NOT EXISTS health_state VARCHAR(32) NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS ui_revision BIGINT NOT NULL DEFAULT 0;

ALTER TABLE plugins
    DROP CONSTRAINT IF EXISTS chk_plugins_backend_state,
    DROP CONSTRAINT IF EXISTS chk_plugins_frontend_state,
    DROP CONSTRAINT IF EXISTS chk_plugins_health_state;

ALTER TABLE plugins
    ADD CONSTRAINT chk_plugins_backend_state CHECK (backend_state IN ('installed', 'starting', 'running', 'restarting', 'stopping', 'stopped', 'pending_restart', 'error')),
    ADD CONSTRAINT chk_plugins_frontend_state CHECK (frontend_state IN ('unloaded', 'loading', 'loaded', 'incompatible', 'error')),
    ADD CONSTRAINT chk_plugins_health_state CHECK (health_state IN ('healthy', 'degraded', 'unavailable', 'unknown'));

CREATE INDEX IF NOT EXISTS idx_plugins_runtime_state
    ON plugins(backend_state, frontend_state, health_state)
    WHERE deleted_at IS NULL;
