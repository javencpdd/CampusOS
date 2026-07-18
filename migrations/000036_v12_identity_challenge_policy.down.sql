-- Down is limited to isolated migration drills. Production retains the policy
-- audit trail and applies a forward fix.

DELETE FROM role_permissions
WHERE created_by = 'v12-challenge-policy';

DELETE FROM permission_definitions
WHERE code IN ('identity.challenge_policy.read', 'identity.challenge_policy.update');

DROP TABLE IF EXISTS identity_challenge_policies;

DELETE FROM identity_challenge_rate_limits
WHERE scope IN ('email_window', 'ip_window');

ALTER TABLE identity_challenge_rate_limits
    DROP CONSTRAINT IF EXISTS chk_identity_challenge_rate_scope;
ALTER TABLE identity_challenge_rate_limits
    ADD CONSTRAINT chk_identity_challenge_rate_scope
    CHECK (scope IN ('email_minute', 'email_day', 'ip_hour'));
