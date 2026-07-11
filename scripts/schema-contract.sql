DO $$
DECLARE
    missing TEXT;
BEGIN
    SELECT string_agg(item, ', ' ORDER BY item) INTO missing
    FROM unnest(ARRAY[
        'users', 'accounts', 'sessions', 'roles', 'user_roles', 'permissions',
        'categories', 'threads', 'posts', 'plugins', 'plugin_permissions', 'plugin_logs',
        'user_spaces', 'user_space_contents', 'richtext_article_contents',
        'richtext_article_assets', 'webhook_endpoints', 'webhook_deliveries'
    ]) item
    WHERE to_regclass('public.' || item) IS NULL;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract missing tables: %', missing;
    END IF;

    SELECT string_agg(expected, ', ' ORDER BY expected) INTO missing
    FROM unnest(ARRAY[
        'users.id', 'users.status', 'threads.author_id', 'threads.category_id', 'threads.status',
        'posts.thread_id', 'posts.author_id', 'posts.floor_number', 'user_roles.scope_type',
        'user_roles.scope_id', 'plugins.checksum', 'plugins.package_size', 'user_spaces.style_manifest'
    ]) expected
    WHERE NOT EXISTS (
        SELECT 1 FROM information_schema.columns c
        WHERE c.table_schema = 'public'
          AND c.table_name = split_part(expected, '.', 1)
          AND c.column_name = split_part(expected, '.', 2)
    );
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract missing columns: %', missing;
    END IF;

    SELECT string_agg(expected, ', ' ORDER BY expected) INTO missing
    FROM unnest(ARRAY[
        'chk_users_status', 'chk_threads_status', 'chk_posts_status',
        'fk_accounts_user', 'fk_threads_author', 'fk_threads_category',
        'fk_posts_thread', 'fk_posts_author', 'fk_user_roles_user', 'fk_user_roles_role',
        'fk_permissions_role', 'fk_user_spaces_user', 'fk_richtext_contents_thread'
    ]) expected
    WHERE NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = expected AND convalidated
    );
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract missing validated constraints: %', missing;
    END IF;

    SELECT string_agg(expected, ', ' ORDER BY expected) INTO missing
    FROM unnest(ARRAY[
        'uk_users_username', 'uk_users_email', 'idx_threads_category_id',
        'idx_posts_thread_floor', 'idx_sessions_expires_at', 'idx_user_roles_scope_lookup'
    ]) expected
    WHERE to_regclass('public.' || expected) IS NULL;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract missing indexes: %', missing;
    END IF;
END $$;

SELECT jsonb_pretty(jsonb_build_object(
    'schema_contract', 'v0.6-core-v1',
    'database', current_database(),
    'validated_at', now(),
    'status', 'pass'
)) AS schema_contract;
