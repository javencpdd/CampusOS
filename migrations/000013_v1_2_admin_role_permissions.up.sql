-- V12-02d: admin-domain role permission bindings. The management-plane roles
-- bind to the shared permission catalogue with an admin-only audience; the
-- seed grants security_admin the full non-deprecated catalogue, mirroring the
-- v1 admin role semantics until the Admin UI manages finer admin roles.
CREATE TABLE public.admin_role_permissions (
    role_id VARCHAR(64) NOT NULL REFERENCES public.admin_roles(id) ON DELETE CASCADE,
    permission_code VARCHAR(160) NOT NULL,
    created_by VARCHAR(128) NOT NULL DEFAULT 'migration-000013',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (role_id, permission_code)
);

INSERT INTO public.admin_role_permissions (role_id, permission_code)
SELECT 'security_admin', pd.code
FROM public.permission_definitions pd
WHERE pd.deprecated_at IS NULL;
