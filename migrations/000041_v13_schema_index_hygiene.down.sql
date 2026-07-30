-- Restore the narrower compatibility indexes for an isolated rollback drill.

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_notifications_user_id
    ON notifications(user_id) WHERE deleted_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_permissions_plugin
    ON plugin_permissions(plugin_name) WHERE deleted_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_records_search
    ON plugin_records(plugin_name, owner_type, owner_id)
    WHERE deleted_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_posts_thread_id
    ON posts(thread_id) WHERE deleted_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_role_permissions_role
    ON role_permissions(role_id) WHERE deleted_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_sessions_user_id
    ON sessions(user_id) WHERE deleted_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_threads_author_id
    ON threads(author_id) WHERE deleted_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_roles_user_id
    ON user_roles(user_id) WHERE deleted_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_webhook_deliveries_status
    ON webhook_deliveries(status);
