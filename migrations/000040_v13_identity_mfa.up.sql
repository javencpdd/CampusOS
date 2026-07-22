-- v13 TOTP MFA. Identity owns encrypted factor envelopes, one-time ticket
-- digests and recovery-code digests. No plaintext factor, ticket or recovery
-- code is stored in PostgreSQL.

ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS authentication_strength VARCHAR(16) NOT NULL DEFAULT 'password',
    ADD COLUMN IF NOT EXISTS mfa_authenticated_at TIMESTAMP NULL;

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS chk_sessions_authentication_strength,
    ADD CONSTRAINT chk_sessions_authentication_strength
        CHECK (authentication_strength IN ('password', 'mfa'));

CREATE TABLE IF NOT EXISTS identity_mfa_totp_methods (
    id                      BIGINT PRIMARY KEY,
    user_id                 BIGINT NOT NULL,
    status                  VARCHAR(16) NOT NULL,
    key_id                  VARCHAR(96) NOT NULL,
    nonce                   TEXT NOT NULL,
    ciphertext              TEXT NOT NULL,
    last_accepted_step      BIGINT NOT NULL DEFAULT 0,
    enrollment_expires_at   TIMESTAMP NOT NULL,
    confirmed_at            TIMESTAMP NULL,
    disabled_at             TIMESTAMP NULL,
    created_at              TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_identity_mfa_totp_user
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT chk_identity_mfa_totp_status
        CHECK (status IN ('pending', 'active', 'disabled')),
    CONSTRAINT chk_identity_mfa_totp_step
        CHECK (last_accepted_step >= 0),
    CONSTRAINT chk_identity_mfa_totp_envelope
        CHECK (length(key_id) > 0 AND length(nonce) >= 8 AND length(ciphertext) >= 16),
    CONSTRAINT chk_identity_mfa_totp_confirmation
        CHECK ((status <> 'active') OR confirmed_at IS NOT NULL)
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_identity_mfa_totp_active_user
    ON identity_mfa_totp_methods (user_id)
    WHERE status = 'active';
CREATE UNIQUE INDEX IF NOT EXISTS uk_identity_mfa_totp_pending_user
    ON identity_mfa_totp_methods (user_id)
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_identity_mfa_totp_user_status
    ON identity_mfa_totp_methods (user_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS identity_mfa_tickets (
    id              BIGINT PRIMARY KEY,
    user_id         BIGINT NOT NULL,
    audience        VARCHAR(16) NOT NULL,
    purpose         VARCHAR(16) NOT NULL,
    ticket_digest   VARCHAR(64) NOT NULL,
    expires_at      TIMESTAMP NOT NULL,
    consumed_at     TIMESTAMP NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 5,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_identity_mfa_ticket_user
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT uk_identity_mfa_ticket_digest UNIQUE (ticket_digest),
    CONSTRAINT chk_identity_mfa_ticket_audience
        CHECK (audience IN ('web', 'admin')),
    CONSTRAINT chk_identity_mfa_ticket_purpose
        CHECK (purpose IN ('login', 'step_up')),
    CONSTRAINT chk_identity_mfa_ticket_digest
        CHECK (ticket_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_identity_mfa_ticket_attempts
        CHECK (attempts >= 0 AND max_attempts BETWEEN 1 AND 10)
);

CREATE INDEX IF NOT EXISTS idx_identity_mfa_tickets_expiry
    ON identity_mfa_tickets (expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_mfa_recovery_codes (
    id              BIGINT PRIMARY KEY,
    user_id         BIGINT NOT NULL,
    method_id       BIGINT NOT NULL,
    code_digest     VARCHAR(64) NOT NULL,
    used_at         TIMESTAMP NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_identity_mfa_recovery_user
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_identity_mfa_recovery_method
        FOREIGN KEY (method_id) REFERENCES identity_mfa_totp_methods(id) ON DELETE RESTRICT,
    CONSTRAINT uk_identity_mfa_recovery_digest UNIQUE (code_digest),
    CONSTRAINT chk_identity_mfa_recovery_digest
        CHECK (code_digest ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_identity_mfa_recovery_user_unused
    ON identity_mfa_recovery_codes (user_id, created_at DESC)
    WHERE used_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_mfa_policies (
    id              VARCHAR(32) PRIMARY KEY,
    mode            VARCHAR(32) NOT NULL DEFAULT 'off',
    grace_ends_at   TIMESTAMP NULL,
    version         BIGINT NOT NULL DEFAULT 1,
    updated_by      BIGINT NULL,
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_identity_mfa_policy_updated_by
        FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT chk_identity_mfa_policy_id
        CHECK (id = 'admin'),
    CONSTRAINT chk_identity_mfa_policy_mode
        CHECK (mode IN ('off', 'enrollment_grace', 'required')),
    CONSTRAINT chk_identity_mfa_policy_grace
        CHECK ((mode = 'enrollment_grace' AND grace_ends_at IS NOT NULL) OR (mode <> 'enrollment_grace' AND grace_ends_at IS NULL)),
    CONSTRAINT chk_identity_mfa_policy_version
        CHECK (version >= 1)
);

INSERT INTO identity_mfa_policies (id, mode, version, updated_at)
VALUES ('admin', 'off', 1, NOW())
ON CONFLICT (id) DO NOTHING;

INSERT INTO permission_definitions (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, created_at, updated_at) VALUES
    (900000000000001040, 'identity.mfa_policy.read', 'identity', 'mfa_policy', 'read', '查看管理员 MFA 强制策略与聚合覆盖率', 'medium', '["global"]'::jsonb, 'standard', NOW(), NOW()),
    (900000000000001041, 'identity.mfa_policy.update', 'identity', 'mfa_policy', 'update', '修改管理员 MFA 强制策略', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001042, 'identity.mfa.local_recovery', 'identity', 'mfa', 'local_recovery', '本机受控 MFA 恢复审计代码，不授予 Web 角色', 'high', '["global"]'::jsonb, 'required', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000208000 + ROW_NUMBER() OVER (ORDER BY pd.id), r.id, pd.id, 'v13-identity-mfa', NOW()
FROM roles r
INNER JOIN permission_definitions pd ON pd.code IN (
    'identity.mfa_policy.read',
    'identity.mfa_policy.update'
)
WHERE r.name = 'admin' AND r.deleted_at IS NULL
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;
