-- Preserve legacy user references and provenance; authenticated domain actors use new pairs.
-- These columns are storage facts, not authentication or authorization.

CREATE FUNCTION public.sync_business_actor_pair() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    current_row jsonb := to_jsonb(NEW);
    old_row jsonb;
    legacy_id text := current_row ->> TG_ARGV[0];
    actor_kind text := current_row ->> TG_ARGV[1];
    actor_ref text := current_row ->> TG_ARGV[2];
    infer_legacy boolean := false;
BEGIN
    IF TG_OP = 'INSERT' THEN
        infer_legacy := actor_kind = 'legacy_unknown' AND actor_ref = '';
    ELSE
        old_row := to_jsonb(OLD);
        infer_legacy := actor_kind IS NOT DISTINCT FROM (old_row ->> TG_ARGV[1])
            AND actor_ref IS NOT DISTINCT FROM (old_row ->> TG_ARGV[2])
            AND legacy_id IS DISTINCT FROM (old_row ->> TG_ARGV[0]);
        -- FK SET NULL must retain the already-known historical User identity.
        IF infer_legacy AND TG_ARGV[3] = 'numeric' AND legacy_id IS NULL
            AND actor_kind = 'user' THEN
            infer_legacy := false;
        END IF;
    END IF;
    IF infer_legacy THEN
        actor_ref := COALESCE(legacy_id, '');
        actor_kind := CASE WHEN TG_ARGV[3] = 'numeric'
            AND actor_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
            THEN 'user' ELSE 'legacy_unknown' END;
        NEW := jsonb_populate_record(NEW, jsonb_build_object(TG_ARGV[1],actor_kind,TG_ARGV[2],actor_ref));
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE public.threads
    ADD COLUMN moderation_actor_kind varchar(16) NOT NULL DEFAULT 'legacy_unknown',
    ADD COLUMN moderation_actor_id varchar(128) NOT NULL DEFAULT '';
UPDATE public.threads SET moderation_actor_kind = CASE WHEN COALESCE(moderation_by::text,'') ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' THEN 'user' ELSE 'legacy_unknown' END,
    moderation_actor_id = COALESCE(moderation_by::text,'');
ALTER TABLE public.threads ADD CONSTRAINT chk_threads_moderation_actor_domain CHECK (
    (moderation_actor_kind = 'legacy_unknown' AND moderation_actor_id = COALESCE(moderation_by::text,''))
    OR (moderation_actor_kind = 'user' AND moderation_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND (moderation_by IS NULL OR moderation_actor_id = COALESCE(moderation_by::text,'')))
    OR (moderation_actor_kind = 'admin' AND moderation_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND moderation_by IS NULL)
);
CREATE TRIGGER trg_01_threads_moderation_actor_actor BEFORE INSERT OR UPDATE ON public.threads
    FOR EACH ROW EXECUTE FUNCTION public.sync_business_actor_pair('moderation_by','moderation_actor_kind','moderation_actor_id','numeric');

ALTER TABLE public.content_revisions
    ADD COLUMN created_actor_kind varchar(16) NOT NULL DEFAULT 'legacy_unknown',
    ADD COLUMN created_actor_id varchar(128) NOT NULL DEFAULT '';
UPDATE public.content_revisions SET created_actor_kind = CASE WHEN COALESCE(created_by::text,'') ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' THEN 'user' ELSE 'legacy_unknown' END,
    created_actor_id = COALESCE(created_by::text,'');
ALTER TABLE public.content_revisions ADD CONSTRAINT chk_content_revisions_created_actor_domain CHECK (
    (created_actor_kind = 'legacy_unknown' AND created_actor_id = COALESCE(created_by::text,''))
    OR (created_actor_kind = 'user' AND created_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND (created_by IS NULL OR created_actor_id = COALESCE(created_by::text,'')))
    OR (created_actor_kind = 'admin' AND created_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND created_by IS NULL)
);
CREATE TRIGGER trg_01_content_revisions_created_actor_actor BEFORE INSERT OR UPDATE ON public.content_revisions
    FOR EACH ROW EXECUTE FUNCTION public.sync_business_actor_pair('created_by','created_actor_kind','created_actor_id','numeric');

ALTER TABLE public.content_moderation_cases
    ADD COLUMN opened_actor_kind varchar(16) NOT NULL DEFAULT 'legacy_unknown',
    ADD COLUMN opened_actor_id varchar(128) NOT NULL DEFAULT '';
UPDATE public.content_moderation_cases SET opened_actor_kind = CASE WHEN COALESCE(opened_by::text,'') ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' THEN 'user' ELSE 'legacy_unknown' END,
    opened_actor_id = COALESCE(opened_by::text,'');
