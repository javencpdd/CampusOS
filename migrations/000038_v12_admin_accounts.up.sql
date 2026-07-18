-- v0.12 management-plane administrator admission. Authentication credentials
-- remain owned by accounts; this table is the independent, authoritative gate
-- for access to Admin routes. Role grants describe authorization, while an
-- active row here describes whether that identity may enter the management
-- plane at all.

DO $$
DECLARE
    invalid_admins TEXT;
BEGIN
    SELECT string_agg(detail, '; ' ORDER BY detail) INTO invalid_admins
    FROM (
        SELECT format('admin user_id=%s has no active email account', ur.user_id) AS detail
        FROM user_roles ur
        INNER JOIN roles r
            ON r.id = ur.role_id
           AND r.name = 'admin'
           AND r.deleted_at IS NULL
        LEFT JOIN accounts a
            ON a.user_id = ur.user_id
           AND a.type = 'email'
           AND a.deleted_at IS NULL
        WHERE ur.scope_type = 'global'
          AND ur.scope_id IS NULL
          AND ur.deleted_at IS NULL
        GROUP BY ur.user_id
        HAVING count(a.id) <> 1
    ) invalid;

    IF invalid_admins IS NOT NULL THEN
        RAISE EXCEPTION 'v12 admin account preflight failed: %', invalid_admins
            USING HINT = 'Every active global admin role must belong to exactly one active email account before applying this migration.';
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS identity_admin_accounts (
    id                    BIGINT PRIMARY KEY,
    user_id               BIGINT NOT NULL,
    credential_account_id BIGINT NOT NULL,
    status                VARCHAR(20) NOT NULL DEFAULT 'active',
    activation_source     VARCHAR(64) NOT NULL DEFAULT 'role_assignment',
    activated_at          TIMESTAMP NOT NULL DEFAULT NOW(),
    revoked_at            TIMESTAMP NULL,
    last_authenticated_at TIMESTAMP NULL,
    version               BIGINT NOT NULL DEFAULT 1,
    created_at            TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_identity_admin_account_user
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_identity_admin_account_credential
        FOREIGN KEY (credential_account_id) REFERENCES accounts(id) ON DELETE RESTRICT,
    CONSTRAINT chk_identity_admin_account_status
        CHECK (status IN ('active', 'suspended', 'revoked')),
    CONSTRAINT chk_identity_admin_account_version
        CHECK (version >= 1),
    CONSTRAINT chk_identity_admin_account_revocation
        CHECK ((status = 'revoked' AND revoked_at IS NOT NULL) OR (status <> 'revoked' AND revoked_at IS NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_identity_admin_accounts_user
    ON identity_admin_accounts (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS uk_identity_admin_accounts_credential
    ON identity_admin_accounts (credential_account_id);
CREATE INDEX IF NOT EXISTS idx_identity_admin_accounts_status
    ON identity_admin_accounts (status, updated_at DESC);

INSERT INTO identity_admin_accounts (
    id, user_id, credential_account_id, status, activation_source,
    activated_at, version, created_at, updated_at
)
SELECT
    ur.id,
    ur.user_id,
    a.id,
    'active',
    'v12_role_backfill',
    COALESCE(ur.created_at, NOW()),
    1,
    COALESCE(ur.created_at, NOW()),
    NOW()
FROM user_roles ur
INNER JOIN roles r
    ON r.id = ur.role_id
   AND r.name = 'admin'
   AND r.deleted_at IS NULL
INNER JOIN accounts a
    ON a.user_id = ur.user_id
   AND a.type = 'email'
   AND a.deleted_at IS NULL
WHERE ur.scope_type = 'global'
  AND ur.scope_id IS NULL
  AND ur.deleted_at IS NULL
ON CONFLICT (user_id) DO UPDATE
SET credential_account_id = EXCLUDED.credential_account_id,
    status = CASE
        WHEN identity_admin_accounts.status = 'suspended' THEN 'suspended'
        ELSE 'active'
    END,
    revoked_at = CASE
        WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.revoked_at
        ELSE NULL
    END,
    activation_source = CASE
        WHEN identity_admin_accounts.status = 'suspended' THEN identity_admin_accounts.activation_source
        ELSE EXCLUDED.activation_source
    END,
    updated_at = NOW(),
    version = identity_admin_accounts.version + 1;

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

CREATE OR REPLACE FUNCTION sync_identity_admin_account_from_role()
RETURNS TRIGGER AS $$
DECLARE
    old_is_admin BOOLEAN := FALSE;
    new_is_admin BOOLEAN := FALSE;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        SELECT EXISTS (
            SELECT 1 FROM roles WHERE id=OLD.role_id AND name='admin' AND deleted_at IS NULL
        ) INTO old_is_admin;
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        SELECT EXISTS (
            SELECT 1 FROM roles WHERE id=NEW.role_id AND name='admin' AND deleted_at IS NULL
        ) INTO new_is_admin;
    END IF;

    IF old_is_admin THEN
        PERFORM sync_identity_admin_account_for_user(OLD.user_id, OLD.id);
    END IF;
    IF new_is_admin AND (NOT old_is_admin OR TG_OP <> 'UPDATE' OR NEW.user_id <> OLD.user_id) THEN
        PERFORM sync_identity_admin_account_for_user(NEW.user_id, NEW.id);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_sync_identity_admin_account_from_role ON user_roles;
CREATE TRIGGER trg_sync_identity_admin_account_from_role
AFTER INSERT OR UPDATE OF user_id, role_id, scope_type, scope_id, deleted_at OR DELETE
ON user_roles
FOR EACH ROW
EXECUTE FUNCTION sync_identity_admin_account_from_role();
