-- v0.12 identity account facts. accounts(type=email) becomes the authoritative
-- login-email record; users.email remains a compatibility projection. This
-- migration stops before any DDL if historical rows cannot be normalized
-- without making an unsafe choice.

DO $$
DECLARE
    conflict_details TEXT;
BEGIN
    SELECT string_agg(detail, '; ' ORDER BY detail) INTO conflict_details
    FROM (
        SELECT format(
            'casefold email collision normalized=%s account_ids=%s',
            normalized,
            string_agg(id::text, ',' ORDER BY id)
        ) AS detail
        FROM (
            SELECT id, lower(btrim(identifier)) AS normalized
            FROM accounts
            WHERE type = 'email' AND deleted_at IS NULL
        ) email_accounts
        GROUP BY normalized
        HAVING count(*) > 1

        UNION ALL

        SELECT format(
            'multiple email accounts user_id=%s account_ids=%s',
            user_id,
            string_agg(id::text, ',' ORDER BY id)
        ) AS detail
        FROM accounts
        WHERE type = 'email' AND deleted_at IS NULL
        GROUP BY user_id
        HAVING count(*) > 1

        UNION ALL

        SELECT format(
            'user/account email mismatch user_id=%s account_id=%s user_email=%s account_email=%s',
            u.id,
            a.id,
            lower(btrim(u.email)),
            lower(btrim(a.identifier))
        ) AS detail
        FROM users u
        INNER JOIN accounts a
            ON a.user_id = u.id
           AND a.type = 'email'
           AND a.deleted_at IS NULL
        WHERE u.deleted_at IS NULL
          AND lower(btrim(u.email)) <> lower(btrim(a.identifier))

        UNION ALL

        SELECT format('active user without email account user_id=%s email=%s', u.id, lower(btrim(u.email))) AS detail
        FROM users u
        LEFT JOIN accounts a
            ON a.user_id = u.id
           AND a.type = 'email'
           AND a.deleted_at IS NULL
        WHERE u.deleted_at IS NULL
          AND btrim(coalesce(u.email, '')) <> ''
          AND a.id IS NULL

        UNION ALL

        SELECT format('orphan email account account_id=%s user_id=%s', a.id, a.user_id) AS detail
        FROM accounts a
        LEFT JOIN users u ON u.id = a.user_id
        WHERE a.type = 'email'
          AND a.deleted_at IS NULL
          AND u.id IS NULL

        UNION ALL

        SELECT format('empty email account account_id=%s user_id=%s', a.id, a.user_id) AS detail
        FROM accounts a
        WHERE a.type = 'email'
          AND a.deleted_at IS NULL
          AND btrim(coalesce(a.identifier, '')) = ''
    ) conflicts;

    IF conflict_details IS NOT NULL THEN
        RAISE EXCEPTION 'v12 identity account preflight failed: %', conflict_details
            USING HINT = 'Resolve the listed account data manually; this migration never chooses or merges an email owner.';
    END IF;
END $$;

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS auth_version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE users
    ADD CONSTRAINT chk_users_auth_version CHECK (auth_version >= 1) NOT VALID;
ALTER TABLE users VALIDATE CONSTRAINT chk_users_auth_version;

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS identifier_normalized VARCHAR(320),
    ADD COLUMN IF NOT EXISTS verification_state VARCHAR(32),
    ADD COLUMN IF NOT EXISTS verified_at TIMESTAMP NULL,
    ADD COLUMN IF NOT EXISTS verification_source VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMP NULL,
    ADD COLUMN IF NOT EXISTS credential_version BIGINT NOT NULL DEFAULT 1;

UPDATE accounts
SET identifier_normalized = lower(btrim(identifier)),
    identifier = lower(btrim(identifier))
WHERE type = 'email';

UPDATE users u
SET email = a.identifier_normalized,
    updated_at = NOW()
FROM accounts a
WHERE a.user_id = u.id
  AND a.type = 'email'
  AND a.deleted_at IS NULL
  AND u.deleted_at IS NULL;

