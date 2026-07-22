-- v13 administrator-admission operations. This is additive: credentials
-- remain in accounts, role grants remain in user_roles, and this table keeps
-- the independent management-plane admission decision plus its transition
-- evidence.

ALTER TABLE identity_admin_accounts
    ADD COLUMN IF NOT EXISTS status_reason VARCHAR(500) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS status_changed_by BIGINT NULL,
    ADD COLUMN IF NOT EXISTS status_changed_at TIMESTAMP NULL;

ALTER TABLE identity_admin_accounts
    DROP CONSTRAINT IF EXISTS fk_identity_admin_account_status_changed_by,
    ADD CONSTRAINT fk_identity_admin_account_status_changed_by
        FOREIGN KEY (status_changed_by) REFERENCES users(id) ON DELETE SET NULL,
    DROP CONSTRAINT IF EXISTS chk_identity_admin_account_status_reason,
    ADD CONSTRAINT chk_identity_admin_account_status_reason
        CHECK (char_length(status_reason) <= 500) NOT VALID;
ALTER TABLE identity_admin_accounts
    VALIDATE CONSTRAINT chk_identity_admin_account_status_reason;

CREATE INDEX IF NOT EXISTS idx_identity_admin_accounts_status_changed
    ON identity_admin_accounts (status, status_changed_at DESC, user_id ASC);

-- Keep role-triggered admission transitions auditable too. A manual
-- suspension remains suspended when a role is re-synchronized; removing the
-- only global admin role still revokes the admission record.
CREATE OR REPLACE FUNCTION sync_identity_admin_account_for_user(
    target_user_id BIGINT,
    preferred_assignment_id BIGINT
) RETURNS VOID AS $$
DECLARE
    active_assignment_id BIGINT;
    active_credential_id BIGINT;
BEGIN
    -- Serialize role-triggered changes with explicit pause/restore commands.
    -- The aggregate is the set of effective management-plane administrators,
    -- not a single admission row.
    PERFORM pg_advisory_xact_lock(130013, 39);

    SELECT ur.id, a.id
    INTO active_assignment_id, active_credential_id
    FROM user_roles ur
    INNER JOIN roles r
        ON r.id = ur.role_id
       AND r.name = 'admin'
       AND r.deleted_at IS NULL
    INNER JOIN accounts a
        ON a.user_id = ur.user_id
       AND a.type = 'email'
       AND a.deleted_at IS NULL
    WHERE ur.user_id = target_user_id
      AND ur.scope_type = 'global'
      AND ur.scope_id IS NULL
      AND ur.deleted_at IS NULL
    ORDER BY (ur.id = preferred_assignment_id) DESC, ur.created_at ASC, ur.id ASC
    LIMIT 1;

    IF FOUND THEN
        INSERT INTO identity_admin_accounts (
            id, user_id, credential_account_id, status, activation_source,
            activated_at, status_reason, status_changed_at, version, created_at, updated_at
        ) VALUES (
            active_assignment_id, target_user_id, active_credential_id, 'active',
            'role_assignment', NOW(), 'role_assignment', NOW(), 1, NOW(), NOW()
        )
        ON CONFLICT (user_id) DO UPDATE
        SET credential_account_id = EXCLUDED.credential_account_id,
            status = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN 'suspended'
                ELSE 'active'
            END,
            activation_source = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.activation_source
                ELSE EXCLUDED.activation_source
            END,
            activated_at = CASE
                WHEN identity_admin_accounts.status = 'revoked' THEN NOW()
                ELSE identity_admin_accounts.activated_at
            END,
            revoked_at = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.revoked_at
                ELSE NULL
            END,
            status_reason = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.status_reason
                WHEN identity_admin_accounts.status = 'revoked' THEN 'role_assignment'
                ELSE identity_admin_accounts.status_reason
            END,
            status_changed_by = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.status_changed_by
                WHEN identity_admin_accounts.status = 'revoked' THEN NULL
                ELSE identity_admin_accounts.status_changed_by
            END,
            status_changed_at = CASE
                WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.status_changed_at
                WHEN identity_admin_accounts.status = 'revoked' THEN NOW()
                ELSE identity_admin_accounts.status_changed_at
            END,
            updated_at = NOW(),
            version = identity_admin_accounts.version + 1;
        RETURN;
    END IF;

    UPDATE identity_admin_accounts
    SET status = 'revoked',
        revoked_at = COALESCE(revoked_at, NOW()),
        status_reason = 'admin_role_revoked',
        status_changed_by = NULL,
        status_changed_at = NOW(),
        updated_at = NOW(),
        version = version + 1
    WHERE user_id = target_user_id
      AND status <> 'revoked';
END;
$$ LANGUAGE plpgsql;

INSERT INTO permission_definitions (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, created_at, updated_at) VALUES
    (900000000000001026, 'identity.admin_account.read', 'identity', 'admin_account', 'read', '查看管理平面准入账号状态', 'medium', '["global"]'::jsonb, 'standard', NOW(), NOW()),
    (900000000000001027, 'identity.admin_account.suspend', 'identity', 'admin_account', 'suspend', '暂停管理平面准入并撤销会话', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001028, 'identity.admin_account.restore', 'identity', 'admin_account', 'restore', '恢复被暂停的管理平面准入', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001029, 'identity.admin_account.read_audit', 'identity', 'admin_account', 'read_audit', '查看管理平面准入变更审计', 'medium', '["global"]'::jsonb, 'standard', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000207000 + ROW_NUMBER() OVER (ORDER BY pd.id), r.id, pd.id, 'v13-admin-admission', NOW()
FROM roles r
INNER JOIN permission_definitions pd ON pd.code IN (
    'identity.admin_account.read',
    'identity.admin_account.suspend',
    'identity.admin_account.restore',
    'identity.admin_account.read_audit'
)
WHERE r.name = 'admin' AND r.deleted_at IS NULL
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;
