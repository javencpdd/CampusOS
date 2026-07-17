-- v12 A7 administrator-assisted account recovery. A case records only the
-- minimum workflow state and a non-sensitive offline-proof reference. It
-- never stores a password, verification code, ticket, session credential or
-- plaintext proof material.

CREATE TABLE IF NOT EXISTS identity_account_recovery_cases (
    id                      BIGINT PRIMARY KEY,
    public_id               VARCHAR(96) NOT NULL,
    user_id                 BIGINT NOT NULL,
    account_id              BIGINT NOT NULL,
    target_email_normalized VARCHAR(320) NOT NULL,
    challenge_id            BIGINT NOT NULL,
    created_by              BIGINT NULL,
    proof_reference         VARCHAR(160) NOT NULL DEFAULT '',
    status                  VARCHAR(24) NOT NULL DEFAULT 'pending',
    expires_at              TIMESTAMP NOT NULL,
    completed_at            TIMESTAMP NULL,
    cancelled_at            TIMESTAMP NULL,
    created_at              TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_identity_recovery_case_user
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_identity_recovery_case_account
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT,
    CONSTRAINT fk_identity_recovery_case_challenge
        FOREIGN KEY (challenge_id) REFERENCES identity_email_challenges(id) ON DELETE RESTRICT,
    CONSTRAINT fk_identity_recovery_case_created_by
        FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT chk_identity_recovery_case_status
        CHECK (status IN ('pending', 'completed', 'cancelled', 'expired')),
    CONSTRAINT chk_identity_recovery_case_target_email
        CHECK (target_email_normalized = lower(btrim(target_email_normalized))),
    CONSTRAINT chk_identity_recovery_case_completion
        CHECK ((status = 'completed' AND completed_at IS NOT NULL) OR status <> 'completed')
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_identity_recovery_case_public_id
    ON identity_account_recovery_cases(public_id);
CREATE UNIQUE INDEX IF NOT EXISTS uk_identity_recovery_case_challenge
    ON identity_account_recovery_cases(challenge_id);
CREATE INDEX IF NOT EXISTS idx_identity_recovery_case_user_status
    ON identity_account_recovery_cases(user_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_identity_recovery_case_expiry
    ON identity_account_recovery_cases(status, expires_at)
    WHERE status = 'pending';

-- These codes deliberately remain separate from broad role/user management.
-- They are assigned only to the built-in admin role; operators can delegate
-- them explicitly with a custom role after understanding their required audit
-- and offline verification responsibilities.
INSERT INTO permission_definitions (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, created_at, updated_at) VALUES
    (900000000000001020, 'identity.account.recovery.override', 'identity', 'account', 'recovery_override', '创建或取消管理员辅助账号恢复', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001021, 'identity.session.read', 'identity', 'session', 'read', '查看指定用户的安全会话投影', 'medium', '["global"]'::jsonb, 'standard', NOW(), NOW()),
    (900000000000001022, 'identity.session.revoke', 'identity', 'session', 'revoke', '撤销指定用户的全部会话', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001023, 'platform.email_delivery.read', 'platform', 'email_delivery', 'read', '查看邮件投递的脱敏运行状态', 'low', '["global"]'::jsonb, 'standard', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000202000 + ROW_NUMBER() OVER (ORDER BY pd.id), r.id, pd.id, 'v12-identity-recovery', NOW()
FROM roles r
INNER JOIN permission_definitions pd ON pd.code IN (
    'identity.account.recovery.override',
    'identity.session.read',
    'identity.session.revoke',
    'platform.email_delivery.read'
)
WHERE r.name = 'admin' AND r.deleted_at IS NULL
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;
