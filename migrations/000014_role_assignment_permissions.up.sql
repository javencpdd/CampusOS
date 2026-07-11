-- v0.6 RBAC role-assignment repair and granular role-management permissions.
-- Existing migrations use manually generated BIGINT identifiers, not PostgreSQL sequences.

-- Earlier uniqueness allowed duplicate global rows because PostgreSQL treats NULL values
-- as distinct in the original (user_id, role_id, scope_type, scope_id) index.
WITH ranked_global_roles AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY user_id, role_id, scope_type
               ORDER BY created_at ASC, id ASC
           ) AS row_number
    FROM user_roles
    WHERE deleted_at IS NULL AND scope_id IS NULL
)
UPDATE user_roles
SET deleted_at = NOW()
WHERE id IN (
    SELECT id FROM ranked_global_roles WHERE row_number > 1
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_user_roles_global_active
    ON user_roles(user_id, role_id, scope_type)
    WHERE deleted_at IS NULL AND scope_id IS NULL;

-- Split role management into read, assign, and revoke. Keep role:manage for
-- existing unrelated administration endpoints until their own v0.6 migration.
INSERT INTO permissions (id, role_id, resource, action) VALUES
    (31, 1, 'role', 'read'),
    (32, 1, 'role', 'assign'),
    (33, 1, 'role', 'revoke')
ON CONFLICT DO NOTHING;
