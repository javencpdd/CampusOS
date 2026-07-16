DO $$
DECLARE
    missing TEXT;
BEGIN
    SELECT string_agg(item, ', ' ORDER BY item) INTO missing
    FROM unnest(ARRAY[
        'users', 'accounts', 'sessions', 'roles', 'user_roles', 'permissions',
        'permission_definitions', 'role_permissions', 'route_operations', 'route_permission_bindings', 'authorization_audits',
        'categories', 'threads', 'posts', 'plugins', 'plugin_permissions', 'plugin_logs',
        'user_spaces', 'user_space_contents', 'richtext_article_contents',
        'richtext_article_assets', 'content_revisions', 'content_moderation_cases', 'content_moderation_actions',
        'webhook_endpoints', 'webhook_deliveries'
    ]) item
    WHERE to_regclass('public.' || item) IS NULL;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract missing tables: %', missing;
    END IF;

    SELECT string_agg(expected, ', ' ORDER BY expected) INTO missing
    FROM unnest(ARRAY[
        'users.id', 'users.status', 'threads.author_id', 'threads.category_id', 'threads.status',
        'threads.publication_status', 'threads.moderation_status', 'threads.deletion_status', 'threads.current_revision',
        'posts.thread_id', 'posts.author_id', 'posts.floor_number', 'user_roles.scope_type',
        'user_roles.scope_id', 'plugins.checksum', 'plugins.package_size',
        'plugins.backend_state', 'plugins.frontend_state', 'plugins.health_state', 'plugins.ui_revision',
        'user_spaces.style_manifest', 'plugin_catalog_entries.experience',
        'permission_definitions.code', 'role_permissions.permission_id', 'route_operations.operation_code',
        'route_permission_bindings.route_operation_id', 'authorization_audits.permission_code'
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
        'chk_threads_publication_status', 'chk_threads_moderation_status', 'chk_threads_deletion_status',
        'chk_permission_definition_code', 'chk_permission_definition_risk', 'chk_permission_definition_audit',
        'chk_route_operation_code', 'chk_authorization_audits_outcome',
        'fk_accounts_user', 'fk_threads_author', 'fk_threads_category',
        'fk_posts_thread', 'fk_posts_author', 'fk_user_roles_user', 'fk_user_roles_role',
        'fk_permissions_role', 'fk_user_spaces_user', 'fk_richtext_contents_thread',
        'fk_v10_role_permissions_role', 'fk_v10_role_permissions_permission',
        'fk_route_permission_operation', 'fk_route_permission_definition',
        'chk_plugins_backend_state', 'chk_plugins_frontend_state', 'chk_plugins_health_state'
    ]) expected
    WHERE NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = expected AND convalidated
    );
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract missing validated constraints: %', missing;
    END IF;

    SELECT string_agg(expected, ', ' ORDER BY expected) INTO missing
    FROM unnest(ARRAY[
        'uk_users_username', 'uk_users_email', 'idx_threads_category_id', 'idx_threads_visibility_v10',
        'idx_posts_thread_floor', 'idx_sessions_expires_at', 'idx_user_roles_scope_lookup',
        'idx_plugins_runtime_state', 'uk_permission_definitions_code', 'uk_route_operations_code',
        'uk_role_permissions_active', 'uk_route_permission_bindings_active'
    ]) expected
    WHERE to_regclass('public.' || expected) IS NULL;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract missing indexes: %', missing;
    END IF;
END $$;

SELECT jsonb_pretty(jsonb_build_object(
    'schema_contract', 'v0.10-governance-v1',
    'database', current_database(),
    'validated_at', now(),
    'status', 'pass'
)) AS schema_contract;
