-- Keep current spaces, style snapshots, typed actor provenance and references.
CREATE SEQUENCE IF NOT EXISTS public.personal_space_facts_version_seq AS bigint NO CYCLE;
ALTER TABLE public.user_spaces ADD COLUMN facts_version bigint;
ALTER TABLE public.user_space_style_snapshots ADD COLUMN facts_version bigint;
UPDATE public.user_spaces SET facts_version=nextval('public.personal_space_facts_version_seq');
UPDATE public.user_space_style_snapshots SET facts_version=nextval('public.personal_space_facts_version_seq');
ALTER TABLE public.user_spaces ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_user_spaces_facts_version CHECK(facts_version>0);
ALTER TABLE public.user_space_style_snapshots ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_user_space_style_snapshots_facts_version CHECK(facts_version>0);
CREATE FUNCTION public.advance_personal_space_facts_version() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    ignored text[] := ARRAY['facts_version'];
BEGIN
    IF TG_TABLE_NAME='user_spaces' THEN
        ignored:=ignored||ARRAY['updated_at','last_sync_at','last_sync_error'];
    END IF;
    IF TG_OP='INSERT' THEN
        NEW.facts_version:=nextval('public.personal_space_facts_version_seq');
    ELSIF (to_jsonb(NEW)-ignored) IS DISTINCT FROM (to_jsonb(OLD)-ignored) THEN
        NEW.facts_version:=nextval('public.personal_space_facts_version_seq');
    ELSE
        NEW.facts_version:=OLD.facts_version;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_user_spaces_facts_version BEFORE INSERT OR UPDATE ON public.user_spaces
    FOR EACH ROW EXECUTE FUNCTION public.advance_personal_space_facts_version();
CREATE TRIGGER trg_space_style_snapshots_facts_version BEFORE INSERT OR UPDATE ON public.user_space_style_snapshots
    FOR EACH ROW EXECUTE FUNCTION public.advance_personal_space_facts_version();
