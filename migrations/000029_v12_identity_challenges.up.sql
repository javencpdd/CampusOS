-- v0.12 email challenge and one-time ticket foundation. Codes are derived from
-- an application HMAC key ring and are deliberately never persisted here.

CREATE TABLE IF NOT EXISTS identity_email_challenges (
    id                  BIGINT PRIMARY KEY,
    public_id           VARCHAR(96) NOT NULL,
    purpose             VARCHAR(32) NOT NULL,
    email_normalized    VARCHAR(320) NOT NULL,
    account_id          BIGINT NULL,
    key_id              VARCHAR(64) NOT NULL,
    nonce               VARCHAR(128) NOT NULL,
    expires_at          TIMESTAMP NOT NULL,
    attempt_count       INTEGER NOT NULL DEFAULT 0,
    max_attempts        INTEGER NOT NULL DEFAULT 5,
    verified_at         TIMESTAMP NULL,
    ticket_digest       VARCHAR(128) NULL,
    ticket_expires_at   TIMESTAMP NULL,
    consumed_at         TIMESTAMP NULL,
    invalidated_at      TIMESTAMP NULL,
    requested_ip_hash   VARCHAR(128) NOT NULL,
    created_at          TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_identity_email_challenge_account
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT,
    CONSTRAINT chk_identity_email_challenge_purpose
        CHECK (purpose IN ('registration', 'email_binding', 'password_reset')),
    CONSTRAINT chk_identity_email_challenge_email_normalized
        CHECK (email_normalized = lower(btrim(email_normalized))),
    CONSTRAINT chk_identity_email_challenge_attempts
        CHECK (attempt_count >= 0 AND max_attempts BETWEEN 1 AND 10 AND attempt_count <= max_attempts),
    CONSTRAINT chk_identity_email_challenge_ticket_state
        CHECK (
            (ticket_digest IS NULL AND ticket_expires_at IS NULL)
            OR (ticket_digest IS NOT NULL AND ticket_expires_at IS NOT NULL AND verified_at IS NOT NULL)
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_identity_email_challenge_public_id
    ON identity_email_challenges (public_id);
CREATE INDEX IF NOT EXISTS idx_identity_email_challenge_email_purpose_created
    ON identity_email_challenges (email_normalized, purpose, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_identity_email_challenge_delivery
    ON identity_email_challenges (id, expires_at)
    WHERE verified_at IS NULL AND invalidated_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_identity_email_challenge_expiry
    ON identity_email_challenges (expires_at, ticket_expires_at)
    WHERE consumed_at IS NULL;

-- Rate counters are keyed by keyed digests, never a raw IP address or email.
-- One command updates all relevant windows in the same transaction as the
-- challenge row, so a failed request cannot leave a usable code behind.
CREATE TABLE IF NOT EXISTS identity_challenge_rate_limits (
    scope               VARCHAR(32) NOT NULL,
    subject_digest      VARCHAR(128) NOT NULL,
    window_started_at   TIMESTAMP NOT NULL,
    request_count       INTEGER NOT NULL DEFAULT 0,
    updated_at          TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scope, subject_digest, window_started_at),
    CONSTRAINT chk_identity_challenge_rate_scope
        CHECK (scope IN ('email_minute', 'email_day', 'ip_hour')),
    CONSTRAINT chk_identity_challenge_rate_count
        CHECK (request_count >= 0 AND request_count <= 10000)
);
CREATE INDEX IF NOT EXISTS idx_identity_challenge_rate_expiry
    ON identity_challenge_rate_limits (window_started_at, updated_at);
