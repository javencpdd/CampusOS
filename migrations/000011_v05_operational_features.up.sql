-- v0.5 运营化与低风险集成基础

ALTER TABLE user_spaces
    ADD COLUMN IF NOT EXISTS disabled_at TIMESTAMP NULL,
    ADD COLUMN IF NOT EXISTS disabled_by VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS disabled_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_sync_at TIMESTAMP NULL,
    ADD COLUMN IF NOT EXISTS last_sync_error TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_user_spaces_disabled
    ON user_spaces(disabled_at)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS user_space_style_snapshots (
    id             BIGINT PRIMARY KEY,
    user_id        VARCHAR(64) NOT NULL,
    snapshot_type  VARCHAR(32) NOT NULL DEFAULT 'before_apply',
    style_name     VARCHAR(64) NOT NULL DEFAULT '',
    style_version  VARCHAR(32) NOT NULL DEFAULT '',
    theme          VARCHAR(64) NOT NULL DEFAULT 'default',
    layout         VARCHAR(64) NOT NULL DEFAULT 'blog',
    style_manifest JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_space_style_snapshots_user_created
    ON user_space_style_snapshots(user_id, created_at DESC);

ALTER TABLE plugins
    ADD COLUMN IF NOT EXISTS checksum VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS package_size BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_preflight_at TIMESTAMP NULL;

CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id             BIGINT PRIMARY KEY,
    name           VARCHAR(128) NOT NULL,
    url            TEXT NOT NULL,
    secret         VARCHAR(255) NOT NULL DEFAULT '',
    events         TEXT[] NOT NULL DEFAULT '{}',
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    max_retries    INTEGER NOT NULL DEFAULT 2,
    timeout_ms     INTEGER NOT NULL DEFAULT 5000,
    created_by     VARCHAR(64) NOT NULL DEFAULT '',
    created_at     TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_webhook_endpoints_enabled
    ON webhook_endpoints(enabled)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id              BIGINT PRIMARY KEY,
    endpoint_id     BIGINT NOT NULL,
    event_id        VARCHAR(128) NOT NULL DEFAULT '',
    event_type      VARCHAR(128) NOT NULL DEFAULT '',
    target_url      TEXT NOT NULL DEFAULT '',
    status          VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    response_status INTEGER NOT NULL DEFAULT 0,
    error_message   TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_endpoint_created
    ON webhook_deliveries(endpoint_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_status
    ON webhook_deliveries(status);

CREATE TABLE IF NOT EXISTS mcp_audit_logs (
    id          BIGINT PRIMARY KEY,
    user_id     VARCHAR(64) NOT NULL DEFAULT '',
    tool        VARCHAR(128) NOT NULL,
    arguments   JSONB NOT NULL DEFAULT '{}',
    success     BOOLEAN NOT NULL DEFAULT FALSE,
    error       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mcp_audit_logs_created
    ON mcp_audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_mcp_audit_logs_tool
    ON mcp_audit_logs(tool, created_at DESC);

CREATE TABLE IF NOT EXISTS message_bindings (
    id                BIGINT PRIMARY KEY,
    user_id           VARCHAR(64) NOT NULL,
    platform          VARCHAR(64) NOT NULL,
    external_user_id  VARCHAR(128) NOT NULL,
    display_name      VARCHAR(128) NOT NULL DEFAULT '',
    created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at        TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_message_bindings_platform_external
    ON message_bindings(platform, external_user_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS message_logs (
    id              BIGINT PRIMARY KEY,
    platform        VARCHAR(64) NOT NULL,
    conversation_id VARCHAR(128) NOT NULL DEFAULT '',
    sender_id       VARCHAR(128) NOT NULL DEFAULT '',
    direction       VARCHAR(16) NOT NULL DEFAULT 'inbound',
    message_type    VARCHAR(32) NOT NULL DEFAULT 'text',
    content         TEXT NOT NULL DEFAULT '',
    raw_payload     JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_message_logs_created
    ON message_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_message_logs_platform_conversation
    ON message_logs(platform, conversation_id, created_at DESC);
