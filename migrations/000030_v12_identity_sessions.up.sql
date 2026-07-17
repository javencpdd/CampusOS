-- v12 Session authority: refresh JWTs are retired in favour of opaque,
-- rotating tokens whose SHA-256 digest is the only persisted credential.
-- Existing rows cannot be made safe by reinterpretation, so they are revoked
-- and their raw refresh values are cleared during the migration.

DROP INDEX IF EXISTS uk_sessions_refresh_token;

ALTER TABLE sessions
    ALTER COLUMN refresh_token DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS refresh_token_digest VARCHAR(64),
    ADD COLUMN IF NOT EXISTS token_family_id VARCHAR(32),
    ADD COLUMN IF NOT EXISTS rotated_from_id VARCHAR(32),
    ADD COLUMN IF NOT EXISTS rotated_to_id VARCHAR(32),
    ADD COLUMN IF NOT EXISTS ip_hash VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMP NULL,
    ADD COLUMN IF NOT EXISTS revoke_reason VARCHAR(64) NOT NULL DEFAULT '';

UPDATE sessions
SET refresh_token = NULL,
    refresh_token_digest = NULL,
    token_family_id = COALESCE(NULLIF(token_family_id, ''), 'legacy-' || id::text),
    revoked_at = COALESCE(revoked_at, NOW()),
    revoke_reason = CASE
        WHEN revoke_reason = '' THEN 'v12_legacy_token_invalidated'
        ELSE revoke_reason
    END,
    updated_at = NOW()
WHERE refresh_token IS NOT NULL
   OR refresh_token_digest IS NOT NULL
   OR token_family_id IS NULL
   OR token_family_id = ''
   OR revoked_at IS NULL;

ALTER TABLE sessions
    ALTER COLUMN token_family_id SET NOT NULL;

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS ck_sessions_refresh_token_cleared,
    DROP CONSTRAINT IF EXISTS ck_sessions_refresh_token_digest_shape,
    ADD CONSTRAINT ck_sessions_refresh_token_cleared CHECK (refresh_token IS NULL),
    ADD CONSTRAINT ck_sessions_refresh_token_digest_shape CHECK (
        refresh_token_digest IS NULL OR refresh_token_digest ~ '^[0-9a-f]{64}$'
    );

CREATE UNIQUE INDEX IF NOT EXISTS uk_sessions_refresh_token_digest
    ON sessions(refresh_token_digest)
    WHERE refresh_token_digest IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_user_active
    ON sessions(user_id, last_active_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_token_family
    ON sessions(token_family_id)
    WHERE deleted_at IS NULL;
