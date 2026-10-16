-- Identity owns an immutable observation version for each Admin execution
-- permission row. Delete/reinsert, metadata ABA and rolled-back allocations
-- cannot reuse an observed value. Down/up preserves the sequence high water.
CREATE SEQUENCE IF NOT EXISTS public.admin_permission_facts_version_seq AS bigint NO CYCLE;

ALTER TABLE public.admin_role_permissions ADD COLUMN facts_version bigint;
UPDATE public.admin_role_permissions
    SET facts_version = nextval('public.admin_permission_facts_version_seq');
ALTER TABLE public.admin_role_permissions
    ALTER COLUMN facts_version SET NOT NULL,
    ADD CONSTRAINT chk_admin_permission_facts_version CHECK (facts_version > 0);
ALTER SEQUENCE public.admin_permission_facts_version_seq OWNED BY public.admin_role_permissions.facts_version;

CREATE FUNCTION public.advance_admin_permission_facts_version() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.facts_version := nextval('public.admin_permission_facts_version_seq');
    ELSIF ROW(NEW.role_id, NEW.permission_code, NEW.created_by, NEW.created_at)
        IS DISTINCT FROM ROW(OLD.role_id, OLD.permission_code, OLD.created_by, OLD.created_at) THEN
        NEW.facts_version := nextval('public.admin_permission_facts_version_seq');
    ELSE
        NEW.facts_version := OLD.facts_version;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_admin_permission_facts_version
    BEFORE INSERT OR UPDATE ON public.admin_role_permissions
    FOR EACH ROW EXECUTE FUNCTION public.advance_admin_permission_facts_version();
