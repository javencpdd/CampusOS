-- Personal PDF invocations are short-lived contexts. They cannot be expressed
-- by the older mandatory-article schema, so a rollback safely removes only
-- those ephemeral contexts before restoring the former NOT NULL contract.

DELETE FROM public.plugin_ui_invocations
WHERE context_kind = 'personal_asset';

ALTER TABLE public.plugin_ui_invocations
    DROP CONSTRAINT IF EXISTS chk_plugin_ui_invocations_context_kind;

ALTER TABLE public.plugin_ui_invocations
    DROP COLUMN IF EXISTS context_kind;

ALTER TABLE public.plugin_ui_invocations
    ALTER COLUMN article_content_id SET NOT NULL;
