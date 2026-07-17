-- Down is for isolated migration drills only. It never reconstructs a raw
-- refresh token: revoked placeholders merely satisfy the pre-v12 NOT NULL
-- column so an older binary can start in controlled read-only recovery mode.

DROP INDEX IF EXISTS idx_sessions_token_family;
DROP INDEX IF EXISTS idx_sessions_user_active;
DROP INDEX IF EXISTS uk_sessions_refresh_token_digest;

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS ck_sessions_refresh_token_cleared,
    DROP CONSTRAINT IF EXISTS ck_sessions_refresh_token_digest_shape;

UPDATE sessions
SET refresh_token = 'v12-revoked-' || id::text
WHERE refresh_token IS NULL;

ALTER TABLE sessions
    ALTER COLUMN refresh_token SET NOT NULL,
    DROP COLUMN IF EXISTS revoke_reason,
    DROP COLUMN IF EXISTS revoked_at,
    DROP COLUMN IF EXISTS ip_hash,
    DROP COLUMN IF EXISTS rotated_to_id,
    DROP COLUMN IF EXISTS rotated_from_id,
    DROP COLUMN IF EXISTS token_family_id,
    DROP COLUMN IF EXISTS refresh_token_digest;

CREATE UNIQUE INDEX IF NOT EXISTS uk_sessions_refresh_token
    ON sessions(refresh_token)
    WHERE deleted_at IS NULL;
