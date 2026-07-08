-- Controlled richtext article plugin storage.
-- CampusOS uses "threads" as the top-level post/article table, so the plugin
-- links richtext article content to threads.thread_id instead of creating a
-- second article primary table.

CREATE TABLE IF NOT EXISTS richtext_article_contents (
    id              BIGINT PRIMARY KEY,
    thread_id       BIGINT NOT NULL UNIQUE,
    title           VARCHAR(255) NOT NULL,
    summary         TEXT NOT NULL DEFAULT '',
    cover_url       TEXT NOT NULL DEFAULT '',
    content_html    TEXT NOT NULL DEFAULT '',
    content_json    JSONB NOT NULL DEFAULT '{}',
    sanitized_html  TEXT NOT NULL DEFAULT '',
    status          VARCHAR(32) NOT NULL DEFAULT 'draft',
    created_by      BIGINT NOT NULL,
    updated_by      BIGINT NULL,
    published_at    TIMESTAMP NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMP NULL,
    CONSTRAINT chk_richtext_article_status CHECK (status IN ('draft', 'published', 'offline', 'deleted'))
);

CREATE INDEX IF NOT EXISTS idx_richtext_article_thread_id
    ON richtext_article_contents(thread_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_richtext_article_created_by
    ON richtext_article_contents(created_by)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_richtext_article_status
    ON richtext_article_contents(status)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS richtext_article_assets (
    id                  BIGINT PRIMARY KEY,
    thread_id           BIGINT NULL,
    article_content_id  BIGINT NULL,
    uploader_id         BIGINT NOT NULL,
    file_url            TEXT NOT NULL,
    file_name           VARCHAR(255) NOT NULL DEFAULT '',
    file_size           BIGINT NOT NULL DEFAULT 0,
    mime_type           VARCHAR(100) NOT NULL DEFAULT '',
    width               INTEGER NOT NULL DEFAULT 0,
    height              INTEGER NOT NULL DEFAULT 0,
    created_at          TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_richtext_assets_thread_id
    ON richtext_article_assets(thread_id)
    WHERE thread_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_richtext_assets_article_content_id
    ON richtext_article_assets(article_content_id)
    WHERE article_content_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_richtext_assets_uploader_id
    ON richtext_article_assets(uploader_id);
