-- Allow the existing first-party PDF Viewer Surface to receive an owner-only
-- personal-asset context. Invocation IDs remain opaque and never contain a
-- storage key, provider path, JWT, or a public download URL.

ALTER TABLE public.plugin_ui_invocations
    ADD COLUMN context_kind character varying(32) NOT NULL DEFAULT 'article_attachment';

ALTER TABLE public.plugin_ui_invocations
    ALTER COLUMN article_content_id DROP NOT NULL;

ALTER TABLE public.plugin_ui_invocations
    ADD CONSTRAINT chk_plugin_ui_invocations_context_kind
    CHECK (
        (
            context_kind = 'article_attachment'
            AND article_content_id IS NOT NULL
            AND asset_id IS NOT NULL
            AND attachment_id IS NOT NULL
        )
        OR
        (
            context_kind = 'personal_asset'
            AND article_content_id IS NULL
            AND asset_id IS NOT NULL
            AND attachment_id IS NULL
        )
    );
