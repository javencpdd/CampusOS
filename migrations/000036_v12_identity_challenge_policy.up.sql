-- v12 configurable, always-on verification request policy. Policy values are
-- bounded and versioned; rate subjects remain keyed digests rather than raw
-- email addresses or IP addresses.

ALTER TABLE identity_challenge_rate_limits
    DROP CONSTRAINT IF EXISTS chk_identity_challenge_rate_scope;
ALTER TABLE identity_challenge_rate_limits
    ADD CONSTRAINT chk_identity_challenge_rate_scope
    CHECK (scope IN ('email_minute', 'email_day', 'ip_hour', 'email_window', 'ip_window'));

-- Conservatively carry recent legacy counters into the new sliding scopes so
-- an upgrade does not create an immediate resend bypass.
INSERT INTO identity_challenge_rate_limits (scope, subject_digest, window_started_at, request_count, updated_at)
SELECT 'email_window', subject_digest, date_trunc('second', MAX(updated_at)), LEAST(MAX(request_count), 5), MAX(updated_at)
FROM identity_challenge_rate_limits
WHERE scope = 'email_day' AND updated_at > NOW() - INTERVAL '10 minutes'
GROUP BY subject_digest
ON CONFLICT (scope, subject_digest, window_started_at)
DO UPDATE SET request_count=GREATEST(identity_challenge_rate_limits.request_count, EXCLUDED.request_count),
              updated_at=GREATEST(identity_challenge_rate_limits.updated_at, EXCLUDED.updated_at);

INSERT INTO identity_challenge_rate_limits (scope, subject_digest, window_started_at, request_count, updated_at)
SELECT 'ip_window', subject_digest, date_trunc('second', MAX(updated_at)), LEAST(MAX(request_count), 10), MAX(updated_at)
FROM identity_challenge_rate_limits
WHERE scope = 'ip_hour' AND updated_at > NOW() - INTERVAL '60 minutes'
GROUP BY subject_digest
ON CONFLICT (scope, subject_digest, window_started_at)
DO UPDATE SET request_count=GREATEST(identity_challenge_rate_limits.request_count, EXCLUDED.request_count),
              updated_at=GREATEST(identity_challenge_rate_limits.updated_at, EXCLUDED.updated_at);

CREATE TABLE IF NOT EXISTS identity_challenge_policies (
    id                      VARCHAR(64) PRIMARY KEY,
    email_window_minutes    INTEGER NOT NULL,
    email_max_requests      INTEGER NOT NULL,
    ip_window_minutes       INTEGER NOT NULL,
    ip_max_requests         INTEGER NOT NULL,
    version                 BIGINT NOT NULL DEFAULT 1,
    updated_by              BIGINT NULL,
    updated_at              TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_identity_challenge_policy_updated_by
        FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT chk_identity_challenge_policy_id
        CHECK (id = 'email_verification'),
    CONSTRAINT chk_identity_challenge_policy_email_window
        CHECK (email_window_minutes BETWEEN 1 AND 1440),
    CONSTRAINT chk_identity_challenge_policy_email_limit
        CHECK (email_max_requests BETWEEN 1 AND 100),
    CONSTRAINT chk_identity_challenge_policy_ip_window
        CHECK (ip_window_minutes BETWEEN 1 AND 1440),
    CONSTRAINT chk_identity_challenge_policy_ip_limit
        CHECK (ip_max_requests BETWEEN 1 AND 1000),
    CONSTRAINT chk_identity_challenge_policy_version
        CHECK (version >= 1)
);

INSERT INTO identity_challenge_policies (
    id, email_window_minutes, email_max_requests, ip_window_minutes, ip_max_requests, version
) VALUES ('email_verification', 10, 5, 60, 10, 1)
ON CONFLICT (id) DO NOTHING;

INSERT INTO permission_definitions (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, created_at, updated_at) VALUES
    (900000000000001024, 'identity.challenge_policy.read', 'identity', 'challenge_policy', 'read', '查看验证码请求频率策略', 'medium', '["global"]'::jsonb, 'standard', NOW(), NOW()),
    (900000000000001025, 'identity.challenge_policy.update', 'identity', 'challenge_policy', 'update', '修改验证码请求频率策略', 'high', '["global"]'::jsonb, 'required', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000206000 + ROW_NUMBER() OVER (ORDER BY pd.id), r.id, pd.id, 'v12-challenge-policy', NOW()
FROM roles r
INNER JOIN permission_definitions pd ON pd.code IN (
    'identity.challenge_policy.read',
    'identity.challenge_policy.update'
)
WHERE r.name = 'admin' AND r.deleted_at IS NULL
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;
