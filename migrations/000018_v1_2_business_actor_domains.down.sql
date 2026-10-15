-- Fail rather than discard any domain identity absent from legacy storage.
DO $$
BEGIN

    IF EXISTS (SELECT 1 FROM public.threads WHERE moderation_actor_kind NOT IN ('user','legacy_unknown')
        OR moderation_actor_id IS DISTINCT FROM COALESCE(moderation_by::text,'')) THEN
        RAISE EXCEPTION 'business actor rollback would lose domain identity: threads.moderation_actor';
    END IF;

    IF EXISTS (SELECT 1 FROM public.content_revisions WHERE created_actor_kind NOT IN ('user','legacy_unknown')
        OR created_actor_id IS DISTINCT FROM COALESCE(created_by::text,'')) THEN
        RAISE EXCEPTION 'business actor rollback would lose domain identity: content_revisions.created_actor';
    END IF;

    IF EXISTS (SELECT 1 FROM public.content_moderation_cases WHERE opened_actor_kind NOT IN ('user','legacy_unknown')
        OR opened_actor_id IS DISTINCT FROM COALESCE(opened_by::text,'')) THEN
        RAISE EXCEPTION 'business actor rollback would lose domain identity: content_moderation_cases.opened_actor';
    END IF;

    IF EXISTS (SELECT 1 FROM public.content_moderation_cases WHERE resolved_actor_kind NOT IN ('user','legacy_unknown')
        OR resolved_actor_id IS DISTINCT FROM COALESCE(resolved_by::text,'')) THEN
        RAISE EXCEPTION 'business actor rollback would lose domain identity: content_moderation_cases.resolved_actor';
    END IF;

    IF EXISTS (SELECT 1 FROM public.content_moderation_actions WHERE actor_kind NOT IN ('user','legacy_unknown')
        OR actor_ref_id IS DISTINCT FROM COALESCE(actor_id::text,'')) THEN
        RAISE EXCEPTION 'business actor rollback would lose domain identity: content_moderation_actions.actor_ref';
    END IF;

    IF EXISTS (SELECT 1 FROM public.richtext_article_contents WHERE updated_actor_kind NOT IN ('user','legacy_unknown')
        OR updated_actor_id IS DISTINCT FROM COALESCE(updated_by::text,'')) THEN
        RAISE EXCEPTION 'business actor rollback would lose domain identity: richtext_article_contents.updated_actor';
    END IF;

    IF EXISTS (SELECT 1 FROM public.asset_lifecycle_audits WHERE actor_kind NOT IN ('user','legacy_unknown')
        OR actor_ref_id IS DISTINCT FROM COALESCE(actor_user_id::text,'')) THEN
        RAISE EXCEPTION 'business actor rollback would lose domain identity: asset_lifecycle_audits.actor_ref';
    END IF;

    IF EXISTS (SELECT 1 FROM public.user_spaces WHERE disabled_actor_kind NOT IN ('user','legacy_unknown')
        OR disabled_actor_id IS DISTINCT FROM COALESCE(disabled_by::text,'')) THEN
        RAISE EXCEPTION 'business actor rollback would lose domain identity: user_spaces.disabled_actor';
    END IF;

    IF EXISTS (SELECT 1 FROM public.user_storage_quotas WHERE updated_actor_kind NOT IN ('user','legacy_unknown')
        OR updated_actor_id IS DISTINCT FROM COALESCE(updated_by::text,'')) THEN
        RAISE EXCEPTION 'business actor rollback would lose domain identity: user_storage_quotas.updated_actor';
    END IF;

END;
$$;
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


DROP TRIGGER trg_01_user_storage_quotas_updated_actor_actor ON public.user_storage_quotas;
ALTER TABLE public.user_storage_quotas DROP CONSTRAINT chk_user_storage_quotas_updated_actor_domain,
    DROP COLUMN updated_actor_kind, DROP COLUMN updated_actor_id;

DROP TRIGGER trg_01_user_spaces_disabled_actor_actor ON public.user_spaces;
ALTER TABLE public.user_spaces DROP CONSTRAINT chk_user_spaces_disabled_actor_domain,
    DROP COLUMN disabled_actor_kind, DROP COLUMN disabled_actor_id;

DROP TRIGGER trg_01_asset_lifecycle_audits_actor_ref_actor ON public.asset_lifecycle_audits;
ALTER TABLE public.asset_lifecycle_audits DROP CONSTRAINT chk_asset_lifecycle_audits_actor_ref_domain,
    DROP COLUMN actor_kind, DROP COLUMN actor_ref_id;

DROP TRIGGER trg_01_richtext_article_contents_updated_actor_actor ON public.richtext_article_contents;
ALTER TABLE public.richtext_article_contents DROP CONSTRAINT chk_richtext_article_contents_updated_actor_domain,
    DROP COLUMN updated_actor_kind, DROP COLUMN updated_actor_id;

DROP TRIGGER trg_01_content_moderation_actions_actor_ref_actor ON public.content_moderation_actions;
ALTER TABLE public.content_moderation_actions DROP CONSTRAINT chk_content_moderation_actions_actor_ref_domain,
    DROP COLUMN actor_kind, DROP COLUMN actor_ref_id;

DROP TRIGGER trg_01_content_moderation_cases_resolved_actor_actor ON public.content_moderation_cases;
ALTER TABLE public.content_moderation_cases DROP CONSTRAINT chk_content_moderation_cases_resolved_actor_domain,
    DROP COLUMN resolved_actor_kind, DROP COLUMN resolved_actor_id;

DROP TRIGGER trg_01_content_moderation_cases_opened_actor_actor ON public.content_moderation_cases;
ALTER TABLE public.content_moderation_cases DROP CONSTRAINT chk_content_moderation_cases_opened_actor_domain,
    DROP COLUMN opened_actor_kind, DROP COLUMN opened_actor_id;

DROP TRIGGER trg_01_content_revisions_created_actor_actor ON public.content_revisions;
ALTER TABLE public.content_revisions DROP CONSTRAINT chk_content_revisions_created_actor_domain,
    DROP COLUMN created_actor_kind, DROP COLUMN created_actor_id;

DROP TRIGGER trg_01_threads_moderation_actor_actor ON public.threads;
ALTER TABLE public.threads DROP CONSTRAINT chk_threads_moderation_actor_domain,
    DROP COLUMN moderation_actor_kind, DROP COLUMN moderation_actor_id;

DROP FUNCTION public.sync_business_actor_pair();
