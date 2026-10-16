-- Stop/revert checked governance readers before removing their required
-- version column. Every permission/binding/role row and the sequence high
-- water are retained; re-up allocates fresh versions for existing rows.
DROP TRIGGER IF EXISTS trg_admin_permission_facts_version ON public.admin_role_permissions;
DROP FUNCTION IF EXISTS public.advance_admin_permission_facts_version();
ALTER TABLE public.admin_role_permissions DROP CONSTRAINT IF EXISTS chk_admin_permission_facts_version;
ALTER SEQUENCE IF EXISTS public.admin_permission_facts_version_seq OWNED BY NONE;
ALTER TABLE public.admin_role_permissions DROP COLUMN IF EXISTS facts_version;
