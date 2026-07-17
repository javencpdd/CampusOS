-- Down is only for isolated migration drills. Production keeps recovery case
-- audit evidence and uses forward-fix rather than dropping workflow history.

DELETE FROM role_permissions
WHERE created_by = 'v12-identity-recovery';

DELETE FROM permission_definitions
WHERE code IN (
    'identity.account.recovery.override',
    'identity.session.read',
    'identity.session.revoke',
    'platform.email_delivery.read'
);

DROP INDEX IF EXISTS idx_identity_recovery_case_expiry;
DROP INDEX IF EXISTS idx_identity_recovery_case_user_status;
DROP INDEX IF EXISTS uk_identity_recovery_case_challenge;
DROP INDEX IF EXISTS uk_identity_recovery_case_public_id;
DROP TABLE IF EXISTS identity_account_recovery_cases;
