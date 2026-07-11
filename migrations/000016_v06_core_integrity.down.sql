DROP INDEX IF EXISTS idx_webhook_deliveries_status_created;
DROP INDEX IF EXISTS idx_api_keys_expires_at;
DROP INDEX IF EXISTS idx_posts_thread_floor;
DROP INDEX IF EXISTS idx_sessions_expires_at;

ALTER TABLE webhook_deliveries DROP CONSTRAINT IF EXISTS fk_webhook_deliveries_endpoint;
ALTER TABLE richtext_article_assets
    DROP CONSTRAINT IF EXISTS fk_richtext_assets_uploader,
    DROP CONSTRAINT IF EXISTS fk_richtext_assets_content,
    DROP CONSTRAINT IF EXISTS fk_richtext_assets_thread;
ALTER TABLE richtext_article_contents
    DROP CONSTRAINT IF EXISTS fk_richtext_contents_updated_by,
    DROP CONSTRAINT IF EXISTS fk_richtext_contents_created_by,
    DROP CONSTRAINT IF EXISTS fk_richtext_contents_thread;
ALTER TABLE user_space_contents
    DROP CONSTRAINT IF EXISTS fk_user_space_contents_category,
    DROP CONSTRAINT IF EXISTS fk_user_space_contents_thread,
    DROP CONSTRAINT IF EXISTS fk_user_space_contents_user;
ALTER TABLE user_spaces DROP CONSTRAINT IF EXISTS fk_user_spaces_user;
ALTER TABLE likes DROP CONSTRAINT IF EXISTS fk_likes_user;
ALTER TABLE configurations DROP CONSTRAINT IF EXISTS fk_configurations_updated_by;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS fk_notifications_user;
ALTER TABLE permissions DROP CONSTRAINT IF EXISTS fk_permissions_role;
ALTER TABLE user_roles
    DROP CONSTRAINT IF EXISTS fk_user_roles_role,
    DROP CONSTRAINT IF EXISTS fk_user_roles_user;
ALTER TABLE posts
    DROP CONSTRAINT IF EXISTS fk_posts_parent,
    DROP CONSTRAINT IF EXISTS fk_posts_author,
    DROP CONSTRAINT IF EXISTS fk_posts_thread;
ALTER TABLE threads
    DROP CONSTRAINT IF EXISTS fk_threads_category,
    DROP CONSTRAINT IF EXISTS fk_threads_author;
ALTER TABLE categories DROP CONSTRAINT IF EXISTS fk_categories_parent;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS fk_sessions_user;
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS fk_accounts_user;

ALTER TABLE plugins
    DROP CONSTRAINT IF EXISTS chk_plugins_package_size,
    DROP CONSTRAINT IF EXISTS chk_plugins_status,
    DROP CONSTRAINT IF EXISTS chk_plugins_runtime;
ALTER TABLE likes DROP CONSTRAINT IF EXISTS chk_likes_target_type;
ALTER TABLE categories DROP CONSTRAINT IF EXISTS chk_categories_counters;
ALTER TABLE posts
    DROP CONSTRAINT IF EXISTS chk_posts_counters,
    DROP CONSTRAINT IF EXISTS chk_posts_status;
ALTER TABLE threads
    DROP CONSTRAINT IF EXISTS chk_threads_counters,
    DROP CONSTRAINT IF EXISTS chk_threads_status;
ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_status;
