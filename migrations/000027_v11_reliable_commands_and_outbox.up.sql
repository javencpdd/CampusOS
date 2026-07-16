-- v0.11 reliable command boundary. These tables are additive: existing event
-- bus consumers and audit logs remain readable while new command paths gain a
-- transactional source of truth and restart-safe delivery queue.

CREATE TABLE IF NOT EXISTS platform_outbox (
    id                VARCHAR(64) PRIMARY KEY,
    event_type        VARCHAR(160) NOT NULL,
    schema_version    VARCHAR(64) NOT NULL DEFAULT 'v1',
    aggregate_type    VARCHAR(80) NOT NULL DEFAULT '',
    aggregate_id      VARCHAR(128) NOT NULL DEFAULT '',
    payload           JSONB NOT NULL DEFAULT '{}',
    headers           JSONB NOT NULL DEFAULT '{}',
    status            VARCHAR(16) NOT NULL DEFAULT 'pending',
    idempotency_key   VARCHAR(255) NULL,
    attempts          INTEGER NOT NULL DEFAULT 0,
    max_attempts      INTEGER NOT NULL DEFAULT 8,
    available_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    lease_owner       VARCHAR(160) NULL,
    lease_until       TIMESTAMP NULL,
    lease_generation  BIGINT NOT NULL DEFAULT 0,
    last_error        TEXT NULL,
    dead_lettered_at  TIMESTAMP NULL,
    created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_platform_outbox_status CHECK (status IN ('pending', 'processing', 'published', 'retry', 'dead')),
    CONSTRAINT chk_platform_outbox_attempts CHECK (attempts >= 0 AND max_attempts > 0)
);
ALTER TABLE platform_outbox
    ADD COLUMN IF NOT EXISTS schema_version VARCHAR(64) NOT NULL DEFAULT 'v1';

CREATE INDEX IF NOT EXISTS idx_platform_outbox_claim
    ON platform_outbox(status, available_at, created_at)
    WHERE status IN ('pending', 'retry');
CREATE INDEX IF NOT EXISTS idx_platform_outbox_dead
    ON platform_outbox(dead_lettered_at DESC)
    WHERE status = 'dead';
