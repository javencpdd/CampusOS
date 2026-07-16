DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permission_definitions
    WHERE code IN ('platform.reliability.read', 'platform.reliability.replay', 'platform.retention.preview')
);
DELETE FROM permission_definitions
WHERE code IN ('platform.reliability.read', 'platform.reliability.replay', 'platform.retention.preview');

DROP INDEX IF EXISTS idx_authorization_audits_command;
ALTER TABLE authorization_audits
    DROP COLUMN IF EXISTS resource_version,
    DROP COLUMN IF EXISTS trace_id,
    DROP COLUMN IF EXISTS command_id;

DROP INDEX IF EXISTS idx_webhook_deliveries_retry;
DROP INDEX IF EXISTS uk_webhook_deliveries_delivery_key;
ALTER TABLE webhook_deliveries DROP CONSTRAINT IF EXISTS fk_webhook_deliveries_outbox;
ALTER TABLE webhook_deliveries
    DROP COLUMN IF EXISTS dead_lettered_at,
    DROP COLUMN IF EXISTS next_attempt_at,
    DROP COLUMN IF EXISTS delivery_key,
    DROP COLUMN IF EXISTS outbox_event_id;

ALTER TABLE webhook_endpoints DROP CONSTRAINT IF EXISTS chk_webhook_endpoint_rate_limit;
ALTER TABLE webhook_endpoints DROP CONSTRAINT IF EXISTS chk_webhook_endpoint_max_concurrent;
ALTER TABLE webhook_endpoints
    DROP COLUMN IF EXISTS rate_limit_per_minute,
    DROP COLUMN IF EXISTS max_concurrent;

DROP INDEX IF EXISTS idx_platform_retention_runs_created;
DROP TABLE IF EXISTS platform_retention_runs;
DROP INDEX IF EXISTS idx_platform_compatibility_last_seen;
DROP TABLE IF EXISTS platform_compatibility_usage;
DROP INDEX IF EXISTS idx_outbox_consumer_receipts_event;
DROP TABLE IF EXISTS outbox_consumer_receipts;
DROP INDEX IF EXISTS idx_platform_outbox_attempts_event;
DROP TABLE IF EXISTS platform_outbox_attempts;
DROP INDEX IF EXISTS uk_platform_operation_idempotency;
DROP INDEX IF EXISTS idx_platform_operation_runs_subject;
DROP TABLE IF EXISTS platform_operation_runs;
DROP TABLE IF EXISTS platform_worker_leases;
DROP INDEX IF EXISTS idx_platform_command_audits_actor;
DROP INDEX IF EXISTS idx_platform_command_audits_created;
DROP TABLE IF EXISTS platform_command_audits;
DROP INDEX IF EXISTS uk_platform_outbox_idempotency;
DROP INDEX IF EXISTS idx_platform_outbox_dead;
DROP INDEX IF EXISTS idx_platform_outbox_claim;
DROP TABLE IF EXISTS platform_outbox;
