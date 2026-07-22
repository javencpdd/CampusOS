DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permission_definitions
    WHERE code IN ('identity.mfa_policy.read', 'identity.mfa_policy.update', 'identity.mfa.local_recovery')
);

DELETE FROM permission_definitions
WHERE code IN ('identity.mfa_policy.read', 'identity.mfa_policy.update', 'identity.mfa.local_recovery');

DROP TABLE IF EXISTS identity_mfa_recovery_codes;
DROP TABLE IF EXISTS identity_mfa_tickets;
DROP TABLE IF EXISTS identity_mfa_totp_methods;
DROP TABLE IF EXISTS identity_mfa_policies;

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS chk_sessions_authentication_strength,
    DROP COLUMN IF EXISTS mfa_authenticated_at,
    DROP COLUMN IF EXISTS authentication_strength;
