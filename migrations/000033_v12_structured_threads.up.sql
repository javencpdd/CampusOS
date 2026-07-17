-- v12 C1 structured thread kernel. Thread type is a stable business contract;
-- content_format remains a renderer detail. Historical RichText rows are
-- preserved in place and become article threads without changing IDs or URLs.

DO $$
DECLARE
    issue_details TEXT;
BEGIN
    SELECT string_agg(detail, '; ' ORDER BY detail) INTO issue_details
    FROM (
        SELECT format('richtext article id=%s references missing thread_id=%s', article.id, article.thread_id) AS detail
        FROM richtext_article_contents article
        LEFT JOIN threads thread ON thread.id = article.thread_id
        WHERE thread.id IS NULL
    ) issues;
    IF issue_details IS NOT NULL THEN
        RAISE EXCEPTION 'v12 structured thread preflight failed: %', issue_details
            USING HINT = 'Restore or reconcile the listed RichText/Thread pairs before applying this migration.';
    END IF;
END $$;

ALTER TABLE threads
    ADD COLUMN IF NOT EXISTS thread_type VARCHAR(32) NOT NULL DEFAULT 'discussion';

UPDATE threads
SET thread_type = 'article', updated_at = NOW()
WHERE EXISTS (
    SELECT 1 FROM richtext_article_contents article
    WHERE article.thread_id = threads.id
);

ALTER TABLE threads
    ADD CONSTRAINT chk_threads_thread_type
        CHECK (thread_type IN ('discussion', 'article', 'mutual_aid', 'secondhand')) NOT VALID;
ALTER TABLE threads VALIDATE CONSTRAINT chk_threads_thread_type;

CREATE INDEX IF NOT EXISTS idx_threads_thread_type_created
    ON threads(thread_type, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS category_thread_type_policies (
    category_id BIGINT NOT NULL,
    thread_type VARCHAR(32) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (category_id, thread_type),
    CONSTRAINT fk_category_thread_type_policy_category
        FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE RESTRICT,
    CONSTRAINT chk_category_thread_type_policy_type
        CHECK (thread_type IN ('discussion', 'article', 'mutual_aid', 'secondhand')),
    CONSTRAINT chk_category_thread_type_policy_enabled
        CHECK (enabled = TRUE OR enabled = FALSE)
);

CREATE INDEX IF NOT EXISTS idx_category_thread_type_policies_enabled
    ON category_thread_type_policies(category_id, thread_type)
    WHERE enabled = TRUE;

-- Only board nodes receive posting policies. Existing boards keep the two
-- historical kinds; mutual aid and secondhand remain opt-in per board.
INSERT INTO category_thread_type_policies (category_id, thread_type, enabled, created_at, updated_at)
SELECT category.id, kinds.thread_type, TRUE, NOW(), NOW()
FROM categories category
CROSS JOIN (VALUES ('discussion'::VARCHAR(32)), ('article'::VARCHAR(32))) AS kinds(thread_type)
WHERE category.deleted_at IS NULL
  AND category.node_kind = 'board'
ON CONFLICT (category_id, thread_type) DO NOTHING;

CREATE OR REPLACE FUNCTION campusos_guard_category_thread_type_policy()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    category_kind VARCHAR(16);
BEGIN
    SELECT node_kind INTO category_kind
    FROM categories
    WHERE id = NEW.category_id AND deleted_at IS NULL
    FOR KEY SHARE;
    IF NOT FOUND OR category_kind <> 'board' THEN
        RAISE EXCEPTION 'thread type policy requires an existing board category';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_category_thread_type_policy_guard ON category_thread_type_policies;
CREATE TRIGGER trg_category_thread_type_policy_guard
BEFORE INSERT OR UPDATE OF category_id, thread_type
ON category_thread_type_policies
FOR EACH ROW EXECUTE FUNCTION campusos_guard_category_thread_type_policy();

INSERT INTO permission_definitions (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, created_at, updated_at) VALUES
    (900000000000001035, 'community.category.configure_thread_types', 'community', 'category', 'configure_thread_types', '配置板块允许发布的帖子类型', 'high', '["global"]'::jsonb, 'required', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000205000 + ROW_NUMBER() OVER (ORDER BY pd.id), r.id, pd.id, 'v12-structured-thread-admin', NOW()
FROM roles r
INNER JOIN permission_definitions pd ON pd.code = 'community.category.configure_thread_types'
WHERE r.name = 'admin' AND r.deleted_at IS NULL
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;
