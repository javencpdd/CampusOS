-- 插件系统稳定化：权限声明与运行日志

CREATE TABLE IF NOT EXISTS plugin_permissions (
    id               BIGINT PRIMARY KEY,
    plugin_name      VARCHAR(128) NOT NULL,
    permission_type  VARCHAR(64) NOT NULL,
    permission_value VARCHAR(255) NOT NULL,
    created_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at       TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_plugin_permissions
    ON plugin_permissions(plugin_name, permission_type, permission_value)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_plugin_permissions_plugin
    ON plugin_permissions(plugin_name)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS plugin_logs (
    id          BIGINT PRIMARY KEY,
    plugin_name VARCHAR(128) NOT NULL,
    level       VARCHAR(16) NOT NULL DEFAULT 'info',
    message     TEXT NOT NULL,
    event_type  VARCHAR(128) DEFAULT '',
    trace_id    VARCHAR(64) DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_plugin_logs_plugin_created
    ON plugin_logs(plugin_name, created_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_plugin_logs_level
    ON plugin_logs(level)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_plugin_logs_trace_id
    ON plugin_logs(trace_id)
    WHERE deleted_at IS NULL AND trace_id != '';
