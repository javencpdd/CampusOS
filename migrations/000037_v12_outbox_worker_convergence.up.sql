-- v12 reliable worker convergence. Delivery attempts need an explicit failed
-- state when the worker cannot persist the requested outbox transition. The
-- existing v11 table and historical rows remain intact.

ALTER TABLE platform_outbox_attempts
    DROP CONSTRAINT IF EXISTS chk_platform_outbox_attempt_status;
ALTER TABLE platform_outbox_attempts
    ADD CONSTRAINT chk_platform_outbox_attempt_status
    CHECK (status IN ('running', 'succeeded', 'retry', 'dead', 'skipped', 'failed'));
