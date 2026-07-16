-- Down migration removes v10 governance structures. It intentionally cannot
-- reconstruct revision and moderation history after rollback.
ALTER TABLE richtext_article_contents
    DROP CONSTRAINT IF EXISTS chk_richtext_article_status;
ALTER TABLE richtext_article_contents
    ADD CONSTRAINT chk_richtext_article_status
        CHECK (status IN ('draft', 'published', 'offline', 'deleted'));

DROP TABLE IF EXISTS content_moderation_actions;
DROP TABLE IF EXISTS content_moderation_cases;
DROP TABLE IF EXISTS content_revisions;

DROP INDEX IF EXISTS idx_threads_author_visibility_v10;
DROP INDEX IF EXISTS idx_threads_visibility_v10;

ALTER TABLE threads
    DROP CONSTRAINT IF EXISTS chk_threads_deletion_status,
    DROP CONSTRAINT IF EXISTS chk_threads_moderation_status,
    DROP CONSTRAINT IF EXISTS chk_threads_publication_status;
ALTER TABLE threads
    DROP COLUMN IF EXISTS current_revision,
    DROP COLUMN IF EXISTS moderation_at,
    DROP COLUMN IF EXISTS moderation_by,
    DROP COLUMN IF EXISTS moderation_reason,
    DROP COLUMN IF EXISTS deletion_status,
    DROP COLUMN IF EXISTS moderation_status,
    DROP COLUMN IF EXISTS publication_status;
