-- V12-02d: independent admin MFA and recovery stores. TOTP methods, MFA
-- tickets and recovery tickets live only in the admin domain and never share
-- a table or digest with the user domain.
CREATE TABLE public.admin_mfa_totp (
    account_id VARCHAR(128) PRIMARY KEY REFERENCES public.admin_accounts(id) ON DELETE CASCADE,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    key_id VARCHAR(96) NOT NULL,
    nonce TEXT NOT NULL,
    ciphertext TEXT NOT NULL,
    last_accepted_step BIGINT NOT NULL DEFAULT 0,
    enrollment_expires_at TIMESTAMPTZ NOT NULL,
    confirmed_at TIMESTAMPTZ,
    disabled_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_admin_mfa_totp_confirmation CHECK ((status <> 'active' OR confirmed_at IS NOT NULL)),
    CONSTRAINT chk_admin_mfa_totp_envelope CHECK ((length(key_id) > 0 AND length(nonce) >= 8 AND length(ciphertext) >= 16)),
    CONSTRAINT chk_admin_mfa_totp_status CHECK (status IN ('pending', 'active', 'disabled')),
    CONSTRAINT chk_admin_mfa_totp_step CHECK (last_accepted_step >= 0),
    CONSTRAINT chk_admin_mfa_totp_version CHECK (version >= 1)
);

-- Admin MFA tickets are the challenge store for admin login and step-up.
-- Consumption is single-use and attempts are bounded at the constraint level.
CREATE TABLE public.admin_mfa_tickets (
    id VARCHAR(64) PRIMARY KEY,
    account_id VARCHAR(128) NOT NULL REFERENCES public.admin_accounts(id) ON DELETE CASCADE,
    audience VARCHAR(16) NOT NULL,
    purpose VARCHAR(16) NOT NULL,
    ticket_digest VARCHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_admin_mfa_ticket_attempts CHECK ((attempts >= 0 AND max_attempts >= 1 AND max_attempts <= 10)),
    CONSTRAINT chk_admin_mfa_ticket_audience CHECK (audience IN ('admin')),
    CONSTRAINT chk_admin_mfa_ticket_purpose CHECK (purpose IN ('login', 'step_up', 'recovery')),
    CONSTRAINT chk_admin_mfa_ticket_digest CHECK (ticket_digest ~ '^[0-9a-f]{64}$')
);
CREATE UNIQUE INDEX uq_admin_mfa_tickets_digest ON public.admin_mfa_tickets (ticket_digest);
CREATE INDEX idx_admin_mfa_tickets_account ON public.admin_mfa_tickets (account_id);

-- Admin recovery tickets bind purpose and account in the admin domain; they
-- are single-use, expiring and attempt bounded, and never reuse user recovery
-- storage.
CREATE TABLE public.admin_recovery_tickets (
    id VARCHAR(64) PRIMARY KEY,
    account_id VARCHAR(128) NOT NULL REFERENCES public.admin_accounts(id) ON DELETE CASCADE,
    purpose VARCHAR(32) NOT NULL,
    ticket_digest VARCHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_admin_recovery_attempts CHECK ((attempts >= 0 AND max_attempts >= 1 AND max_attempts <= 10)),
    CONSTRAINT chk_admin_recovery_purpose CHECK (purpose IN ('password_reset', 'totp_recovery', 'account_restore')),
    CONSTRAINT chk_admin_recovery_digest CHECK (ticket_digest ~ '^[0-9a-f]{64}$')
);
CREATE UNIQUE INDEX uq_admin_recovery_tickets_digest ON public.admin_recovery_tickets (ticket_digest);
