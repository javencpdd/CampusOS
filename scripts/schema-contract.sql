DO $$
DECLARE
    missing TEXT;
BEGIN
    SELECT string_agg(item, ', ' ORDER BY item) INTO missing
    FROM unnest(ARRAY[
        'users', 'accounts', 'identity_admin_accounts', 'sessions', 'roles', 'user_roles', 'permissions',
        'identity_legacy_email_placeholders', 'identity_reserved_identifiers',
        'identity_email_challenges', 'identity_challenge_rate_limits', 'identity_challenge_policies', 'identity_account_recovery_cases',
        'permission_definitions', 'role_permissions', 'route_operations', 'route_permission_bindings', 'authorization_audits',
        'categories', 'threads', 'posts', 'category_thread_type_policies', 'mutual_aid_details', 'secondhand_details', 'plugins', 'plugin_permissions', 'plugin_logs',
        'user_spaces', 'user_space_contents', 'richtext_article_contents',
        'richtext_article_assets', 'content_revisions', 'content_moderation_cases', 'content_moderation_actions',
        'webhook_endpoints', 'webhook_deliveries',
        'platform_outbox', 'outbox_consumer_receipts', 'platform_outbox_attempts', 'platform_command_audits', 'platform_worker_leases',
        'platform_operation_runs', 'platform_compatibility_usage', 'platform_retention_runs'
    ]) item
    WHERE to_regclass('public.' || item) IS NULL;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract missing tables: %', missing;
    END IF;

    SELECT string_agg(expected, ', ' ORDER BY expected) INTO missing
    FROM unnest(ARRAY[
        'users.id', 'users.status', 'users.auth_version', 'users.must_change_password',
        'accounts.identifier_normalized', 'accounts.verification_state', 'accounts.credential_version',
        'identity_admin_accounts.user_id', 'identity_admin_accounts.credential_account_id',
        'identity_admin_accounts.status', 'identity_admin_accounts.version',
        'identity_email_challenges.public_id', 'identity_email_challenges.purpose',
        'identity_email_challenges.ticket_digest', 'identity_challenge_rate_limits.scope',
        'identity_challenge_policies.email_window_minutes', 'identity_challenge_policies.email_max_requests',
        'identity_challenge_policies.ip_window_minutes', 'identity_challenge_policies.ip_max_requests',
        'identity_challenge_policies.version',
        'identity_account_recovery_cases.public_id', 'identity_account_recovery_cases.user_id',
        'identity_account_recovery_cases.account_id', 'identity_account_recovery_cases.challenge_id',
        'identity_account_recovery_cases.status',
        'sessions.refresh_token_digest', 'sessions.token_family_id', 'sessions.revoked_at',
        'sessions.revoke_reason', 'sessions.ip_hash',
        'categories.node_kind', 'categories.lifecycle_status', 'categories.version', 'categories.color',
        'threads.author_id', 'threads.category_id', 'threads.status', 'threads.thread_type',
        'threads.publication_status', 'threads.moderation_status', 'threads.deletion_status', 'threads.current_revision',
        'category_thread_type_policies.category_id', 'category_thread_type_policies.thread_type',
        'category_thread_type_policies.enabled',
        'mutual_aid_details.thread_id', 'mutual_aid_details.aid_type', 'mutual_aid_details.aid_status',
        'mutual_aid_details.contact_mode', 'mutual_aid_details.version', 'mutual_aid_details.created_by',
        'secondhand_details.thread_id', 'secondhand_details.price_minor', 'secondhand_details.currency',
        'secondhand_details.item_condition', 'secondhand_details.trade_method', 'secondhand_details.trade_status',
        'secondhand_details.location_scope', 'secondhand_details.version', 'secondhand_details.created_by',
        'posts.thread_id', 'posts.author_id', 'posts.floor_number', 'posts.parent_floor_number', 'user_roles.scope_type',
        'user_roles.scope_id', 'plugins.checksum', 'plugins.package_size',
        'plugins.backend_state', 'plugins.frontend_state', 'plugins.health_state', 'plugins.ui_revision',
        'user_spaces.style_manifest', 'plugin_catalog_entries.experience',
        'permission_definitions.code', 'role_permissions.permission_id', 'route_operations.operation_code',
        'route_permission_bindings.route_operation_id', 'authorization_audits.permission_code',
        'authorization_audits.command_id', 'platform_outbox.status', 'platform_outbox.schema_version',
        'platform_command_audits.command_code', 'platform_operation_runs.status',
        'webhook_deliveries.delivery_key', 'webhook_endpoints.max_concurrent', 'webhook_endpoints.rate_limit_per_minute', 'outbox_consumer_receipts.consumer_name', 'platform_outbox_attempts.status',
        'platform_retention_runs.target'
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
        'chk_users_status', 'chk_users_auth_version', 'chk_accounts_identifier_normalized',
        'chk_accounts_verification_state', 'chk_accounts_credential_version',
        'chk_identity_admin_account_status', 'chk_identity_admin_account_version',
        'chk_identity_admin_account_revocation',
        'chk_identity_email_challenge_purpose', 'chk_identity_email_challenge_email_normalized',
        'chk_identity_email_challenge_attempts', 'chk_identity_email_challenge_ticket_state',
        'chk_identity_challenge_rate_scope', 'chk_identity_challenge_rate_count',
        'chk_identity_challenge_policy_id', 'chk_identity_challenge_policy_email_window',
        'chk_identity_challenge_policy_email_limit', 'chk_identity_challenge_policy_ip_window',
        'chk_identity_challenge_policy_ip_limit', 'chk_identity_challenge_policy_version',
        'chk_identity_recovery_case_status', 'chk_identity_recovery_case_target_email',
        'chk_identity_recovery_case_completion',
        'ck_sessions_refresh_token_cleared', 'ck_sessions_refresh_token_digest_shape',
        'chk_categories_node_kind', 'chk_categories_lifecycle_status', 'chk_categories_version',
        'chk_categories_color', 'chk_categories_group_root',
        'chk_threads_status', 'chk_threads_thread_type', 'chk_posts_status',
        'chk_threads_publication_status', 'chk_threads_moderation_status', 'chk_threads_deletion_status',
        'fk_category_thread_type_policy_category', 'chk_category_thread_type_policy_type',
        'chk_category_thread_type_policy_enabled',
        'fk_mutual_aid_details_thread', 'fk_mutual_aid_details_created_by',
        'chk_mutual_aid_details_type', 'chk_mutual_aid_details_status',
        'chk_mutual_aid_details_contact_mode', 'chk_mutual_aid_details_location_scope',
        'chk_mutual_aid_details_version', 'chk_mutual_aid_details_deadline',
        'fk_secondhand_details_thread', 'fk_secondhand_details_created_by',
        'chk_secondhand_details_price_minor', 'chk_secondhand_details_currency',
        'chk_secondhand_details_condition', 'chk_secondhand_details_trade_method',
        'chk_secondhand_details_trade_status', 'chk_secondhand_details_location_scope',
        'chk_secondhand_details_version',
        'chk_permission_definition_code', 'chk_permission_definition_risk', 'chk_permission_definition_audit',
        'chk_route_operation_code', 'chk_authorization_audits_outcome',
        'chk_platform_outbox_status', 'chk_platform_outbox_attempts', 'chk_platform_outbox_attempt_status', 'chk_platform_operation_status',
        'chk_platform_retention_run_mode', 'chk_platform_retention_run_status',
        'chk_webhook_endpoint_max_concurrent', 'chk_webhook_endpoint_rate_limit',
        'fk_accounts_user', 'fk_identity_admin_account_user', 'fk_identity_admin_account_credential',
        'fk_identity_email_challenge_account', 'fk_identity_challenge_policy_updated_by', 'fk_identity_recovery_case_user',
        'fk_identity_recovery_case_account', 'fk_identity_recovery_case_challenge',
        'fk_identity_recovery_case_created_by', 'fk_threads_author', 'fk_threads_category',
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
        'uk_users_username', 'uk_users_email', 'uk_accounts_email_normalized',
        'uk_identity_admin_accounts_user', 'uk_identity_admin_accounts_credential', 'idx_identity_admin_accounts_status',
        'idx_accounts_user_email_active', 'idx_accounts_verification_state',
        'uk_identity_email_challenge_public_id', 'idx_identity_email_challenge_email_purpose_created',
        'idx_identity_challenge_rate_expiry',
        'uk_identity_recovery_case_public_id', 'uk_identity_recovery_case_challenge',
        'idx_identity_recovery_case_user_status', 'idx_identity_recovery_case_expiry',
        'uk_sessions_refresh_token_digest', 'idx_sessions_user_active', 'idx_sessions_token_family',
        'idx_categories_parent_active', 'idx_categories_lifecycle_kind',
        'idx_threads_category_id', 'idx_threads_visibility_v10', 'idx_threads_thread_type_created',
        'idx_category_thread_type_policies_enabled',
        'idx_mutual_aid_details_status_updated', 'idx_mutual_aid_details_created_by_updated',
        'idx_secondhand_details_status_updated', 'idx_secondhand_details_created_by_updated',
        'idx_posts_thread_floor', 'idx_sessions_expires_at', 'idx_user_roles_scope_lookup',
        'idx_plugins_runtime_state', 'uk_permission_definitions_code', 'uk_route_operations_code',
        'uk_role_permissions_active', 'uk_route_permission_bindings_active',
        'uk_platform_outbox_idempotency', 'uk_platform_operation_idempotency',
        'uk_webhook_deliveries_delivery_key'
    ]) expected
    WHERE to_regclass('public.' || expected) IS NULL;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract missing indexes: %', missing;
    END IF;
END $$;

SELECT jsonb_pretty(jsonb_build_object(
    'schema_contract', 'v0.12-admin-plane-v2',
    'database', current_database(),
    'validated_at', now(),
    'status', 'pass'
)) AS schema_contract;
