DROP INDEX IF EXISTS public.idx_richtext_article_assets_asset_id;
ALTER TABLE ONLY public.richtext_article_assets
    DROP CONSTRAINT IF EXISTS fk_richtext_article_assets_user_asset;
ALTER TABLE public.richtext_article_assets
    DROP COLUMN IF EXISTS asset_id;