UPDATE accounts
SET verification_state = CASE
        WHEN type = 'email' AND identifier_normalized = 'admin@campusos.local' THEN 'system_managed'
        WHEN type = 'email' THEN 'legacy_accepted'
        ELSE 'legacy_accepted'
    END,
    verified = CASE
        WHEN type = 'email' AND identifier_normalized = 'admin@campusos.local' THEN TRUE
        ELSE FALSE
    END,
    verified_at = CASE
        WHEN type = 'email' AND identifier_normalized = 'admin@campusos.local' THEN COALESCE(verified_at, updated_at, created_at, NOW())
        ELSE NULL
    END,
    verification_source = CASE
        WHEN type = 'email' AND identifier_normalized = 'admin@campusos.local' THEN 'v12.system_seed'
        ELSE 'v12.legacy_migration'
    END,
    password_changed_at = COALESCE(password_changed_at, updated_at, created_at, NOW()),
    credential_version = GREATEST(COALESCE(credential_version, 1), 1);

UPDATE accounts
SET identifier_normalized = lower(btrim(identifier)),
    verification_state = COALESCE(NULLIF(verification_state, ''), 'legacy_accepted')
WHERE identifier_normalized IS NULL OR verification_state IS NULL OR verification_state = '';

ALTER TABLE accounts
    ALTER COLUMN identifier_normalized SET NOT NULL,
    ALTER COLUMN verification_state SET NOT NULL;

ALTER TABLE accounts
    ADD CONSTRAINT chk_accounts_identifier_normalized
        CHECK (type <> 'email' OR identifier = identifier_normalized) NOT VALID,
    ADD CONSTRAINT chk_accounts_verification_state
        CHECK (verification_state IN ('unverified', 'legacy_accepted', 'verified', 'system_managed')) NOT VALID,
    ADD CONSTRAINT chk_accounts_credential_version
        CHECK (credential_version >= 1) NOT VALID;
ALTER TABLE accounts VALIDATE CONSTRAINT chk_accounts_identifier_normalized;
ALTER TABLE accounts VALIDATE CONSTRAINT chk_accounts_verification_state;
ALTER TABLE accounts VALIDATE CONSTRAINT chk_accounts_credential_version;

CREATE UNIQUE INDEX IF NOT EXISTS uk_accounts_email_normalized
    ON accounts (identifier_normalized)
    WHERE type = 'email' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_accounts_user_email_active
    ON accounts (user_id, identifier_normalized)
    WHERE type = 'email' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_accounts_verification_state
    ON accounts (verification_state)
    WHERE type = 'email' AND deleted_at IS NULL;

-- A historical shared mailbox is represented as metadata only. It is not a
-- login identifier, a verification destination, or a user-account binding.
CREATE TABLE IF NOT EXISTS identity_legacy_email_placeholders (
    id                BIGINT PRIMARY KEY,
    user_id           BIGINT NOT NULL,
    placeholder_email VARCHAR(320) NOT NULL,
    migration_source  VARCHAR(128) NOT NULL DEFAULT '',
    resolved_at       TIMESTAMP NULL,
    created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_identity_legacy_placeholder_user
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT chk_identity_legacy_placeholder_email
        CHECK (placeholder_email = lower(btrim(placeholder_email)))
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_identity_legacy_placeholder_user
    ON identity_legacy_email_placeholders (user_id)
    WHERE resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_identity_legacy_placeholder_email
    ON identity_legacy_email_placeholders (placeholder_email)
    WHERE resolved_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_reserved_identifiers (
    identifier_type       VARCHAR(32) NOT NULL,
    identifier_normalized VARCHAR(320) NOT NULL,
    reason                VARCHAR(128) NOT NULL,
    created_at            TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (identifier_type, identifier_normalized),
    CONSTRAINT chk_identity_reserved_identifier_normalized
        CHECK (identifier_normalized = lower(btrim(identifier_normalized)))
);

INSERT INTO identity_reserved_identifiers (identifier_type, identifier_normalized, reason)
VALUES ('email', '1904650862@qq.com', 'legacy_shared_placeholder')
ON CONFLICT (identifier_type, identifier_normalized) DO NOTHING;
