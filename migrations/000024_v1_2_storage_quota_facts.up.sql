-- Preserve every quota, legacy User FK, and typed actor18 provenance.
-- Retain the allocator across down/up so an abandoned proof cannot recur.
CREATE SEQUENCE IF NOT EXISTS public.storage_quota_facts_version_seq AS bigint NO CYCLE;
ALTER TABLE public.user_storage_quotas ADD COLUMN facts_version bigint;
UPDATE public.user_storage_quotas SET facts_version=nextval('public.storage_quota_facts_version_seq');
ALTER TABLE public.user_storage_quotas ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_user_storage_quotas_facts_version CHECK(facts_version>0);
CREATE FUNCTION public.advance_storage_quota_facts_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        NEW.facts_version:=nextval('public.storage_quota_facts_version_seq');
    ELSIF (to_jsonb(NEW)-'facts_version') IS DISTINCT FROM (to_jsonb(OLD)-'facts_version') THEN
        NEW.facts_version:=nextval('public.storage_quota_facts_version_seq');
    ELSE
        NEW.facts_version:=OLD.facts_version;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_user_storage_quotas_facts_version BEFORE INSERT OR UPDATE ON public.user_storage_quotas
    FOR EACH ROW EXECUTE FUNCTION public.advance_storage_quota_facts_version();
