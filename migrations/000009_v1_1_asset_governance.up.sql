-- CampusOS v1.1: asset lifecycle governance.  This is deliberately append-only
-- and stores no filename, provider path, object key, payload or free-form file metadata.

ALTER TABLE public.user_assets
    DROP CONSTRAINT IF EXISTS chk_user_assets_status;
ALTER TABLE public.user_assets
    ADD CONSTRAINT chk_user_assets_status CHECK (((status)::text = ANY ((ARRAY[
        'active'::character varying,
        'trashed'::character varying,
        'quarantined'::character varying,
        'purging'::character varying,
        'deleted'::character varying
    ])::text[])));

CREATE TABLE public.asset_lifecycle_audits (
    id bigint NOT NULL,
    asset_id bigint,
    actor_user_id bigint,
    actor_type character varying(16) NOT NULL,
    action character varying(48) NOT NULL,
    reason character varying(500),
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT asset_lifecycle_audits_pkey PRIMARY KEY (id),
    CONSTRAINT chk_asset_lifecycle_audits_actor_type CHECK (((actor_type)::text = ANY ((ARRAY['user'::character varying, 'admin'::character varying, 'system'::character varying])::text[]))),
    CONSTRAINT chk_asset_lifecycle_audits_action CHECK (((action)::text = ANY ((ARRAY['trashed'::character varying, 'restored'::character varying, 'quarantined'::character varying, 'purge_started'::character varying, 'purge_failed'::character varying, 'purged'::character varying])::text[]))),
    CONSTRAINT chk_asset_lifecycle_audits_reason CHECK ((reason IS NULL) OR (length(btrim((reason)::text)) > 0))
);

ALTER TABLE ONLY public.asset_lifecycle_audits
    ADD CONSTRAINT fk_asset_lifecycle_audits_asset FOREIGN KEY (asset_id) REFERENCES public.user_assets(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.asset_lifecycle_audits
    ADD CONSTRAINT fk_asset_lifecycle_audits_actor FOREIGN KEY (actor_user_id) REFERENCES public.users(id) ON DELETE SET NULL;

CREATE INDEX idx_asset_lifecycle_audits_created_action
    ON public.asset_lifecycle_audits USING btree (created_at DESC, action);
CREATE INDEX idx_asset_lifecycle_audits_asset_created
    ON public.asset_lifecycle_audits USING btree (asset_id, created_at DESC)
    WHERE asset_id IS NOT NULL;
CREATE INDEX idx_asset_lifecycle_audits_actor_created
    ON public.asset_lifecycle_audits USING btree (actor_user_id, created_at DESC)
    WHERE actor_user_id IS NOT NULL;

INSERT INTO public.permission_definitions
    (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level)
VALUES
    (900000000000001110, 'personal_space.asset.read_audit', 'personal_space', 'asset', 'read_audit', '查看用户资产聚合状态与低敏审计', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001111, 'personal_space.asset.manage', 'personal_space', 'asset', 'manage', '隔离、恢复或清除用户资产', 'high', '["global"]'::jsonb, 'required')
ON CONFLICT (code) DO NOTHING;

INSERT INTO public.role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 910000000000001110 + ROW_NUMBER() OVER (ORDER BY pd.id), r.id, pd.id, 'v1.1-asset-governance', NOW()
FROM public.roles r
JOIN public.permission_definitions pd ON pd.code IN ('personal_space.asset.read_audit', 'personal_space.asset.manage')
WHERE r.name = 'admin' AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;
