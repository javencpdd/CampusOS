-- V12-02d: independent admin session store. Admin sessions carry their own
-- refresh digests and MFA strength facts and never mix with user sessions;
-- the same account or family cannot cross domains.
CREATE TABLE public.admin_sessions (
    id VARCHAR(64) PRIMARY KEY,
    account_id VARCHAR(128) NOT NULL REFERENCES public.admin_accounts(id) ON DELETE CASCADE,
    refresh_token_digest VARCHAR(64) NOT NULL,
    token_family_id VARCHAR(32) NOT NULL,
    rotated_from_id VARCHAR(32),
    rotated_to_id VARCHAR(32),
    device_name VARCHAR(128) NOT NULL DEFAULT '',
    user_agent VARCHAR(512) NOT NULL DEFAULT '',
    ip_hash VARCHAR(64) NOT NULL DEFAULT '',
    authentication_strength VARCHAR(16) NOT NULL DEFAULT 'password',
    mfa_authenticated_at TIMESTAMPTZ,
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    revoke_reason VARCHAR(64) NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT chk_admin_sessions_strength CHECK (authentication_strength IN ('password', 'mfa')),
    CONSTRAINT chk_admin_sessions_digest CHECK (refresh_token_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_admin_sessions_revocation CHECK ((revoked_at IS NULL AND revoke_reason = '') OR (revoked_at IS NOT NULL AND revoke_reason <> '')),
    CONSTRAINT chk_admin_sessions_version CHECK (version >= 1)
);
CREATE UNIQUE INDEX uq_admin_sessions_refresh ON public.admin_sessions (refresh_token_digest);
CREATE INDEX idx_admin_sessions_account ON public.admin_sessions (account_id);
