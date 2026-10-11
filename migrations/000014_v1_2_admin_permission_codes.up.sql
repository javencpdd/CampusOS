-- V12-02d: management-plane permission codes for the independent admin
-- identity domain routes, bound to security_admin alongside the existing
-- catalogue seed.
INSERT INTO public.permission_definitions
    (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level)
VALUES
    (900000000000002001, 'identity.admin_account.create', 'identity', 'admin_account', 'create', '创建独立管理员账号', 'high', '["global"]'::jsonb, 'required'),
    (900000000000002002, 'identity.admin_account.revoke', 'identity', 'admin_account', 'revoke', '撤销独立管理员账号', 'high', '["global"]'::jsonb, 'required'),
    (900000000000002003, 'identity.admin_session.revoke', 'identity', 'admin_session', 'revoke', '撤销管理员会话', 'medium', '["global"]'::jsonb, 'required'),
    (900000000000002004, 'identity.admin_mfa.manage', 'identity', 'admin_mfa', 'manage', '管理管理员 TOTP 因素', 'high', '["global"]'::jsonb, 'required')
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.admin_role_permissions (role_id, permission_code)
SELECT 'security_admin', code FROM (VALUES
    ('identity.admin_account.create'),
    ('identity.admin_account.revoke'),
    ('identity.admin_session.revoke'),
    ('identity.admin_mfa.manage')
) AS additions(code)
ON CONFLICT DO NOTHING;
