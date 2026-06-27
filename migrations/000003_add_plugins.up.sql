-- 插件系统表
CREATE TABLE IF NOT EXISTS plugins (
    id            BIGINT PRIMARY KEY,
    name          VARCHAR(128) NOT NULL,
    display_name  VARCHAR(255) NOT NULL DEFAULT '',
    version       VARCHAR(32) NOT NULL DEFAULT '0.0.0',
    description   TEXT DEFAULT '',
    author        VARCHAR(128) DEFAULT '',
    runtime       VARCHAR(10) NOT NULL DEFAULT 'grpc',
    manifest      JSONB NOT NULL DEFAULT '{}',
    status        VARCHAR(20) NOT NULL DEFAULT 'installed',
    api_key       VARCHAR(64) DEFAULT '',
    config        JSONB DEFAULT '{}',
    error_message TEXT DEFAULT '',
    installed_by  VARCHAR(128) DEFAULT 'system',
    installed_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_plugins_name ON plugins(name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_plugins_status ON plugins(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_plugins_api_key ON plugins(api_key) WHERE deleted_at IS NULL AND api_key != '';

-- API Key 表（独立管理，支持多 Key）
CREATE TABLE IF NOT EXISTS api_keys (
    id          BIGINT PRIMARY KEY,
    key         VARCHAR(64) NOT NULL,
    name        VARCHAR(128) NOT NULL,
    user_id     BIGINT NULL,
    plugin_name VARCHAR(128) NULL,
    permissions JSONB NOT NULL DEFAULT '[]',
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    last_used_at TIMESTAMP NULL,
    expires_at  TIMESTAMP NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_api_keys_key ON api_keys(key) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_plugin ON api_keys(plugin_name) WHERE deleted_at IS NULL;