ALTER TABLE public.content_moderation_cases ADD CONSTRAINT chk_content_moderation_cases_opened_actor_domain CHECK (
    (opened_actor_kind = 'legacy_unknown' AND opened_actor_id = COALESCE(opened_by::text,''))
    OR (opened_actor_kind = 'user' AND opened_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND (opened_by IS NULL OR opened_actor_id = COALESCE(opened_by::text,'')))
    OR (opened_actor_kind = 'admin' AND opened_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND opened_by IS NULL)
);
CREATE TRIGGER trg_01_content_moderation_cases_opened_actor_actor BEFORE INSERT OR UPDATE ON public.content_moderation_cases
    FOR EACH ROW EXECUTE FUNCTION public.sync_business_actor_pair('opened_by','opened_actor_kind','opened_actor_id','numeric');

ALTER TABLE public.content_moderation_cases
    ADD COLUMN resolved_actor_kind varchar(16) NOT NULL DEFAULT 'legacy_unknown',
    ADD COLUMN resolved_actor_id varchar(128) NOT NULL DEFAULT '';
UPDATE public.content_moderation_cases SET resolved_actor_kind = CASE WHEN COALESCE(resolved_by::text,'') ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' THEN 'user' ELSE 'legacy_unknown' END,
    resolved_actor_id = COALESCE(resolved_by::text,'');
ALTER TABLE public.content_moderation_cases ADD CONSTRAINT chk_content_moderation_cases_resolved_actor_domain CHECK (
    (resolved_actor_kind = 'legacy_unknown' AND resolved_actor_id = COALESCE(resolved_by::text,''))
    OR (resolved_actor_kind = 'user' AND resolved_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND (resolved_by IS NULL OR resolved_actor_id = COALESCE(resolved_by::text,'')))
    OR (resolved_actor_kind = 'admin' AND resolved_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND resolved_by IS NULL)
);
CREATE TRIGGER trg_01_content_moderation_cases_resolved_actor_actor BEFORE INSERT OR UPDATE ON public.content_moderation_cases
    FOR EACH ROW EXECUTE FUNCTION public.sync_business_actor_pair('resolved_by','resolved_actor_kind','resolved_actor_id','numeric');

ALTER TABLE public.content_moderation_actions
    ADD COLUMN actor_kind varchar(16) NOT NULL DEFAULT 'legacy_unknown',
    ADD COLUMN actor_ref_id varchar(128) NOT NULL DEFAULT '';
UPDATE public.content_moderation_actions SET actor_kind = CASE WHEN COALESCE(actor_id::text,'') ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' THEN 'user' ELSE 'legacy_unknown' END,
    actor_ref_id = COALESCE(actor_id::text,'');
ALTER TABLE public.content_moderation_actions ADD CONSTRAINT chk_content_moderation_actions_actor_ref_domain CHECK (
    (actor_kind = 'legacy_unknown' AND actor_ref_id = COALESCE(actor_id::text,''))
    OR (actor_kind = 'user' AND actor_ref_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND (actor_id IS NULL OR actor_ref_id = COALESCE(actor_id::text,'')))
    OR (actor_kind = 'admin' AND actor_ref_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND actor_id IS NULL)
);
CREATE TRIGGER trg_01_content_moderation_actions_actor_ref_actor BEFORE INSERT OR UPDATE ON public.content_moderation_actions
    FOR EACH ROW EXECUTE FUNCTION public.sync_business_actor_pair('actor_id','actor_kind','actor_ref_id','numeric');

ALTER TABLE public.richtext_article_contents
    ADD COLUMN updated_actor_kind varchar(16) NOT NULL DEFAULT 'legacy_unknown',
    ADD COLUMN updated_actor_id varchar(128) NOT NULL DEFAULT '';
UPDATE public.richtext_article_contents SET updated_actor_kind = CASE WHEN COALESCE(updated_by::text,'') ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' THEN 'user' ELSE 'legacy_unknown' END,
    updated_actor_id = COALESCE(updated_by::text,'');
ALTER TABLE public.richtext_article_contents ADD CONSTRAINT chk_richtext_article_contents_updated_actor_domain CHECK (
    (updated_actor_kind = 'legacy_unknown' AND updated_actor_id = COALESCE(updated_by::text,''))
    OR (updated_actor_kind = 'user' AND updated_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND (updated_by IS NULL OR updated_actor_id = COALESCE(updated_by::text,'')))
    OR (updated_actor_kind = 'admin' AND updated_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND updated_by IS NULL)
);
CREATE TRIGGER trg_01_richtext_article_contents_updated_actor_actor BEFORE INSERT OR UPDATE ON public.richtext_article_contents
    FOR EACH ROW EXECUTE FUNCTION public.sync_business_actor_pair('updated_by','updated_actor_kind','updated_actor_id','numeric');

ALTER TABLE public.asset_lifecycle_audits
    ADD COLUMN actor_kind varchar(16) NOT NULL DEFAULT 'legacy_unknown',
    ADD COLUMN actor_ref_id varchar(128) NOT NULL DEFAULT '';
UPDATE public.asset_lifecycle_audits SET actor_kind = CASE WHEN COALESCE(actor_user_id::text,'') ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' THEN 'user' ELSE 'legacy_unknown' END,
    actor_ref_id = COALESCE(actor_user_id::text,'');
