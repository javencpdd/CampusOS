-- 用户个人主页同步内容

CREATE TABLE IF NOT EXISTS user_space_contents (
    id                 BIGINT PRIMARY KEY,
    user_id            BIGINT NOT NULL,
    thread_id          BIGINT NOT NULL,
    title              VARCHAR(255) NOT NULL,
    excerpt            TEXT NOT NULL DEFAULT '',
    author_name        VARCHAR(64) NOT NULL DEFAULT '',
    category_id        BIGINT NOT NULL,
    tags               TEXT[] NOT NULL DEFAULT '{}',
    status             VARCHAR(20) NOT NULL DEFAULT 'published',
    thread_created_at  TIMESTAMP NOT NULL,
    thread_updated_at  TIMESTAMP NOT NULL,
    synced_at          TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at         TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_user_space_contents_thread_id
    ON user_space_contents(thread_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_space_contents_user_created
    ON user_space_contents(user_id, thread_created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_space_contents_category
    ON user_space_contents(category_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_space_contents_tags
    ON user_space_contents USING GIN(tags)
    WHERE deleted_at IS NULL;
