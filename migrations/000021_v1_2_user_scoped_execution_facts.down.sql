-- Drop only the version mechanism; preserve all catalog, scope and deprecation
-- records, and keep the shared sequence allocation high water for a later up.
DROP TRIGGER trg_roles_user_scope_facts_version ON public.roles;
DROP TRIGGER trg_user_roles_user_scope_facts_version ON public.user_roles;
DROP TRIGGER trg_role_permissions_user_scope_facts_version ON public.role_permissions;
DROP TRIGGER trg_permission_definitions_user_scope_facts_version ON public.permission_definitions;
DROP FUNCTION public.advance_user_scope_facts_version();
ALTER TABLE public.roles DROP CONSTRAINT chk_roles_facts_version, DROP COLUMN facts_version;
ALTER TABLE public.user_roles DROP CONSTRAINT chk_user_roles_facts_version, DROP COLUMN facts_version;
ALTER TABLE public.role_permissions DROP CONSTRAINT chk_role_permissions_facts_version, DROP COLUMN facts_version;
ALTER TABLE public.permission_definitions DROP CONSTRAINT chk_permission_definitions_facts_version, DROP COLUMN facts_version;
