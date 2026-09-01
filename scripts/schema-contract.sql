DO $$
DECLARE
    missing TEXT;
BEGIN
    SELECT string_agg(item, ', ' ORDER BY item) INTO missing
    FROM unnest(ARRAY[
        'schema_migrations', 'schema_migration_locks',
        'users', 'accounts', 'identity_admin_accounts', 'sessions', 'roles', 'user_roles',
        'identity_legacy_email_placeholders', 'identity_reserved_identifiers',
        'identity_email_challenges', 'identity_challenge_rate_limits', 'identity_challenge_policies', 'identity_account_recovery_cases',
        'permission_definitions', 'role_permissions', 'route_operations', 'route_permission_bindings', 'authorization_audits',
        'categories', 'threads', 'posts', 'category_thread_type_policies', 'mutual_aid_details', 'secondhand_details', 'plugins', 'plugin_permissions', 'plugin_logs',
        'plugin_publishers', 'plugin_versions', 'plugin_capability_declarations', 'plugin_admin_grants',
        'plugin_user_consents', 'plugin_delegations', 'plugin_secret_values', 'plugin_authorization_decisions',
        'user_spaces', 'user_space_contents', 'richtext_article_contents',
        'richtext_article_assets', 'content_revisions', 'content_moderation_cases', 'content_moderation_actions',
        'webhook_endpoints', 'webhook_deliveries',
        'platform_outbox', 'outbox_consumer_receipts', 'platform_outbox_attempts', 'platform_command_audits', 'platform_worker_leases',
        'platform_operation_runs', 'platform_compatibility_usage', 'platform_retention_runs',
        'academic_terms', 'user_storage_accounts', 'storage_objects', 'user_storage_reservations',
        'user_schedule_terms', 'user_schedule_preferences', 'personal_documents', 'personal_document_versions', 'personal_document_previews'
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
        'platform_retention_runs.target',
        'academic_terms.id', 'academic_terms.year', 'academic_terms.semester',
        'academic_terms.first_week_start', 'academic_terms.status', 'academic_terms.is_default',
        'academic_terms.version', 'academic_terms.created_by', 'academic_terms.updated_by',
        'user_storage_accounts.user_id', 'user_storage_accounts.used_bytes', 'user_storage_accounts.reserved_bytes', 'user_storage_accounts.version',
        'storage_objects.id', 'storage_objects.owner_user_id', 'storage_objects.namespace', 'storage_objects.purpose', 'storage_objects.provider', 'storage_objects.storage_key', 'storage_objects.size_bytes', 'storage_objects.sha256', 'storage_objects.status', 'storage_objects.version',
        'user_storage_reservations.id', 'user_storage_reservations.user_id', 'user_storage_reservations.object_id', 'user_storage_reservations.reserved_bytes', 'user_storage_reservations.status', 'user_storage_reservations.expires_at'
        ,'user_schedule_terms.user_id', 'user_schedule_terms.academic_term_id', 'user_schedule_terms.current_object_id', 'user_schedule_terms.first_week_start', 'user_schedule_terms.version',
        'user_schedule_preferences.user_id', 'user_schedule_preferences.academic_term_id',
        'personal_documents.id', 'personal_documents.owner_user_id', 'personal_documents.name', 'personal_documents.document_type', 'personal_documents.status', 'personal_documents.current_version_id', 'personal_documents.version',
        'personal_document_versions.id', 'personal_document_versions.document_id', 'personal_document_versions.version_number', 'personal_document_versions.source_object_id', 'personal_document_versions.source_type', 'personal_document_versions.size_bytes', 'personal_document_versions.sha256',
        'personal_document_previews.id', 'personal_document_previews.document_version_id', 'personal_document_previews.preview_object_id', 'personal_document_previews.status', 'personal_document_previews.attempts',
        'schema_migrations.checksum', 'schema_migrations.execution_ms', 'schema_migrations.executor',
        'plugins.publisher_id', 'plugin_publishers.slug', 'plugin_publishers.trust_status',
        'plugin_versions.plugin_id', 'plugin_versions.package_digest', 'plugin_versions.permission_fingerprint',
        'plugin_capability_declarations.plugin_version_id', 'plugin_capability_declarations.capability_code',
        'plugin_admin_grants.policy_revision', 'plugin_user_consents.purpose_hash',
        'plugin_delegations.token_digest', 'plugin_secret_values.ciphertext',
        'plugin_authorization_decisions.request_id', 'plugin_authorization_decisions.outcome'
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
        'ck_sessions_refresh_token_digest_shape',
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
        'chk_academic_terms_year', 'chk_academic_terms_semester', 'chk_academic_terms_first_week_monday',
        'chk_academic_terms_status', 'chk_academic_terms_default_open', 'chk_academic_terms_version',
        'chk_academic_terms_closed_at',
        'chk_user_storage_accounts_used', 'chk_user_storage_accounts_reserved', 'chk_user_storage_accounts_version',
        'chk_storage_objects_provider', 'chk_storage_objects_size', 'chk_storage_objects_status', 'chk_storage_objects_version', 'chk_storage_objects_deleted_at', 'chk_storage_objects_ready_payload',
        'chk_user_storage_reservations_bytes', 'chk_user_storage_reservations_status',
		'chk_personal_documents_type', 'chk_personal_documents_status', 'chk_personal_documents_version',
		'chk_personal_document_versions_number', 'chk_personal_document_versions_type', 'chk_personal_document_versions_size',
		'chk_personal_document_previews_status', 'chk_personal_document_previews_attempts',
        'chk_webhook_endpoint_max_concurrent', 'chk_webhook_endpoint_rate_limit',
        'fk_accounts_user', 'fk_identity_admin_account_user', 'fk_identity_admin_account_credential',
        'fk_identity_email_challenge_account', 'fk_identity_challenge_policy_updated_by', 'fk_identity_recovery_case_user',
        'fk_identity_recovery_case_account', 'fk_identity_recovery_case_challenge',
        'fk_identity_recovery_case_created_by', 'fk_threads_author', 'fk_threads_category',
        'fk_posts_thread', 'fk_posts_author', 'fk_user_roles_user', 'fk_user_roles_role',
        'fk_user_spaces_user', 'fk_richtext_contents_thread',
        'fk_v10_role_permissions_role', 'fk_v10_role_permissions_permission',
        'fk_route_permission_operation', 'fk_route_permission_definition',
        'chk_plugins_backend_state', 'chk_plugins_frontend_state', 'chk_plugins_health_state',
        'fk_plugins_publisher', 'chk_plugin_publishers_trust', 'chk_plugin_versions_lifecycle',
        'chk_plugin_capability_code', 'chk_plugin_admin_grants_status', 'chk_plugin_user_consents_status',
        'chk_plugin_delegations_status', 'chk_plugin_secret_values_payload', 'chk_plugin_authorization_outcome',
        'fk_plugin_authorization_declaration'
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
        'uk_webhook_deliveries_delivery_key',
        'uk_academic_terms_year_semester', 'uk_academic_terms_open_default', 'idx_academic_terms_status_year',
        'uk_storage_objects_provider_key', 'idx_storage_objects_owner_id_desc', 'idx_storage_objects_owner_namespace_purpose_id_desc', 'idx_storage_objects_status_updated_at',
        'uk_user_storage_reservations_object', 'idx_user_storage_reservations_pending_expiry',
        'idx_user_schedule_terms_academic_term', 'idx_user_schedule_terms_current_object', 'idx_user_schedule_preferences_term', 'idx_personal_documents_owner_status_updated',
        'idx_personal_document_versions_document_number', 'uk_personal_document_versions_number', 'uk_personal_document_previews_version',
        'uk_plugin_publishers_slug_active', 'uk_plugin_versions_version', 'uk_plugin_versions_active',
        'uk_plugin_capability_declaration', 'uk_plugin_admin_grants_current', 'uk_plugin_user_consents_current',
        'uk_plugin_delegations_token_digest', 'uk_plugin_secret_values_active', 'uk_plugin_authorization_request',
        'idx_plugin_admin_grants_declaration', 'idx_plugin_user_consents_declaration',
        'idx_plugin_authorization_declaration'
    ]) expected
    WHERE to_regclass('public.' || expected) IS NULL;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract missing indexes: %', missing;
    END IF;

    SELECT string_agg(format('%s.%s', conrelid::regclass, conname), ', ' ORDER BY conrelid::regclass::text, conname)
    INTO missing
    FROM pg_constraint fk
    WHERE fk.contype = 'f'
      AND fk.connamespace = 'public'::regnamespace
      AND NOT EXISTS (
          SELECT 1
          FROM pg_index idx
          WHERE idx.indrelid = fk.conrelid
            AND idx.indisvalid
            AND (idx.indkey::smallint[])[0:cardinality(fk.conkey)-1] @> fk.conkey
      );
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract foreign keys without a leading index: %', missing;
    END IF;
END $$;

SELECT jsonb_pretty(jsonb_build_object(
    'schema_contract', 'v1.0-clean-baseline-v1',
    'database', current_database(),
    'validated_at', now(),
    'status', 'pass'
)) AS schema_contract;
