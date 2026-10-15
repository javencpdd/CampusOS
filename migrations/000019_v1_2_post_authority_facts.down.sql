-- There is no legacy deletion-actor column; refuse to discard typed history.
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM public.posts WHERE deleted_actor_kind<>'legacy_unknown' OR deleted_actor_id<>'') THEN
        RAISE EXCEPTION 'post facts rollback would lose deletion actor history';
    END IF;
END;
$$;
DROP TRIGGER trg_posts_facts_version ON public.posts;
DROP FUNCTION public.advance_post_facts_version();
ALTER SEQUENCE public.post_facts_version_seq OWNED BY NONE;
ALTER TABLE public.posts
    DROP CONSTRAINT chk_posts_deleted_actor_domain,
    DROP CONSTRAINT chk_posts_facts_version,
    DROP COLUMN deleted_actor_kind,
    DROP COLUMN deleted_actor_id,
    DROP COLUMN facts_version;
