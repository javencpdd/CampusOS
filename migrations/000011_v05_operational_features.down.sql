DROP INDEX IF EXISTS idx_message_logs_platform_conversation;
DROP INDEX IF EXISTS idx_message_logs_created;
DROP TABLE IF EXISTS message_logs;

DROP INDEX IF EXISTS uk_message_bindings_platform_external;
DROP TABLE IF EXISTS message_bindings;

DROP INDEX IF EXISTS idx_mcp_audit_logs_tool;
DROP INDEX IF EXISTS idx_mcp_audit_logs_created;
DROP TABLE IF EXISTS mcp_audit_logs;

DROP INDEX IF EXISTS idx_webhook_deliveries_status;
DROP INDEX IF EXISTS idx_webhook_deliveries_endpoint_created;
DROP TABLE IF EXISTS webhook_deliveries;

DROP INDEX IF EXISTS idx_webhook_endpoints_enabled;
DROP TABLE IF EXISTS webhook_endpoints;

ALTER TABLE plugins
    DROP COLUMN IF EXISTS last_preflight_at,
    DROP COLUMN IF EXISTS package_size,
    DROP COLUMN IF EXISTS checksum;

DROP INDEX IF EXISTS idx_user_space_style_snapshots_user_created;
DROP TABLE IF EXISTS user_space_style_snapshots;

DROP INDEX IF EXISTS idx_user_spaces_disabled;
ALTER TABLE user_spaces
    DROP COLUMN IF EXISTS last_sync_error,
    DROP COLUMN IF EXISTS last_sync_at,
    DROP COLUMN IF EXISTS disabled_reason,
    DROP COLUMN IF EXISTS disabled_by,
    DROP COLUMN IF EXISTS disabled_at;
