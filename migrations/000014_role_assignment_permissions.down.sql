DELETE FROM permissions
WHERE id IN (31, 32, 33)
  AND role_id = 1
  AND resource = 'role'
  AND action IN ('read', 'assign', 'revoke');

DROP INDEX IF EXISTS uk_user_roles_global_active;
