-- v12 B1/B2 Category ownership. This migration strengthens the existing
-- categories table in place. It does not create a parallel navigation model
-- and it aborts instead of silently flattening malformed historical trees.

DO $$
DECLARE
    issue_details TEXT;
BEGIN
    SELECT string_agg(detail, '; ' ORDER BY detail) INTO issue_details
    FROM (
        WITH RECURSIVE lineage AS (
            SELECT c.id, c.parent_id, ARRAY[c.id]::BIGINT[] AS path, FALSE AS cycle, 0 AS depth
            FROM categories c
            WHERE c.deleted_at IS NULL

            UNION ALL

            SELECT parent.id, parent.parent_id, lineage.path || parent.id,
                   parent.id = ANY(lineage.path), lineage.depth + 1
            FROM lineage
            INNER JOIN categories parent ON parent.id = lineage.parent_id
            WHERE parent.deleted_at IS NULL AND NOT lineage.cycle
        )
        SELECT DISTINCT format('category cycle detected at id=%s path=%s', id, path::text) AS detail
        FROM lineage
        WHERE cycle

        UNION ALL

        SELECT DISTINCT format('category hierarchy exceeds two levels at id=%s path=%s', id, path::text)
        FROM lineage
        WHERE depth >= 2

        UNION ALL

        SELECT format('category parent is missing or deleted child_id=%s parent_id=%s', child.id, child.parent_id)
        FROM categories child
        LEFT JOIN categories parent ON parent.id = child.parent_id AND parent.deleted_at IS NULL
        WHERE child.deleted_at IS NULL AND child.parent_id IS NOT NULL AND parent.id IS NULL
    ) issues;

    IF issue_details IS NOT NULL THEN
        RAISE EXCEPTION 'v12 category hierarchy preflight failed: %', issue_details
            USING HINT = 'Resolve the listed category IDs manually; this migration never flattens or rewires a hierarchy.';
    END IF;
END $$;

ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS node_kind VARCHAR(16) NOT NULL DEFAULT 'board',
    ADD COLUMN IF NOT EXISTS lifecycle_status VARCHAR(16) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS color VARCHAR(9) NOT NULL DEFAULT '';

-- Existing parents become groups. Every remaining historical leaf remains a
-- board, preserving its existing posting semantics.
UPDATE categories parent
SET node_kind = 'group', updated_at = NOW()
WHERE parent.deleted_at IS NULL
  AND EXISTS (
      SELECT 1 FROM categories child
      WHERE child.parent_id = parent.id AND child.deleted_at IS NULL
  );

UPDATE categories
SET node_kind = CASE WHEN node_kind = 'group' THEN 'group' ELSE 'board' END,
    lifecycle_status = CASE WHEN lifecycle_status = 'archived' THEN 'archived' ELSE 'active' END,
    version = GREATEST(COALESCE(version, 1), 1),
    color = COALESCE(color, '')
WHERE deleted_at IS NULL;

ALTER TABLE categories
    ADD CONSTRAINT chk_categories_node_kind
        CHECK (node_kind IN ('group', 'board')) NOT VALID,
    ADD CONSTRAINT chk_categories_lifecycle_status
        CHECK (lifecycle_status IN ('active', 'archived')) NOT VALID,
    ADD CONSTRAINT chk_categories_version
        CHECK (version >= 1) NOT VALID,
    ADD CONSTRAINT chk_categories_color
        CHECK (color = '' OR color ~ '^#[0-9A-Fa-f]{6}([0-9A-Fa-f]{2})?$') NOT VALID,
    ADD CONSTRAINT chk_categories_group_root
        CHECK (node_kind = 'board' OR parent_id IS NULL) NOT VALID;
ALTER TABLE categories VALIDATE CONSTRAINT chk_categories_node_kind;
ALTER TABLE categories VALIDATE CONSTRAINT chk_categories_lifecycle_status;
ALTER TABLE categories VALIDATE CONSTRAINT chk_categories_version;
ALTER TABLE categories VALIDATE CONSTRAINT chk_categories_color;
ALTER TABLE categories VALIDATE CONSTRAINT chk_categories_group_root;

