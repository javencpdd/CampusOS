-- Full asset and binding facts remain independent of Article/Core ownership.
-- Preserve the existing lifecycle version, rows, references and constraints.
CREATE SEQUENCE IF NOT EXISTS public.richtext_asset_binding_facts_version_seq AS bigint NO CYCLE;
ALTER TABLE public.user_assets ADD COLUMN facts_version bigint;
ALTER TABLE public.richtext_article_attachments ADD COLUMN facts_version bigint;
UPDATE public.user_assets SET facts_version=nextval('public.richtext_asset_binding_facts_version_seq');
UPDATE public.richtext_article_attachments SET facts_version=nextval('public.richtext_asset_binding_facts_version_seq');
ALTER TABLE public.user_assets ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_user_assets_facts_version CHECK (facts_version>0);
ALTER TABLE public.richtext_article_attachments ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_richtext_article_attachments_facts_version CHECK (facts_version>0);
CREATE FUNCTION public.advance_richtext_asset_binding_facts_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        NEW.facts_version:=nextval('public.richtext_asset_binding_facts_version_seq');
    ELSIF (to_jsonb(NEW)-'facts_version'-'updated_at')
        IS DISTINCT FROM (to_jsonb(OLD)-'facts_version'-'updated_at') THEN
        NEW.facts_version:=nextval('public.richtext_asset_binding_facts_version_seq');
    ELSE
        NEW.facts_version:=OLD.facts_version;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_user_assets_richtext_facts_version BEFORE INSERT OR UPDATE ON public.user_assets
    FOR EACH ROW EXECUTE FUNCTION public.advance_richtext_asset_binding_facts_version();
CREATE TRIGGER trg_article_attachments_richtext_facts_version BEFORE INSERT OR UPDATE ON public.richtext_article_attachments
    FOR EACH ROW EXECUTE FUNCTION public.advance_richtext_asset_binding_facts_version();
