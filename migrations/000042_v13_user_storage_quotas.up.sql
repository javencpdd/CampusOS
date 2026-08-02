-- Per-user User Storage quota overrides. The absence of a row means that the
-- current application default applies, so future default changes do not need
-- to rewrite every user.

CREATE TABLE IF NOT EXISTS user_storage_quotas (
    user_id       BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    quota_bytes   BIGINT NOT NULL CHECK (quota_bytes > 0),
    updated_by    BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_storage_quotas_updated_at
    ON user_storage_quotas(updated_at DESC);
