-- Community owns a complete authorization/content version independently from
-- the author's content revision. Sequence allocations survive rollback, so
-- restoring a previous state or deleting/reinserting an ID cannot reuse one.
-- The sequence intentionally survives down/up to retain its high-water mark.
CREATE SEQUENCE IF NOT EXISTS public.thread_facts_version_seq AS bigint NO CYCLE;

ALTER TABLE public.threads
    ADD COLUMN facts_version bigint;
UPDATE public.threads
    SET facts_version = nextval('public.thread_facts_version_seq');
ALTER TABLE public.threads
    ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_threads_facts_version CHECK (facts_version > 0);
ALTER SEQUENCE public.thread_facts_version_seq OWNED BY public.threads.facts_version;

CREATE FUNCTION public.advance_thread_facts_version() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.facts_version := nextval('public.thread_facts_version_seq');
    ELSIF ROW(NEW.id, NEW.author_id, NEW.author_name, NEW.category_id,
              NEW.publication_status, NEW.moderation_status, NEW.deletion_status,
              NEW.deleted_at, NEW.title, NEW.content, NEW.content_format,
              NEW.thread_type, NEW.current_revision, NEW.tags, NEW.status,
              NEW.moderation_reason, NEW.moderation_by, NEW.moderation_at,
              NEW.is_pinned, NEW.is_locked, NEW.is_highlighted, NEW.created_at)
          IS DISTINCT FROM
          ROW(OLD.id, OLD.author_id, OLD.author_name, OLD.category_id,
              OLD.publication_status, OLD.moderation_status, OLD.deletion_status,
              OLD.deleted_at, OLD.title, OLD.content, OLD.content_format,
              OLD.thread_type, OLD.current_revision, OLD.tags, OLD.status,
              OLD.moderation_reason, OLD.moderation_by, OLD.moderation_at,
              OLD.is_pinned, OLD.is_locked, OLD.is_highlighted, OLD.created_at) THEN
        NEW.facts_version := nextval('public.thread_facts_version_seq');
    ELSE
        -- Ignore a caller-provided version even when no facts changed.
        NEW.facts_version := OLD.facts_version;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_threads_facts_version
    BEFORE INSERT OR UPDATE ON public.threads
    FOR EACH ROW EXECUTE FUNCTION public.advance_thread_facts_version();
