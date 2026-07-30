-- v13 schema index hygiene.
--
-- Historical migrations remain immutable because deployed instances may have
-- any version recorded in schema_migrations. These nine indexes are removed by
-- a forward migration because each is a strict left-prefix of another B-tree
-- index with the same table and predicate. PostgreSQL can use the covering
-- index for the shorter lookup, so keeping both only duplicates write,
-- vacuum, backup and cache work.

-- The project migration runners execute files without an outer transaction,
-- which lets PostgreSQL avoid blocking normal writes while each index is
-- removed. Do not wrap this migration in BEGIN/COMMIT.
DROP INDEX CONCURRENTLY IF EXISTS idx_notifications_user_id;
DROP INDEX CONCURRENTLY IF EXISTS idx_plugin_permissions_plugin;
DROP INDEX CONCURRENTLY IF EXISTS idx_plugin_records_search;
DROP INDEX CONCURRENTLY IF EXISTS idx_posts_thread_id;
DROP INDEX CONCURRENTLY IF EXISTS idx_role_permissions_role;
DROP INDEX CONCURRENTLY IF EXISTS idx_sessions_user_id;
DROP INDEX CONCURRENTLY IF EXISTS idx_threads_author_id;
DROP INDEX CONCURRENTLY IF EXISTS idx_user_roles_user_id;
DROP INDEX CONCURRENTLY IF EXISTS idx_webhook_deliveries_status;