CREATE UNIQUE INDEX IF NOT EXISTS uk_platform_outbox_idempotency
    ON platform_outbox(idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS outbox_consumer_receipts (
    consumer_name     VARCHAR(160) NOT NULL,
    event_id          VARCHAR(64) NOT NULL,
    attempt           INTEGER NOT NULL DEFAULT 0,
    delivered_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (consumer_name, event_id),
    CONSTRAINT fk_outbox_consumer_receipt_event
        FOREIGN KEY (event_id) REFERENCES platform_outbox(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_outbox_consumer_receipts_event
    ON outbox_consumer_receipts(event_id, delivered_at DESC);

CREATE TABLE IF NOT EXISTS platform_outbox_attempts (
    id                VARCHAR(96) PRIMARY KEY,
    event_id          VARCHAR(64) NOT NULL,
    consumer_name     VARCHAR(160) NOT NULL,
    worker_id         VARCHAR(160) NOT NULL,
    lease_generation  BIGINT NOT NULL,
    attempt           INTEGER NOT NULL,
    status            VARCHAR(24) NOT NULL,
    error_message     TEXT NULL,
    started_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    finished_at       TIMESTAMP NULL,
    CONSTRAINT fk_platform_outbox_attempt_event
        FOREIGN KEY (event_id) REFERENCES platform_outbox(id) ON DELETE CASCADE,
    CONSTRAINT chk_platform_outbox_attempt_status
        CHECK (status IN ('running', 'succeeded', 'retry', 'dead', 'skipped'))
);
CREATE INDEX IF NOT EXISTS idx_platform_outbox_attempts_event
    ON platform_outbox_attempts(event_id, started_at DESC);

CREATE TABLE IF NOT EXISTS platform_command_audits (
    id                VARCHAR(64) PRIMARY KEY,
    command_id        VARCHAR(64) NOT NULL,
    command_code      VARCHAR(160) NOT NULL,
    actor_id          VARCHAR(64) NULL,
    actor_type        VARCHAR(32) NULL,
    resource_type     VARCHAR(80) NULL,
    resource_id       VARCHAR(128) NULL,
    operation_code    VARCHAR(200) NULL,
    permission_code   VARCHAR(160) NULL,
    request_id        VARCHAR(128) NULL,
    trace_id          VARCHAR(128) NULL,
    event_id          VARCHAR(64) NULL,
    details           JSONB NOT NULL DEFAULT '{}',
    created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_platform_command_audit_event FOREIGN KEY (event_id) REFERENCES platform_outbox(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_platform_command_audits_created
    ON platform_command_audits(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_platform_command_audits_actor
    ON platform_command_audits(actor_id, created_at DESC);

CREATE TABLE IF NOT EXISTS platform_worker_leases (
    worker_id          VARCHAR(160) PRIMARY KEY,
    last_heartbeat_at  TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS platform_operation_runs (
    id                VARCHAR(96) PRIMARY KEY,
    kind              VARCHAR(120) NOT NULL,
    subject_type      VARCHAR(80) NOT NULL,
    subject_id        VARCHAR(160) NOT NULL,
    status            VARCHAR(24) NOT NULL DEFAULT 'pending',
    actor_id          VARCHAR(64) NULL,
    idempotency_key   VARCHAR(255) NULL,
    details           JSONB NOT NULL DEFAULT '{}',
    error_message     TEXT NULL,
    created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_platform_operation_status CHECK (status IN ('pending', 'running', 'compensating', 'succeeded', 'failed'))
);
CREATE INDEX IF NOT EXISTS idx_platform_operation_runs_subject
    ON platform_operation_runs(kind, subject_type, subject_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uk_platform_operation_idempotency
    ON platform_operation_runs(idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS platform_compatibility_usage (
    usage_key        VARCHAR(255) PRIMARY KEY,
    usage_kind       VARCHAR(80) NOT NULL,
    detail           JSONB NOT NULL DEFAULT '{}',
    first_seen       TIMESTAMP NOT NULL DEFAULT NOW(),
    last_seen        TIMESTAMP NOT NULL DEFAULT NOW(),
    usage_count      BIGINT NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_platform_compatibility_last_seen
    ON platform_compatibility_usage(last_seen DESC);

CREATE TABLE IF NOT EXISTS platform_retention_runs (
    id                VARCHAR(96) PRIMARY KEY,
    target            VARCHAR(80) NOT NULL,
    before_at         TIMESTAMP NOT NULL,
    eligible_rows     BIGINT NOT NULL DEFAULT 0,
    mode              VARCHAR(24) NOT NULL DEFAULT 'dry-run',
    status            VARCHAR(24) NOT NULL DEFAULT 'completed',
    created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_platform_retention_run_mode CHECK (mode = 'dry-run'),
    CONSTRAINT chk_platform_retention_run_status CHECK (status IN ('completed', 'failed'))
);
CREATE INDEX IF NOT EXISTS idx_platform_retention_runs_created
    ON platform_retention_runs(created_at DESC);

ALTER TABLE webhook_deliveries
    ADD COLUMN IF NOT EXISTS outbox_event_id VARCHAR(64) NULL,
    ADD COLUMN IF NOT EXISTS delivery_key VARCHAR(255) NULL,
    ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMP NULL,
    ADD COLUMN IF NOT EXISTS dead_lettered_at TIMESTAMP NULL;

-- Endpoint-level flow control is stored with the endpoint so all Worker
-- instances enforce the same declared limits. The in-process limiter is a
-- first protective layer; future clustered limits may use a shared adapter.
ALTER TABLE webhook_endpoints
    ADD COLUMN IF NOT EXISTS max_concurrent INTEGER NOT NULL DEFAULT 2,
    ADD COLUMN IF NOT EXISTS rate_limit_per_minute INTEGER NOT NULL DEFAULT 60;
ALTER TABLE webhook_endpoints
    ADD CONSTRAINT chk_webhook_endpoint_max_concurrent CHECK (max_concurrent BETWEEN 1 AND 16) NOT VALID,
    ADD CONSTRAINT chk_webhook_endpoint_rate_limit CHECK (rate_limit_per_minute BETWEEN 1 AND 600) NOT VALID;
ALTER TABLE webhook_endpoints VALIDATE CONSTRAINT chk_webhook_endpoint_max_concurrent;
ALTER TABLE webhook_endpoints VALIDATE CONSTRAINT chk_webhook_endpoint_rate_limit;

ALTER TABLE webhook_deliveries
    ADD CONSTRAINT fk_webhook_deliveries_outbox
    FOREIGN KEY (outbox_event_id) REFERENCES platform_outbox(id) ON DELETE SET NULL NOT VALID;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT fk_webhook_deliveries_outbox;

CREATE UNIQUE INDEX IF NOT EXISTS uk_webhook_deliveries_delivery_key
    ON webhook_deliveries(delivery_key)
    WHERE delivery_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_retry
    ON webhook_deliveries(status, next_attempt_at)
    WHERE status IN ('pending', 'retry');

-- Keep the older authorization audit table operational. The additive linkage
-- makes command correlation available to reporting without rewriting history.
ALTER TABLE authorization_audits
    ADD COLUMN IF NOT EXISTS command_id VARCHAR(64) NULL,
    ADD COLUMN IF NOT EXISTS trace_id VARCHAR(128) NULL,
    ADD COLUMN IF NOT EXISTS resource_version VARCHAR(128) NULL;
CREATE INDEX IF NOT EXISTS idx_authorization_audits_command
    ON authorization_audits(command_id, created_at DESC)
    WHERE command_id IS NOT NULL;

-- Reliability administration has stable Permission Codes. The legacy pairs
-- remain mapped during the v10 compatibility window, but the route contract
-- and role matrix no longer depend on a generic admin role name.
INSERT INTO permission_definitions (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, created_at, updated_at) VALUES
    (900000000000001010, 'platform.reliability.read', 'platform', 'reliability', 'read', '查看可靠任务状态', 'medium', '["global"]'::jsonb, 'standard', NOW(), NOW()),
    (900000000000001011, 'platform.reliability.replay', 'platform', 'reliability', 'replay', '重放可靠任务', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001012, 'platform.retention.preview', 'platform', 'retention', 'preview', '执行保留策略预演', 'medium', '["global"]'::jsonb, 'standard', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000200000 + 100 + ROW_NUMBER() OVER (ORDER BY pd.id), r.id, pd.id, 'v11-reliability', NOW()
FROM roles r
INNER JOIN permission_definitions pd ON pd.code IN ('platform.reliability.read', 'platform.reliability.replay', 'platform.retention.preview')
WHERE r.name = 'admin' AND r.deleted_at IS NULL
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;
