DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permission_definitions
    WHERE code IN (
        'identity.admin_account.read',
        'identity.admin_account.suspend',
        'identity.admin_account.restore',
        'identity.admin_account.read_audit'
    )
);

DELETE FROM permission_definitions
WHERE code IN (
    'identity.admin_account.read',
    'identity.admin_account.suspend',
    'identity.admin_account.restore',
    'identity.admin_account.read_audit'
);

CREATE OR REPLACE FUNCTION sync_identity_admin_account_for_user(
    target_user_id BIGINT,
    preferred_assignment_id BIGINT
) RETURNS VOID AS $$
DECLARE
    active_assignment_id BIGINT;
    active_credential_id BIGINT;
BEGIN
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
            activated_at, version, created_at, updated_at
        ) VALUES (
            active_assignment_id, target_user_id, active_credential_id, 'active',
            'role_assignment', NOW(), 1, NOW(), NOW()
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
            updated_at = NOW(),
            version = identity_admin_accounts.version + 1;
        RETURN;
    END IF;

    UPDATE identity_admin_accounts
    SET status = 'revoked',
        revoked_at = COALESCE(revoked_at, NOW()),
        updated_at = NOW(),
        version = version + 1
    WHERE user_id = target_user_id
      AND status <> 'revoked';
END;
$$ LANGUAGE plpgsql;

DROP INDEX IF EXISTS idx_identity_admin_accounts_status_changed;
ALTER TABLE identity_admin_accounts
    DROP CONSTRAINT IF EXISTS chk_identity_admin_account_status_reason,
    DROP CONSTRAINT IF EXISTS fk_identity_admin_account_status_changed_by,
    DROP COLUMN IF EXISTS status_changed_at,
    DROP COLUMN IF EXISTS status_changed_by,
    DROP COLUMN IF EXISTS status_reason;
