DO $$
DECLARE
    missing TEXT;
    actor_guard RECORD;
BEGIN
    SELECT string_agg(item, ', ' ORDER BY item) INTO missing
    FROM unnest(ARRAY[
        'schema_migrations', 'schema_migration_locks', 'integration_principals', 'integration_credentials',
        'users', 'accounts', 'identity_admin_accounts', 'sessions', 'roles', 'user_roles',
        'identity_legacy_email_placeholders', 'identity_reserved_identifiers',
        'identity_email_challenges', 'identity_challenge_rate_limits', 'identity_challenge_policies', 'identity_account_recovery_cases',
        'permission_definitions', 'role_permissions', 'route_operations', 'route_permission_bindings', 'authorization_audits',
        'categories', 'threads', 'posts', 'category_thread_type_policies', 'mutual_aid_details', 'secondhand_details', 'plugins', 'plugin_permissions', 'plugin_logs',
        'plugin_packages','plugin_package_releases','plugin_installations','plugin_user_installations','plugin_v5_admin_grants','plugin_v5_user_consents','plugin_v5_configurations','plugin_operations',
        'plugin_publishers', 'plugin_versions', 'plugin_package_identities', 'plugin_capability_declarations', 'plugin_admin_grants',
        'plugin_user_consents', 'plugin_delegations', 'plugin_secret_values', 'plugin_configurations', 'plugin_authorization_decisions',
        'user_spaces', 'user_space_contents', 'richtext_article_contents',
        'richtext_article_assets', 'user_assets', 'richtext_article_attachments', 'plugin_ui_invocations', 'asset_lifecycle_audits',
        'content_revisions', 'content_moderation_cases', 'content_moderation_actions',
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
        'threads.publication_status', 'threads.moderation_status', 'threads.deletion_status', 'threads.current_revision', 'threads.facts_version',
        'admin_role_permissions.facts_version', 'integration_principals.facts_version', 'integration_principals.created_by_admin_id', 'integration_credentials.digest', 'integration_credentials.prefix', 'integration_credentials.purpose', 'integration_credentials.rules', 'integration_credentials.facts_version',
        'richtext_article_contents.facts_version', 'user_assets.facts_version', 'richtext_article_attachments.facts_version',
		'user_spaces.facts_version','user_space_style_snapshots.facts_version','user_storage_quotas.facts_version',
        'roles.facts_version', 'user_roles.facts_version', 'role_permissions.facts_version', 'permission_definitions.facts_version',
        'posts.facts_version','posts.deleted_actor_kind','posts.deleted_actor_id',
        'threads.moderation_actor_kind','threads.moderation_actor_id',
        'content_revisions.created_actor_kind','content_revisions.created_actor_id',
        'content_moderation_cases.opened_actor_kind','content_moderation_cases.opened_actor_id',
        'content_moderation_cases.resolved_actor_kind','content_moderation_cases.resolved_actor_id',
        'content_moderation_actions.actor_kind','content_moderation_actions.actor_ref_id',
        'richtext_article_contents.updated_actor_kind','richtext_article_contents.updated_actor_id',
        'asset_lifecycle_audits.actor_kind','asset_lifecycle_audits.actor_ref_id',
        'user_spaces.disabled_actor_kind','user_spaces.disabled_actor_id',
        'user_storage_quotas.updated_actor_kind','user_storage_quotas.updated_actor_id',
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
        'route_permission_bindings.route_operation_id', 'authorization_audits.actor_kind', 'authorization_audits.actor_id', 'authorization_audits.permission_code',
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
        'user_assets.id', 'user_assets.owner_user_id', 'user_assets.kind', 'user_assets.storage_object_id', 'user_assets.mime_type', 'user_assets.size_bytes', 'user_assets.status', 'user_assets.version',
        'richtext_article_attachments.id', 'richtext_article_attachments.article_content_id', 'richtext_article_attachments.asset_id', 'richtext_article_attachments.display_name', 'richtext_article_attachments.display_order',
        'plugin_ui_invocations.id', 'plugin_ui_invocations.user_id', 'plugin_ui_invocations.plugin_key', 'plugin_ui_invocations.surface_id', 'plugin_ui_invocations.context_kind', 'plugin_ui_invocations.article_content_id', 'plugin_ui_invocations.personal_document_id', 'plugin_ui_invocations.presentation', 'plugin_ui_invocations.expires_at',
        'asset_lifecycle_audits.id', 'asset_lifecycle_audits.asset_id', 'asset_lifecycle_audits.actor_user_id', 'asset_lifecycle_audits.actor_type', 'asset_lifecycle_audits.action', 'asset_lifecycle_audits.created_at',
        'schema_migrations.checksum', 'schema_migrations.execution_ms', 'schema_migrations.executor',
        'plugins.publisher_id', 'plugin_publishers.slug', 'plugin_publishers.trust_status',
        'plugin_versions.plugin_id', 'plugin_versions.package_digest', 'plugin_versions.permission_fingerprint', 'plugin_versions.activated_at',
        'plugin_package_identities.publisher', 'plugin_package_identities.plugin_key', 'plugin_package_identities.version', 'plugin_package_identities.package_digest', 'plugin_package_identities.created_at',
        'plugin_capability_declarations.plugin_version_id', 'plugin_capability_declarations.capability_code',
        'plugin_admin_grants.policy_revision', 'plugin_user_consents.purpose_hash',
        'plugin_delegations.token_digest', 'plugin_secret_values.ciphertext',
        'plugin_configurations.plugin_version_id', 'plugin_configurations.owner_user_id',
        'plugin_configurations.definition_version', 'plugin_configurations.revision',
        'plugin_configurations.values', 'plugin_configurations.secret_refs',
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
        'chk_threads_moderation_actor_domain',
        'chk_content_revisions_created_actor_domain',
        'chk_content_moderation_cases_opened_actor_domain',
        'chk_content_moderation_cases_resolved_actor_domain',
        'chk_content_moderation_actions_actor_ref_domain',
        'chk_richtext_article_contents_updated_actor_domain',
        'chk_asset_lifecycle_audits_actor_ref_domain',
        'chk_user_spaces_disabled_actor_domain',
        'chk_user_storage_quotas_updated_actor_domain',
		'chk_user_spaces_facts_version','chk_user_space_style_snapshots_facts_version',
        'chk_posts_facts_version',
        'chk_richtext_article_facts_version','chk_user_assets_facts_version','chk_richtext_article_attachments_facts_version','chk_posts_deleted_actor_domain',
        'chk_roles_facts_version','chk_user_roles_facts_version','chk_role_permissions_facts_version','chk_permission_definitions_facts_version',
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
        'chk_route_operation_code', 'chk_authorization_audits_outcome', 'chk_authorization_audits_actor',
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
        'chk_user_assets_kind', 'chk_user_assets_size', 'chk_user_assets_status', 'chk_user_assets_version', 'chk_user_assets_deleted_at',
        'chk_richtext_article_attachment_name', 'chk_richtext_article_attachment_order',
        'chk_plugin_ui_invocations_presentation', 'chk_plugin_ui_invocations_purpose', 'chk_plugin_ui_invocations_expiry', 'chk_plugin_ui_invocations_context_kind',
        'chk_asset_lifecycle_audits_actor_type', 'chk_asset_lifecycle_audits_action', 'chk_asset_lifecycle_audits_reason',
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
        'fk_user_assets_owner', 'fk_user_assets_storage_object', 'fk_richtext_article_attachments_article', 'fk_richtext_article_attachments_asset',
        'fk_plugin_ui_invocations_user', 'fk_plugin_ui_invocations_article', 'fk_plugin_ui_invocations_asset', 'fk_plugin_ui_invocations_attachment', 'fk_plugin_ui_invocations_personal_document',
        'fk_asset_lifecycle_audits_asset', 'fk_asset_lifecycle_audits_actor',
        'fk_richtext_article_assets_user_asset',
        'chk_plugin_capability_code', 'chk_plugin_admin_grants_status', 'chk_plugin_user_consents_status',
        'chk_plugin_delegations_status', 'chk_plugin_secret_values_payload', 'chk_plugin_authorization_outcome',
        'chk_plugin_configurations_definition_version', 'chk_plugin_configurations_revision',
        'chk_plugin_configurations_values', 'chk_plugin_configurations_secret_refs', 'chk_threads_facts_version', 'chk_admin_permission_facts_version'
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
        'idx_authorization_audits_actor', 'idx_plugins_runtime_state', 'uk_permission_definitions_code', 'uk_route_operations_code',
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
        'uq_plugin_configurations_scope', 'idx_plugin_configurations_owner',
        'idx_plugin_admin_grants_declaration', 'idx_plugin_user_consents_declaration',
        'idx_plugin_authorization_declaration'
        ,'uq_user_assets_storage_object', 'idx_user_assets_owner_status_updated', 'idx_user_assets_owner_kind_updated',
        'uq_richtext_article_attachment_asset', 'uq_richtext_article_attachment_order', 'idx_richtext_article_attachments_article_order', 'idx_richtext_article_attachments_asset_article',
        'idx_plugin_ui_invocations_expiry', 'idx_plugin_ui_invocations_user_plugin_expiry', 'idx_plugin_ui_invocations_article', 'idx_plugin_ui_invocations_asset', 'idx_plugin_ui_invocations_attachment', 'idx_plugin_ui_invocations_personal_document',
        'idx_asset_lifecycle_audits_created_action', 'idx_asset_lifecycle_audits_asset_created', 'idx_asset_lifecycle_audits_actor_created',
        'idx_richtext_article_assets_asset_id'
    ]) expected
    WHERE to_regclass('public.' || expected) IS NULL;
    IF missing IS NOT NULL THEN
        RAISE EXCEPTION 'schema contract missing indexes: %', missing;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'authorization_audits'
          AND column_name = 'actor_kind' AND is_nullable = 'NO'
          AND data_type = 'character varying' AND character_maximum_length = 32
    ) OR NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'authorization_audits'
          AND column_name = 'actor_id' AND is_nullable = 'YES'
          AND data_type = 'character varying' AND character_maximum_length = 128
    ) THEN
        RAISE EXCEPTION 'authorization audit actor columns have incorrect shape';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.plugin_versions'::regclass
          AND tgname = 'trg_plugin_version_identity_immutable'
          AND NOT tgisinternal AND tgenabled IN ('O', 'A')
          AND tgtype & 19 = 19
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.plugin_capability_declarations'::regclass
          AND tgname = 'trg_plugin_declaration_immutable'
          AND NOT tgisinternal AND tgenabled IN ('O', 'A')
          AND tgtype & 19 = 19
    ) THEN
        RAISE EXCEPTION 'plugin version identity update guards are missing or disabled';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.plugin_versions'::regclass
          AND tgname = 'trg_plugin_version_publication_guard'
          AND NOT tgisinternal AND tgenabled IN ('O', 'A')
          AND tgtype & 23 = 23
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.plugin_capability_declarations'::regclass
          AND tgname = 'trg_plugin_declaration_membership_guard'
          AND NOT tgisinternal AND tgenabled IN ('O', 'A')
          AND tgtype & 15 = 15
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.plugin_versions'::regclass
          AND tgname = 'trg_plugin_version_revoke_delegations'
          AND NOT tgisinternal AND tgenabled IN ('O', 'A')
          AND tgtype & 19 = 17
    ) THEN
        RAISE EXCEPTION 'plugin publication and delegation revocation guards are missing or disabled';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_attribute
        WHERE attrelid = 'public.threads'::regclass AND attname = 'facts_version'
          AND attnotnull AND atttypid = 'bigint'::regtype AND NOT attisdropped
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_sequence
        WHERE seqrelid = to_regclass('public.thread_facts_version_seq')
          AND NOT seqcycle AND seqincrement = 1 AND seqtypid = 'bigint'::regtype
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.threads'::regclass AND tgname = 'trg_threads_facts_version'
          AND NOT tgisinternal AND tgenabled IN ('O', 'A') AND tgtype = 23
          AND tgfoid = to_regprocedure('public.advance_thread_facts_version()')
    ) THEN
        RAISE EXCEPTION 'thread facts version column, allocation sequence or write guard is missing or disabled';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_attribute
        WHERE attrelid = 'public.admin_role_permissions'::regclass AND attname = 'facts_version'
          AND attnotnull AND atttypid = 'bigint'::regtype AND NOT attisdropped
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_sequence
        WHERE seqrelid = to_regclass('public.admin_permission_facts_version_seq')
          AND NOT seqcycle AND seqincrement = 1 AND seqtypid = 'bigint'::regtype
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.admin_role_permissions'::regclass AND tgname = 'trg_admin_permission_facts_version'
          AND NOT tgisinternal AND tgenabled IN ('O', 'A') AND tgtype = 23
          AND tgfoid = to_regprocedure('public.advance_admin_permission_facts_version()')
    ) THEN
        RAISE EXCEPTION 'admin permission facts column, allocation sequence or write guard is missing or disabled';
    END IF;

    IF NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='public.posts'::regclass AND attname='facts_version' AND attnotnull AND atttypid='bigint'::regtype AND NOT attisdropped)
        OR NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='public.posts'::regclass AND attname='deleted_actor_kind' AND attnotnull AND atttypid='varchar'::regtype AND atttypmod=20 AND NOT attisdropped)
        OR NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='public.posts'::regclass AND attname='deleted_actor_id' AND attnotnull AND atttypid='varchar'::regtype AND atttypmod=132 AND NOT attisdropped)
        OR NOT EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.post_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1)
        OR NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.posts'::regclass AND tgname='trg_posts_facts_version' AND NOT tgisinternal AND tgenabled IN('O','A') AND tgtype=23 AND tgfoid=to_regprocedure('public.advance_post_facts_version()')) THEN
        RAISE EXCEPTION 'post authority facts or deletion actor guards missing or disabled';
    END IF;

    IF NOT EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.integration_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1) OR
       (SELECT count(*) FROM (VALUES ('public.integration_principals','trg_integration_principal_facts'),('public.integration_credentials','trg_integration_credential_facts')) x(tbl,trg) JOIN pg_trigger t ON t.tgrelid=to_regclass(x.tbl) AND t.tgname=x.trg WHERE NOT t.tgisinternal AND t.tgenabled IN('O','A') AND t.tgtype=23 AND t.tgfoid=to_regprocedure('public.advance_integration_facts()'))<>2 THEN RAISE EXCEPTION 'missing managed Integration facts guard'; END IF;
    IF NOT EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.user_scope_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1) OR
       (SELECT count(*) FROM (VALUES ('public.roles','trg_roles_user_scope_facts_version'),('public.user_roles','trg_user_roles_user_scope_facts_version'),('public.role_permissions','trg_role_permissions_user_scope_facts_version'),('public.permission_definitions','trg_permission_definitions_user_scope_facts_version')) x(tbl,trg) JOIN pg_trigger t ON t.tgrelid=to_regclass(x.tbl) AND t.tgname=x.trg WHERE NOT t.tgisinternal AND t.tgenabled IN('O','A') AND t.tgtype=23 AND t.tgfoid=to_regprocedure('public.advance_user_scope_facts_version()'))<>4 THEN RAISE EXCEPTION 'missing managed User scoped execution facts guard'; END IF;
    IF NOT EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.richtext_asset_binding_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1) OR
       (SELECT count(*) FROM (VALUES ('public.user_assets','trg_user_assets_richtext_facts_version'),('public.richtext_article_attachments','trg_article_attachments_richtext_facts_version')) x(tbl,trg) JOIN pg_trigger t ON t.tgrelid=to_regclass(x.tbl) AND t.tgname=x.trg WHERE NOT t.tgisinternal AND t.tgenabled IN('O','A') AND t.tgtype=23 AND t.tgfoid=to_regprocedure('public.advance_richtext_asset_binding_facts_version()'))<>2 THEN RAISE EXCEPTION 'missing managed RichText asset/binding facts guard'; END IF;
    IF NOT EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.personal_space_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1) OR
       (SELECT count(*) FROM (VALUES ('public.user_spaces','trg_user_spaces_facts_version'),('public.user_space_style_snapshots','trg_space_style_snapshots_facts_version')) x(tbl,trg) JOIN pg_trigger t ON t.tgrelid=to_regclass(x.tbl) AND t.tgname=x.trg WHERE NOT t.tgisinternal AND t.tgenabled IN('O','A') AND t.tgtype=23 AND t.tgfoid=to_regprocedure('public.advance_personal_space_facts_version()'))<>2 THEN RAISE EXCEPTION 'missing managed Personal Space facts guard'; END IF;
    IF NOT EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.storage_quota_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1) OR NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.user_storage_quotas'::regclass AND tgname='trg_user_storage_quotas_facts_version' AND NOT tgisinternal AND tgenabled IN('O','A') AND tgtype=23 AND tgfoid=to_regprocedure('public.advance_storage_quota_facts_version()')) THEN RAISE EXCEPTION 'missing managed storage quota facts guard'; END IF;
    IF NOT EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.article_facts_version_seq') AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1) OR NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.richtext_article_contents'::regclass AND tgname='trg_article_facts_version' AND NOT tgisinternal AND tgenabled IN('O','A') AND tgtype=23 AND tgfoid=to_regprocedure('public.advance_article_facts_version()')) THEN RAISE EXCEPTION 'missing managed Article facts guard'; END IF;

    FOR actor_guard IN SELECT * FROM (VALUES
        ('threads','moderation_actor_kind','moderation_actor_id','trg_01_threads_moderation_actor_actor'),
        ('content_revisions','created_actor_kind','created_actor_id','trg_01_content_revisions_created_actor_actor'),
        ('content_moderation_cases','opened_actor_kind','opened_actor_id','trg_01_content_moderation_cases_opened_actor_actor'),
        ('content_moderation_cases','resolved_actor_kind','resolved_actor_id','trg_01_content_moderation_cases_resolved_actor_actor'),
        ('content_moderation_actions','actor_kind','actor_ref_id','trg_01_content_moderation_actions_actor_ref_actor'),
        ('richtext_article_contents','updated_actor_kind','updated_actor_id','trg_01_richtext_article_contents_updated_actor_actor'),
        ('asset_lifecycle_audits','actor_kind','actor_ref_id','trg_01_asset_lifecycle_audits_actor_ref_actor'),
        ('user_spaces','disabled_actor_kind','disabled_actor_id','trg_01_user_spaces_disabled_actor_actor'),
        ('user_storage_quotas','updated_actor_kind','updated_actor_id','trg_01_user_storage_quotas_updated_actor_actor')
    ) AS guards(table_name,kind_column,id_column,trigger_name) LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('public.'||actor_guard.table_name)
            AND attname=actor_guard.kind_column AND attnotnull AND atttypid='varchar'::regtype AND atttypmod=20 AND NOT attisdropped)
            OR NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('public.'||actor_guard.table_name)
            AND attname=actor_guard.id_column AND attnotnull AND atttypid='varchar'::regtype AND atttypmod=132 AND NOT attisdropped)
            OR NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('public.'||actor_guard.table_name)
            AND tgname=actor_guard.trigger_name AND NOT tgisinternal AND tgenabled IN ('O','A') AND tgtype=23
            AND tgfoid=to_regprocedure('public.sync_business_actor_pair()')) THEN
            RAISE EXCEPTION 'business actor domain columns or legacy provenance guard missing/disabled: %',actor_guard.table_name;
        END IF;
    END LOOP;

    IF (SELECT count(*) FROM (VALUES ('plugin_package_identity_write_once',27),('plugin_package_identity_no_truncate',34)) x(name,kind)
        JOIN pg_trigger t ON t.tgrelid='public.plugin_package_identities'::regclass AND t.tgname=x.name
        WHERE NOT t.tgisinternal AND t.tgenabled IN('O','A') AND t.tgtype=x.kind
        AND t.tgfoid=to_regprocedure('public.reject_plugin_package_identity_mutation()')) <> 2 THEN
        RAISE EXCEPTION 'missing or disabled plugin package identity immutability guards';
    END IF;

    FOREACH missing IN ARRAY ARRAY['plugin_package_releases','plugin_installations','plugin_user_installations','plugin_v5_admin_grants','plugin_v5_user_consents','plugin_v5_configurations','plugin_operations'] LOOP
      IF (SELECT count(*) FROM pg_trigger WHERE tgrelid=to_regclass('public.'||missing) AND NOT tgisinternal AND tgenabled IN('O','A') AND tgfoid=to_regprocedure('public.guard_plugin_v5_state()'))<>2 THEN RAISE EXCEPTION 'missing v5 state history guard: %',missing; END IF;
      IF NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('public.'||missing) AND attname='state' AND attnotnull AND atttypid='jsonb'::regtype) THEN RAISE EXCEPTION 'missing v5 state fact: %',missing; END IF;
    END LOOP;
    IF NOT EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.plugin_v5_generations') AND NOT seqcycle AND seqtypid='bigint'::regtype) THEN RAISE EXCEPTION 'missing v5 monotonic generation sequence'; END IF;

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
    'schema_contract', 'v1.2-plugin-v5-config-v1',
    'database', current_database(),
    'validated_at', now(),
    'status', 'pass'
)) AS schema_contract;
