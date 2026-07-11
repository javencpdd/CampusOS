-- v0.6 category-scoped moderator enforcement.

-- A global moderator grant cannot be converted safely without knowing which
-- categories the administrator intended. Disable it and require re-assignment
-- through the category moderation UI.
UPDATE user_roles
SET deleted_at = NOW()
WHERE role_id = 2
  AND scope_type = 'global'
  AND scope_id IS NULL
  AND deleted_at IS NULL;

-- Remove broad moderator capabilities that do not belong to category content
-- governance. Existing role rows are retained as soft-deleted history.
UPDATE permissions
SET deleted_at = NOW()
WHERE role_id = 2
  AND deleted_at IS NULL
  AND (
    (resource = 'user' AND action = 'suspend')
    OR (resource = 'thread' AND action IN ('write', 'delete'))
  );

INSERT INTO permissions (id, role_id, resource, action) VALUES
    (34, 1, 'thread', 'lock'),
    (35, 2, 'thread', 'lock')
ON CONFLICT DO NOTHING;

-- Active grants must either be global with no scope_id or category-scoped with
-- a concrete category ID. Historical soft-deleted rows remain readable.
UPDATE user_roles
SET deleted_at = NOW()
WHERE deleted_at IS NULL
  AND NOT COALESCE(
    (
      (
        (scope_type = 'global' AND scope_id IS NULL)
        OR (scope_type = 'category' AND scope_id IS NOT NULL AND scope_id > 0)
      )
      AND (role_id <> 2 OR scope_type = 'category')
    ),
    FALSE
  );

ALTER TABLE user_roles
    ADD CONSTRAINT chk_user_roles_scope_shape
    CHECK (
      deleted_at IS NOT NULL
      OR COALESCE(
        (
          (
            (scope_type = 'global' AND scope_id IS NULL)
            OR (scope_type = 'category' AND scope_id IS NOT NULL AND scope_id > 0)
          )
          AND (role_id <> 2 OR scope_type = 'category')
        ),
        FALSE
      )
    );

CREATE INDEX IF NOT EXISTS idx_user_roles_scope_lookup
    ON user_roles(user_id, scope_type, scope_id, role_id)
    WHERE deleted_at IS NULL;
