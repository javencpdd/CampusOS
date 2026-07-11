DROP INDEX IF EXISTS idx_plugins_runtime_state;

ALTER TABLE plugins
    DROP CONSTRAINT IF EXISTS chk_plugins_backend_state,
    DROP CONSTRAINT IF EXISTS chk_plugins_frontend_state,
    DROP CONSTRAINT IF EXISTS chk_plugins_health_state,
    DROP COLUMN IF EXISTS ui_revision,
    DROP COLUMN IF EXISTS health_state,
    DROP COLUMN IF EXISTS frontend_state,
    DROP COLUMN IF EXISTS backend_state;
