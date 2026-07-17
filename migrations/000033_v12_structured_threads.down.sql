-- Isolated drill only. Production retains the structured-thread contract and
-- fixes forward; this down migration exists to verify restore mechanics.

DELETE FROM role_permissions
WHERE created_by = 'v12-structured-thread-admin';

DELETE FROM permission_definitions
WHERE code = 'community.category.configure_thread_types';

DROP TRIGGER IF EXISTS trg_category_thread_type_policy_guard ON category_thread_type_policies;
DROP FUNCTION IF EXISTS campusos_guard_category_thread_type_policy();
DROP INDEX IF EXISTS idx_category_thread_type_policies_enabled;
DROP TABLE IF EXISTS category_thread_type_policies;

DROP INDEX IF EXISTS idx_threads_thread_type_created;
ALTER TABLE threads DROP CONSTRAINT IF EXISTS chk_threads_thread_type;
ALTER TABLE threads DROP COLUMN IF EXISTS thread_type;
