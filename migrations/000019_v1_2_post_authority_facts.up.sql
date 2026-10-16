-- Posts own a nonreused content/authority snapshot and an explicit deletion
-- actor domain. Historical rows have no verified actor evidence: keep unknown.
CREATE SEQUENCE IF NOT EXISTS public.post_facts_version_seq AS bigint NO CYCLE;
ALTER TABLE public.posts
    ADD COLUMN facts_version bigint,
    ADD COLUMN deleted_actor_kind varchar(16) NOT NULL DEFAULT 'legacy_unknown',
    ADD COLUMN deleted_actor_id varchar(128) NOT NULL DEFAULT '';
UPDATE public.posts SET facts_version=nextval('public.post_facts_version_seq');
ALTER TABLE public.posts
    ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_posts_facts_version CHECK (facts_version>0),
    ADD CONSTRAINT chk_posts_deleted_actor_domain CHECK (
        (deleted_actor_kind='legacy_unknown' AND deleted_actor_id='')
        OR (deleted_at IS NOT NULL AND deleted_actor_kind IN ('user','admin') AND deleted_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$')
    );
ALTER SEQUENCE public.post_facts_version_seq OWNED BY public.posts.facts_version;
CREATE FUNCTION public.advance_post_facts_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        NEW.facts_version:=nextval('public.post_facts_version_seq');
    ELSIF ROW(NEW.id,NEW.thread_id,NEW.author_id,NEW.author_name,NEW.parent_id,
        NEW.content,NEW.content_format,NEW.status,NEW.floor_number,NEW.parent_floor_number,
        NEW.metadata,NEW.created_at,NEW.deleted_at,NEW.deleted_actor_kind,NEW.deleted_actor_id)
        IS DISTINCT FROM
        ROW(OLD.id,OLD.thread_id,OLD.author_id,OLD.author_name,OLD.parent_id,
        OLD.content,OLD.content_format,OLD.status,OLD.floor_number,OLD.parent_floor_number,
        OLD.metadata,OLD.created_at,OLD.deleted_at,OLD.deleted_actor_kind,OLD.deleted_actor_id) THEN
        NEW.facts_version:=nextval('public.post_facts_version_seq');
    ELSE
        NEW.facts_version:=OLD.facts_version;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_posts_facts_version BEFORE INSERT OR UPDATE ON public.posts
    FOR EACH ROW EXECUTE FUNCTION public.advance_post_facts_version();
