-- v10 content governance: keep publication, moderation and deletion as
-- separate dimensions while preserving the legacy threads.status column.

ALTER TABLE threads
    ADD COLUMN IF NOT EXISTS publication_status VARCHAR(20) NOT NULL DEFAULT 'published',
    ADD COLUMN IF NOT EXISTS moderation_status VARCHAR(20) NOT NULL DEFAULT 'clear',
    ADD COLUMN IF NOT EXISTS deletion_status VARCHAR(20) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS moderation_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS moderation_by BIGINT NULL,
    ADD COLUMN IF NOT EXISTS moderation_at TIMESTAMP NULL,
    ADD COLUMN IF NOT EXISTS current_revision INTEGER NOT NULL DEFAULT 0;

UPDATE threads
SET publication_status = CASE
        WHEN status = 'draft' THEN 'draft'
        WHEN status = 'private' THEN 'private'
        ELSE 'published'
    END,
    moderation_status = CASE
        WHEN status = 'pending_review' THEN 'pending'
        WHEN status = 'archived' THEN 'taken_down'
        ELSE 'clear'
    END,
    deletion_status = CASE
        WHEN deleted_at IS NULL THEN 'active'
        ELSE 'purged'
    END
WHERE publication_status = 'published'
  AND moderation_status = 'clear'
  AND deletion_status = 'active';

ALTER TABLE threads
    ADD CONSTRAINT chk_threads_publication_status
        CHECK (publication_status IN ('draft', 'published', 'private')),
    ADD CONSTRAINT chk_threads_moderation_status
        CHECK (moderation_status IN ('clear', 'pending', 'rejected', 'taken_down')),
    ADD CONSTRAINT chk_threads_deletion_status
        CHECK (deletion_status IN ('active', 'trashed', 'purged'));

CREATE INDEX IF NOT EXISTS idx_threads_visibility_v10
    ON threads(publication_status, moderation_status, deletion_status, created_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_threads_author_visibility_v10
    ON threads(author_id, deletion_status, updated_at DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS content_revisions (
    id              BIGINT PRIMARY KEY,
    thread_id       BIGINT NOT NULL,
    version         INTEGER NOT NULL,
    title           VARCHAR(255) NOT NULL,
    content         TEXT NOT NULL,
    content_format  VARCHAR(32) NOT NULL,
    tags            TEXT[] NOT NULL DEFAULT '{}',
    action          VARCHAR(64) NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    created_by      BIGINT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT uk_content_revisions_thread_version UNIQUE (thread_id, version)
);

CREATE INDEX IF NOT EXISTS idx_content_revisions_thread_created
    ON content_revisions(thread_id, created_at DESC);

CREATE TABLE IF NOT EXISTS content_moderation_cases (
    id              BIGINT PRIMARY KEY,
    thread_id       BIGINT NOT NULL,
    status          VARCHAR(32) NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    opened_by       BIGINT NULL,
    resolved_by     BIGINT NULL,
    opened_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    resolved_at     TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_content_moderation_cases_open
    ON content_moderation_cases(thread_id, opened_at DESC)
    WHERE resolved_at IS NULL;

CREATE TABLE IF NOT EXISTS content_moderation_actions (
    id              BIGINT PRIMARY KEY,
    case_id         BIGINT NULL,
    thread_id       BIGINT NOT NULL,
    action          VARCHAR(64) NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    actor_id        BIGINT NULL,
    before_state    VARCHAR(96) NOT NULL DEFAULT '',
    after_state     VARCHAR(96) NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_content_moderation_actions_thread_created
    ON content_moderation_actions(thread_id, created_at DESC);

-- RichText keeps a presentation-local status for compatibility. New pending
-- and recoverable-trash states mirror the canonical thread governance state.
ALTER TABLE richtext_article_contents
    DROP CONSTRAINT IF EXISTS chk_richtext_article_status;
ALTER TABLE richtext_article_contents
    ADD CONSTRAINT chk_richtext_article_status
        CHECK (status IN ('draft', 'published', 'pending_review', 'offline', 'trashed', 'deleted'));
