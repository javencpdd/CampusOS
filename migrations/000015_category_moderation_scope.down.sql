DROP INDEX IF EXISTS idx_user_roles_scope_lookup;

ALTER TABLE user_roles
    DROP CONSTRAINT IF EXISTS chk_user_roles_scope_shape;

DELETE FROM permissions
WHERE id IN (34, 35)
  AND resource = 'thread'
  AND action = 'lock';

UPDATE permissions
SET deleted_at = NULL
WHERE role_id = 2
  AND (
    (resource = 'user' AND action = 'suspend')
    OR (resource = 'thread' AND action IN ('write', 'delete'))
  );

UPDATE user_roles
SET deleted_at = NULL
WHERE role_id = 2
  AND scope_type = 'global'
  AND scope_id IS NULL
  AND deleted_at IS NOT NULL;
