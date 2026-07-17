-- Down migrations are for isolated verification only. Production rollbacks use
-- forward-fix and must not use this to restore known default credentials.

DROP TABLE IF EXISTS identity_reserved_identifiers;

DROP INDEX IF EXISTS idx_identity_legacy_placeholder_email;
DROP INDEX IF EXISTS uk_identity_legacy_placeholder_user;
DROP TABLE IF EXISTS identity_legacy_email_placeholders;

DROP INDEX IF EXISTS idx_accounts_verification_state;
DROP INDEX IF EXISTS idx_accounts_user_email_active;
DROP INDEX IF EXISTS uk_accounts_email_normalized;
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS chk_accounts_credential_version;
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS chk_accounts_verification_state;
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS chk_accounts_identifier_normalized;
ALTER TABLE accounts
    DROP COLUMN IF EXISTS credential_version,
    DROP COLUMN IF EXISTS password_changed_at,
    DROP COLUMN IF EXISTS verification_source,
    DROP COLUMN IF EXISTS verified_at,
    DROP COLUMN IF EXISTS verification_state,
    DROP COLUMN IF EXISTS identifier_normalized;

ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_auth_version;
ALTER TABLE users
    DROP COLUMN IF EXISTS must_change_password,
    DROP COLUMN IF EXISTS auth_version;