ALTER TABLE public.asset_lifecycle_audits ADD CONSTRAINT chk_asset_lifecycle_audits_actor_ref_domain CHECK (
    (actor_kind = 'legacy_unknown' AND actor_ref_id = COALESCE(actor_user_id::text,''))
    OR (actor_kind = 'user' AND actor_ref_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND (actor_user_id IS NULL OR actor_ref_id = COALESCE(actor_user_id::text,'')))
    OR (actor_kind = 'admin' AND actor_ref_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND actor_user_id IS NULL)
);
CREATE TRIGGER trg_01_asset_lifecycle_audits_actor_ref_actor BEFORE INSERT OR UPDATE ON public.asset_lifecycle_audits
    FOR EACH ROW EXECUTE FUNCTION public.sync_business_actor_pair('actor_user_id','actor_kind','actor_ref_id','numeric');

ALTER TABLE public.user_spaces
    ADD COLUMN disabled_actor_kind varchar(16) NOT NULL DEFAULT 'legacy_unknown',
    ADD COLUMN disabled_actor_id varchar(128) NOT NULL DEFAULT '';
UPDATE public.user_spaces SET disabled_actor_kind = CASE WHEN FALSE THEN 'user' ELSE 'legacy_unknown' END,
    disabled_actor_id = COALESCE(disabled_by::text,'');
ALTER TABLE public.user_spaces ADD CONSTRAINT chk_user_spaces_disabled_actor_domain CHECK (
    (disabled_actor_kind = 'legacy_unknown' AND disabled_actor_id = COALESCE(disabled_by::text,''))
    OR (disabled_actor_kind = 'user' AND disabled_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND (disabled_actor_id = COALESCE(disabled_by::text,'')))
    OR (disabled_actor_kind = 'admin' AND disabled_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND disabled_by = '')
);
CREATE TRIGGER trg_01_user_spaces_disabled_actor_actor BEFORE INSERT OR UPDATE ON public.user_spaces
    FOR EACH ROW EXECUTE FUNCTION public.sync_business_actor_pair('disabled_by','disabled_actor_kind','disabled_actor_id','text');

ALTER TABLE public.user_storage_quotas
    ADD COLUMN updated_actor_kind varchar(16) NOT NULL DEFAULT 'legacy_unknown',
    ADD COLUMN updated_actor_id varchar(128) NOT NULL DEFAULT '';
UPDATE public.user_storage_quotas SET updated_actor_kind = CASE WHEN COALESCE(updated_by::text,'') ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' THEN 'user' ELSE 'legacy_unknown' END,
    updated_actor_id = COALESCE(updated_by::text,'');
ALTER TABLE public.user_storage_quotas ADD CONSTRAINT chk_user_storage_quotas_updated_actor_domain CHECK (
    (updated_actor_kind = 'legacy_unknown' AND updated_actor_id = COALESCE(updated_by::text,''))
    OR (updated_actor_kind = 'user' AND updated_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND (updated_by IS NULL OR updated_actor_id = COALESCE(updated_by::text,'')))
    OR (updated_actor_kind = 'admin' AND updated_actor_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$' AND updated_by IS NULL)
);
CREATE TRIGGER trg_01_user_storage_quotas_updated_actor_actor BEFORE INSERT OR UPDATE ON public.user_storage_quotas
    FOR EACH ROW EXECUTE FUNCTION public.sync_business_actor_pair('updated_by','updated_actor_kind','updated_actor_id','numeric');

CREATE OR REPLACE FUNCTION public.advance_thread_facts_version() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.facts_version := nextval('public.thread_facts_version_seq');
    ELSIF ROW(NEW.id, NEW.author_id, NEW.author_name, NEW.category_id,
              NEW.publication_status, NEW.moderation_status, NEW.deletion_status,
              NEW.deleted_at, NEW.title, NEW.content, NEW.content_format,
              NEW.thread_type, NEW.current_revision, NEW.tags, NEW.status,
              NEW.moderation_reason, NEW.moderation_by, NEW.moderation_at,
              NEW.moderation_actor_kind, NEW.moderation_actor_id,
              NEW.is_pinned, NEW.is_locked, NEW.is_highlighted, NEW.created_at)
          IS DISTINCT FROM
          ROW(OLD.id, OLD.author_id, OLD.author_name, OLD.category_id,
              OLD.publication_status, OLD.moderation_status, OLD.deletion_status,
              OLD.deleted_at, OLD.title, OLD.content, OLD.content_format,
              OLD.thread_type, OLD.current_revision, OLD.tags, OLD.status,
              OLD.moderation_reason, OLD.moderation_by, OLD.moderation_at,
              OLD.moderation_actor_kind, OLD.moderation_actor_id,
              OLD.is_pinned, OLD.is_locked, OLD.is_highlighted, OLD.created_at) THEN
        NEW.facts_version := nextval('public.thread_facts_version_seq');
    ELSE
        -- Ignore a caller-provided version even when no facts changed.
        NEW.facts_version := OLD.facts_version;
    END IF;
    RETURN NEW;
END;
$$;

