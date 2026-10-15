-- RichText owns its full content/metadata/actor snapshot independently of Core.
CREATE SEQUENCE IF NOT EXISTS public.article_facts_version_seq AS bigint NO CYCLE;
ALTER TABLE public.richtext_article_contents ADD COLUMN facts_version bigint;
UPDATE public.richtext_article_contents SET facts_version=nextval('public.article_facts_version_seq');
ALTER TABLE public.richtext_article_contents
    ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_richtext_article_facts_version CHECK (facts_version>0);
ALTER SEQUENCE public.article_facts_version_seq OWNED BY public.richtext_article_contents.facts_version;
CREATE FUNCTION public.advance_article_facts_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        NEW.facts_version:=nextval('public.article_facts_version_seq');
    ELSIF ROW(NEW.id,NEW.thread_id,NEW.title,NEW.summary,NEW.cover_url,
        NEW.content_html,NEW.content_json,NEW.sanitized_html,NEW.status,
        NEW.created_by,NEW.updated_by,NEW.published_at,NEW.created_at,NEW.deleted_at,
        NEW.updated_actor_kind,NEW.updated_actor_id)
        IS DISTINCT FROM
        ROW(OLD.id,OLD.thread_id,OLD.title,OLD.summary,OLD.cover_url,
        OLD.content_html,OLD.content_json,OLD.sanitized_html,OLD.status,
        OLD.created_by,OLD.updated_by,OLD.published_at,OLD.created_at,OLD.deleted_at,
        OLD.updated_actor_kind,OLD.updated_actor_id) THEN
        NEW.facts_version:=nextval('public.article_facts_version_seq');
    ELSE
        NEW.facts_version:=OLD.facts_version;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_article_facts_version BEFORE INSERT OR UPDATE ON public.richtext_article_contents
    FOR EACH ROW EXECUTE FUNCTION public.advance_article_facts_version();
