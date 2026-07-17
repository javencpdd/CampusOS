-- Down migrations are for isolated up/down/up verification. Production uses
-- forward-fix and must retain challenge/audit evidence while investigating.

DROP INDEX IF EXISTS idx_identity_challenge_rate_expiry;
DROP TABLE IF EXISTS identity_challenge_rate_limits;

DROP INDEX IF EXISTS idx_identity_email_challenge_expiry;
DROP INDEX IF EXISTS idx_identity_email_challenge_delivery;
DROP INDEX IF EXISTS idx_identity_email_challenge_email_purpose_created;
DROP INDEX IF EXISTS uk_identity_email_challenge_public_id;
DROP TABLE IF EXISTS identity_email_challenges;
