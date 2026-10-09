-- V12-02a rollback: restore the moderator role's two governance codes, then
-- remove the delegation baseline. Rows written after the migration seed must
-- be exported and removed first; a rollback that would silently erase live
-- delegation state is refused.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.identity_delegations
        WHERE created_by <> 'migration-000008'
    ) THEN
        RAISE EXCEPTION 'identity delegation data written after the seed must be exported and removed before rollback'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

INSERT INTO public.role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 920000000000010000 + pd.id, r.id, pd.id, 'v1.2-rollback-000008', NOW()
FROM public.roles r
CROSS JOIN public.permission_definitions pd
WHERE r.name = 'moderator' AND r.deleted_at IS NULL
  AND pd.code IN ('community.thread.take_down', 'community.post.delete') AND pd.deprecated_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM public.role_permissions existing
      WHERE existing.role_id = r.id AND existing.permission_id = pd.id
  );

DROP TABLE public.identity_delegations;
