-- Isolated drill only. Production keeps category lifecycle and audit evidence
-- and uses forward-fix rather than dropping hierarchy semantics.

DELETE FROM role_permissions
WHERE created_by IN ('v12-category-legacy', 'v12-category-admin');

DELETE FROM permission_definitions
WHERE code IN (
    'community.category.create', 'community.category.update', 'community.category.move',
    'community.category.archive', 'community.category.restore'
);

DROP TRIGGER IF EXISTS trg_categories_hierarchy_guard ON categories;
DROP FUNCTION IF EXISTS campusos_guard_category_hierarchy();

DROP INDEX IF EXISTS idx_categories_lifecycle_kind;
DROP INDEX IF EXISTS idx_categories_parent_active;

ALTER TABLE categories
    DROP CONSTRAINT IF EXISTS chk_categories_group_root,
    DROP CONSTRAINT IF EXISTS chk_categories_color,
    DROP CONSTRAINT IF EXISTS chk_categories_version,
    DROP CONSTRAINT IF EXISTS chk_categories_lifecycle_status,
    DROP CONSTRAINT IF EXISTS chk_categories_node_kind;

ALTER TABLE categories
    DROP COLUMN IF EXISTS color,
    DROP COLUMN IF EXISTS version,
    DROP COLUMN IF EXISTS lifecycle_status,
    DROP COLUMN IF EXISTS node_kind;
