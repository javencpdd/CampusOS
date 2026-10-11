-- V12-02d rollback: remove the management-plane permission codes and their
-- admin-domain bindings.
DELETE FROM public.admin_role_permissions WHERE permission_code IN (
    'identity.admin_account.create', 'identity.admin_account.revoke',
    'identity.admin_session.revoke', 'identity.admin_mfa.manage');
DELETE FROM public.permission_definitions WHERE code IN (
    'identity.admin_account.create', 'identity.admin_account.revoke',
    'identity.admin_session.revoke', 'identity.admin_mfa.manage');
