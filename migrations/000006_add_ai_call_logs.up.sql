-- AI Gateway 调用日志

CREATE TABLE IF NOT EXISTS ai_call_logs (
    id                 BIGINT PRIMARY KEY,
    provider           VARCHAR(64) NOT NULL,
    model              VARCHAR(128) NOT NULL DEFAULT '',
    source             VARCHAR(128) NOT NULL DEFAULT '',
    status             VARCHAR(32) NOT NULL,
    duration_ms        BIGINT NOT NULL DEFAULT 0,
    prompt_tokens      INTEGER NOT NULL DEFAULT 0,
    completion_tokens  INTEGER NOT NULL DEFAULT 0,
    total_tokens       INTEGER NOT NULL DEFAULT 0,
    error_message      TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at         TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_call_logs_created
    ON ai_call_logs(created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_ai_call_logs_provider_created
    ON ai_call_logs(provider, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_ai_call_logs_status
    ON ai_call_logs(status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_ai_call_logs_source
    ON ai_call_logs(source)
    WHERE deleted_at IS NULL AND source != '';
