-- v0.6 first database integrity closure.
-- Preflight aborts before constraints are added if historical data violates the contract.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM users WHERE status NOT IN ('active', 'suspended', 'deactivated')) THEN
        RAISE EXCEPTION 'preflight failed: users.status contains unsupported values';
    END IF;
    IF EXISTS (SELECT 1 FROM threads WHERE status NOT IN ('draft', 'pending_review', 'published', 'private', 'archived')) THEN
        RAISE EXCEPTION 'preflight failed: threads.status contains unsupported values';
    END IF;
    IF EXISTS (SELECT 1 FROM posts WHERE status NOT IN ('published', 'deleted')) THEN
        RAISE EXCEPTION 'preflight failed: posts.status contains unsupported values';
    END IF;
    IF EXISTS (SELECT 1 FROM categories WHERE thread_count < 0 OR post_count < 0)
       OR EXISTS (SELECT 1 FROM threads WHERE view_count < 0 OR reply_count < 0 OR like_count < 0)
       OR EXISTS (SELECT 1 FROM posts WHERE like_count < 0 OR floor_number < 0) THEN
        RAISE EXCEPTION 'preflight failed: content counters cannot be negative';
    END IF;
END $$;

ALTER TABLE users
    ADD CONSTRAINT chk_users_status CHECK (status IN ('active', 'suspended', 'deactivated'));
ALTER TABLE threads
    ADD CONSTRAINT chk_threads_status CHECK (status IN ('draft', 'pending_review', 'published', 'private', 'archived')),
    ADD CONSTRAINT chk_threads_counters CHECK (view_count >= 0 AND reply_count >= 0 AND like_count >= 0);
ALTER TABLE posts
    ADD CONSTRAINT chk_posts_status CHECK (status IN ('published', 'deleted')),
    ADD CONSTRAINT chk_posts_counters CHECK (like_count >= 0 AND floor_number >= 0);
ALTER TABLE categories
    ADD CONSTRAINT chk_categories_counters CHECK (thread_count >= 0 AND post_count >= 0);
ALTER TABLE likes
    ADD CONSTRAINT chk_likes_target_type CHECK (target_type IN ('thread', 'post'));
ALTER TABLE plugins
    ADD CONSTRAINT chk_plugins_runtime CHECK (runtime IN ('builtin', 'grpc', 'wasm')),
    ADD CONSTRAINT chk_plugins_status CHECK (status IN ('installed', 'enabled', 'running', 'stopped', 'error')),
    ADD CONSTRAINT chk_plugins_package_size CHECK (package_size >= 0);

ALTER TABLE accounts
    ADD CONSTRAINT fk_accounts_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE sessions
    ADD CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE categories
    ADD CONSTRAINT fk_categories_parent FOREIGN KEY (parent_id) REFERENCES categories(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE threads
    ADD CONSTRAINT fk_threads_author FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT fk_threads_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE posts
    ADD CONSTRAINT fk_posts_thread FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT fk_posts_author FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT fk_posts_parent FOREIGN KEY (parent_id) REFERENCES posts(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE user_roles
    ADD CONSTRAINT fk_user_roles_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT fk_user_roles_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE permissions
    ADD CONSTRAINT fk_permissions_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE notifications
    ADD CONSTRAINT fk_notifications_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE configurations
    ADD CONSTRAINT fk_configurations_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE likes
    ADD CONSTRAINT fk_likes_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE user_spaces
    ADD CONSTRAINT fk_user_spaces_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE user_space_contents
    ADD CONSTRAINT fk_user_space_contents_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT fk_user_space_contents_thread FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT fk_user_space_contents_category FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE richtext_article_contents
    ADD CONSTRAINT fk_richtext_contents_thread FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT fk_richtext_contents_created_by FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT fk_richtext_contents_updated_by FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE richtext_article_assets
    ADD CONSTRAINT fk_richtext_assets_thread FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT fk_richtext_assets_content FOREIGN KEY (article_content_id) REFERENCES richtext_article_contents(id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT fk_richtext_assets_uploader FOREIGN KEY (uploader_id) REFERENCES users(id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE webhook_deliveries
    ADD CONSTRAINT fk_webhook_deliveries_endpoint FOREIGN KEY (endpoint_id) REFERENCES webhook_endpoints(id) ON DELETE RESTRICT NOT VALID;

ALTER TABLE accounts VALIDATE CONSTRAINT fk_accounts_user;
ALTER TABLE sessions VALIDATE CONSTRAINT fk_sessions_user;
ALTER TABLE categories VALIDATE CONSTRAINT fk_categories_parent;
ALTER TABLE threads VALIDATE CONSTRAINT fk_threads_author;
ALTER TABLE threads VALIDATE CONSTRAINT fk_threads_category;
ALTER TABLE posts VALIDATE CONSTRAINT fk_posts_thread;
ALTER TABLE posts VALIDATE CONSTRAINT fk_posts_author;
ALTER TABLE posts VALIDATE CONSTRAINT fk_posts_parent;
ALTER TABLE user_roles VALIDATE CONSTRAINT fk_user_roles_user;
ALTER TABLE user_roles VALIDATE CONSTRAINT fk_user_roles_role;
ALTER TABLE permissions VALIDATE CONSTRAINT fk_permissions_role;
ALTER TABLE notifications VALIDATE CONSTRAINT fk_notifications_user;
ALTER TABLE configurations VALIDATE CONSTRAINT fk_configurations_updated_by;
ALTER TABLE likes VALIDATE CONSTRAINT fk_likes_user;
ALTER TABLE user_spaces VALIDATE CONSTRAINT fk_user_spaces_user;
ALTER TABLE user_space_contents VALIDATE CONSTRAINT fk_user_space_contents_user;
ALTER TABLE user_space_contents VALIDATE CONSTRAINT fk_user_space_contents_thread;
ALTER TABLE user_space_contents VALIDATE CONSTRAINT fk_user_space_contents_category;
ALTER TABLE richtext_article_contents VALIDATE CONSTRAINT fk_richtext_contents_thread;
ALTER TABLE richtext_article_contents VALIDATE CONSTRAINT fk_richtext_contents_created_by;
ALTER TABLE richtext_article_contents VALIDATE CONSTRAINT fk_richtext_contents_updated_by;
ALTER TABLE richtext_article_assets VALIDATE CONSTRAINT fk_richtext_assets_thread;
ALTER TABLE richtext_article_assets VALIDATE CONSTRAINT fk_richtext_assets_content;
ALTER TABLE richtext_article_assets VALIDATE CONSTRAINT fk_richtext_assets_uploader;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT fk_webhook_deliveries_endpoint;

CREATE INDEX IF NOT EXISTS idx_sessions_expires_at
    ON sessions(expires_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_posts_thread_floor
    ON posts(thread_id, floor_number) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at
    ON api_keys(expires_at) WHERE deleted_at IS NULL AND is_active = TRUE;
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_status_created
    ON webhook_deliveries(status, created_at DESC);
