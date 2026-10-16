-- Stop dependent commands first. Keep original asset/binding rows and manual
-- lifecycle versions, plus allocation high water for any subsequent up.
DROP TRIGGER trg_user_assets_richtext_facts_version ON public.user_assets;
DROP TRIGGER trg_article_attachments_richtext_facts_version ON public.richtext_article_attachments;
DROP FUNCTION public.advance_richtext_asset_binding_facts_version();
ALTER TABLE public.user_assets DROP CONSTRAINT chk_user_assets_facts_version, DROP COLUMN facts_version;
ALTER TABLE public.richtext_article_attachments DROP CONSTRAINT chk_richtext_article_attachments_facts_version, DROP COLUMN facts_version;
