-- Development/test-only rollback. Never use this down migration to erase
-- lifecycle audit evidence or user assets in a real environment.

DELETE FROM public.role_permissions
WHERE permission_id IN (
    SELECT id FROM public.permission_definitions
    WHERE code IN ('personal_space.asset.read_audit', 'personal_space.asset.manage')
);
DELETE FROM public.permission_definitions
WHERE code IN ('personal_space.asset.read_audit', 'personal_space.asset.manage');

DROP TABLE IF EXISTS public.asset_lifecycle_audits;

ALTER TABLE public.user_assets
    DROP CONSTRAINT IF EXISTS chk_user_assets_status;
ALTER TABLE public.user_assets
    ADD CONSTRAINT chk_user_assets_status CHECK (((status)::text = ANY ((ARRAY[
        'active'::character varying,
        'trashed'::character varying,
        'quarantined'::character varying,
        'deleted'::character varying
    ])::text[])));
