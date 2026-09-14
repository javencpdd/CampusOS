-- v1.1 cutover guard.  New RichText non-image attachments must use
-- richtext_article_attachments -> user_assets -> storage_objects.  Legacy
-- image rows remain readable while their separate image migration is planned.

ALTER TABLE public.richtext_article_assets
    ADD COLUMN IF NOT EXISTS asset_id bigint;

ALTER TABLE ONLY public.richtext_article_assets
    ADD CONSTRAINT fk_richtext_article_assets_user_asset
    FOREIGN KEY (asset_id) REFERENCES public.user_assets(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_richtext_article_assets_asset_id
    ON public.richtext_article_assets USING btree (asset_id)
    WHERE asset_id IS NOT NULL;
