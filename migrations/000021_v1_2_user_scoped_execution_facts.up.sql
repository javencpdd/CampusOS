-- Current User catalog execution facts remain separate from Admin and frozen
-- board delegation grants. Shared allocation is never reused across four tables.
CREATE SEQUENCE IF NOT EXISTS public.user_scope_facts_version_seq AS bigint NO CYCLE;
ALTER TABLE public.roles ADD COLUMN facts_version bigint;
ALTER TABLE public.user_roles ADD COLUMN facts_version bigint;
ALTER TABLE public.role_permissions ADD COLUMN facts_version bigint;
ALTER TABLE public.permission_definitions ADD COLUMN facts_version bigint;
UPDATE public.roles SET facts_version=nextval('public.user_scope_facts_version_seq');
UPDATE public.user_roles SET facts_version=nextval('public.user_scope_facts_version_seq');
UPDATE public.role_permissions SET facts_version=nextval('public.user_scope_facts_version_seq');
UPDATE public.permission_definitions SET facts_version=nextval('public.user_scope_facts_version_seq');
ALTER TABLE public.roles ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_roles_facts_version CHECK (facts_version>0);
ALTER TABLE public.user_roles ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_user_roles_facts_version CHECK (facts_version>0);
ALTER TABLE public.role_permissions ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_role_permissions_facts_version CHECK (facts_version>0);
ALTER TABLE public.permission_definitions ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_permission_definitions_facts_version CHECK (facts_version>0);
CREATE FUNCTION public.advance_user_scope_facts_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        NEW.facts_version:=nextval('public.user_scope_facts_version_seq');
    ELSIF (to_jsonb(NEW)-'facts_version'-'updated_at')
        IS DISTINCT FROM (to_jsonb(OLD)-'facts_version'-'updated_at') THEN
        NEW.facts_version:=nextval('public.user_scope_facts_version_seq');
    ELSE
        NEW.facts_version:=OLD.facts_version;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_roles_user_scope_facts_version BEFORE INSERT OR UPDATE ON public.roles
    FOR EACH ROW EXECUTE FUNCTION public.advance_user_scope_facts_version();
CREATE TRIGGER trg_user_roles_user_scope_facts_version BEFORE INSERT OR UPDATE ON public.user_roles
    FOR EACH ROW EXECUTE FUNCTION public.advance_user_scope_facts_version();
CREATE TRIGGER trg_role_permissions_user_scope_facts_version BEFORE INSERT OR UPDATE ON public.role_permissions
    FOR EACH ROW EXECUTE FUNCTION public.advance_user_scope_facts_version();
CREATE TRIGGER trg_permission_definitions_user_scope_facts_version BEFORE INSERT OR UPDATE ON public.permission_definitions
    FOR EACH ROW EXECUTE FUNCTION public.advance_user_scope_facts_version();