CREATE INDEX IF NOT EXISTS idx_categories_parent_active
    ON categories(parent_id, sort_order, created_at)
    WHERE deleted_at IS NULL AND lifecycle_status = 'active';
CREATE INDEX IF NOT EXISTS idx_categories_lifecycle_kind
    ON categories(lifecycle_status, node_kind, sort_order, created_at)
    WHERE deleted_at IS NULL;

-- A row-level trigger protects the parts of the two-level invariant that
-- cannot be represented by a simple CHECK: a board cannot own children and a
-- child can only be attached to an active root group.
CREATE OR REPLACE FUNCTION campusos_guard_category_hierarchy()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    parent_kind VARCHAR(16);
    parent_status VARCHAR(16);
BEGIN
    IF NEW.deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.node_kind = 'group' AND NEW.parent_id IS NOT NULL THEN
        RAISE EXCEPTION 'category group must be a root node';
    END IF;

    IF NEW.parent_id IS NOT NULL THEN
        SELECT node_kind, lifecycle_status INTO parent_kind, parent_status
        FROM categories
        WHERE id = NEW.parent_id AND deleted_at IS NULL
        FOR KEY SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'category parent is unavailable';
        END IF;
        IF parent_kind <> 'group' THEN
            RAISE EXCEPTION 'category parent must be an active group';
        END IF;
        IF parent_status <> 'active' THEN
            RAISE EXCEPTION 'category parent is archived';
        END IF;
    END IF;

    IF NEW.node_kind <> 'group' AND EXISTS (
        SELECT 1 FROM categories child
        WHERE child.parent_id = NEW.id AND child.deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'category board cannot own child nodes';
    END IF;

    IF NEW.lifecycle_status = 'archived' AND NEW.node_kind = 'group' AND EXISTS (
        SELECT 1 FROM categories child
        WHERE child.parent_id = NEW.id
          AND child.deleted_at IS NULL
          AND child.lifecycle_status = 'active'
    ) THEN
        RAISE EXCEPTION 'archive or move active child boards before archiving a group';
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_categories_hierarchy_guard ON categories;
CREATE TRIGGER trg_categories_hierarchy_guard
BEFORE INSERT OR UPDATE OF parent_id, node_kind, lifecycle_status, deleted_at
ON categories
FOR EACH ROW EXECUTE FUNCTION campusos_guard_category_hierarchy();

-- New routes use stable category-operation codes. Existing category:write and
-- category:delete grants are translated to the closest non-destructive codes
-- during the compatibility window; archive/move/restore remain admin-only.
INSERT INTO permission_definitions (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, created_at, updated_at) VALUES
    (900000000000001030, 'community.category.create', 'community', 'category', 'create', '创建版块或分组', 'medium', '["global"]'::jsonb, 'standard', NOW(), NOW()),
    (900000000000001031, 'community.category.update', 'community', 'category', 'update', '更新版块展示与基础设置', 'medium', '["global"]'::jsonb, 'standard', NOW(), NOW()),
    (900000000000001032, 'community.category.move', 'community', 'category', 'move', '移动版块层级', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001033, 'community.category.archive', 'community', 'category', 'archive', '归档版块或分组', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001034, 'community.category.restore', 'community', 'category', 'restore', '恢复已归档版块或分组', 'high', '["global"]'::jsonb, 'required', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000203000 + ROW_NUMBER() OVER (ORDER BY p.role_id, pd.id), p.role_id, pd.id, 'v12-category-legacy', NOW()
FROM permissions p
INNER JOIN permission_definitions pd ON (
    (p.resource = 'category' AND p.action = 'write' AND pd.code IN ('community.category.create', 'community.category.update'))
    OR (p.resource = 'category' AND p.action = 'delete' AND pd.code = 'community.category.archive')
)
WHERE p.deleted_at IS NULL
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000204000 + ROW_NUMBER() OVER (ORDER BY pd.id), r.id, pd.id, 'v12-category-admin', NOW()
FROM roles r
INNER JOIN permission_definitions pd ON pd.code IN (
    'community.category.create', 'community.category.update', 'community.category.move',
    'community.category.archive', 'community.category.restore'
)
WHERE r.name = 'admin' AND r.deleted_at IS NULL
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;
