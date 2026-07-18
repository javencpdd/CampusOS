-- Down is intended for isolated migration drills. A failed finalize attempt
-- maps to retry evidence before restoring the v11 constraint.

UPDATE platform_outbox_attempts
SET status = 'retry',
    error_message = COALESCE(NULLIF(error_message, ''), 'outbox state transition failed')
WHERE status = 'failed';

ALTER TABLE platform_outbox_attempts
    DROP CONSTRAINT IF EXISTS chk_platform_outbox_attempt_status;
ALTER TABLE platform_outbox_attempts
    ADD CONSTRAINT chk_platform_outbox_attempt_status
    CHECK (status IN ('running', 'succeeded', 'retry', 'dead', 'skipped'));
