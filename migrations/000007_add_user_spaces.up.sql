-- 用户个人主页配置

CREATE TABLE IF NOT EXISTS user_spaces (
    id               BIGINT PRIMARY KEY,
    user_id          BIGINT NOT NULL,
    title            VARCHAR(120) NOT NULL DEFAULT '',
    bio              VARCHAR(500) NOT NULL DEFAULT '',
    avatar           VARCHAR(512) NOT NULL DEFAULT '',
    cover_image      VARCHAR(512) NOT NULL DEFAULT '',
    theme            VARCHAR(64) NOT NULL DEFAULT 'default',
    layout           VARCHAR(64) NOT NULL DEFAULT 'blog',
    visibility       VARCHAR(20) NOT NULL DEFAULT 'public',
    sync_enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    sync_categories  TEXT[] NOT NULL DEFAULT '{}',
    sync_tags        TEXT[] NOT NULL DEFAULT '{}',
    created_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at       TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_user_spaces_user_id
    ON user_spaces(user_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_spaces_visibility
    ON user_spaces(visibility)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_spaces_updated_at
    ON user_spaces(updated_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_spaces_sync_categories
    ON user_spaces USING GIN(sync_categories)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_spaces_sync_tags
    ON user_spaces USING GIN(sync_tags)
    WHERE deleted_at IS NULL;
