# CampusOS Database Relationship Report

- Tables: **77**
- Foreign keys: **43**
- Inferred 1:1 relations: **3**
- Physical 1:N relations: **40**
- Inferred logical M:N relations: **2**

> Cardinalities are inferred from actual FK + PK/UNIQUE constraints. Nullable FKs are shown as optional on the child side.

## 1:1 / 1:0..1

| Parent | Child | FK | Columns | Cardinality | ON DELETE |
|---|---|---|---|---|---|
| `threads` | `mutual_aid_details` | `fk_mutual_aid_details_thread` | `thread_id` → `id` | 1 : 0..1 | RESTRICT |
| `threads` | `richtext_article_contents` | `fk_richtext_contents_thread` | `thread_id` → `id` | 1 : 1 | RESTRICT |
| `threads` | `secondhand_details` | `fk_secondhand_details_thread` | `thread_id` → `id` | 1 : 0..1 | RESTRICT |

## 1:N / 1:0..N

| Parent | Child | FK | Columns | Cardinality | ON DELETE |
|---|---|---|---|---|---|
| `users` | `accounts` | `fk_accounts_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `categories` | `categories` | `fk_categories_parent` | `parent_id` → `id` | 1 : 0..N | RESTRICT |
| `categories` | `category_thread_type_policies` | `fk_category_thread_type_policy_category` | `category_id` → `id` | 1 : N | RESTRICT |
| `users` | `configurations` | `fk_configurations_updated_by` | `updated_by` → `id` | 1 : N | RESTRICT |
| `users` | `identity_account_recovery_cases` | `fk_identity_recovery_case_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `accounts` | `identity_account_recovery_cases` | `fk_identity_recovery_case_account` | `account_id` → `id` | 1 : N | RESTRICT |
| `identity_email_challenges` | `identity_account_recovery_cases` | `fk_identity_recovery_case_challenge` | `challenge_id` → `id` | 1 : N | RESTRICT |
| `users` | `identity_account_recovery_cases` | `fk_identity_recovery_case_created_by` | `created_by` → `id` | 1 : 0..N | RESTRICT |
| `users` | `identity_admin_accounts` | `fk_identity_admin_account_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `accounts` | `identity_admin_accounts` | `fk_identity_admin_account_credential` | `credential_account_id` → `id` | 1 : N | RESTRICT |
| `users` | `identity_challenge_policies` | `fk_identity_challenge_policy_updated_by` | `updated_by` → `id` | 1 : 0..N | SET NULL |
| `accounts` | `identity_email_challenges` | `fk_identity_email_challenge_account` | `account_id` → `id` | 1 : 0..N | RESTRICT |
| `users` | `identity_legacy_email_placeholders` | `fk_identity_legacy_placeholder_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `users` | `identity_mfa_policies` | `fk_identity_mfa_policy_updated_by` | `updated_by` → `id` | 1 : 0..N | SET NULL |
| `users` | `identity_mfa_recovery_codes` | `fk_identity_mfa_recovery_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `identity_mfa_totp_methods` | `identity_mfa_recovery_codes` | `fk_identity_mfa_recovery_method` | `method_id` → `id` | 1 : N | RESTRICT |
| `users` | `identity_mfa_tickets` | `fk_identity_mfa_ticket_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `users` | `identity_mfa_totp_methods` | `fk_identity_mfa_totp_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `users` | `likes` | `fk_likes_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `users` | `mutual_aid_details` | `fk_mutual_aid_details_created_by` | `created_by` → `id` | 1 : N | RESTRICT |
| `users` | `notifications` | `fk_notifications_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `platform_outbox` | `outbox_consumer_receipts` | `fk_outbox_consumer_receipt_event` | `event_id` → `id` | 1 : N | CASCADE |
| `roles` | `permissions` | `fk_permissions_role` | `role_id` → `id` | 1 : N | RESTRICT |
| `personal_document_versions` | `personal_documents` | `fk_personal_documents_current_version` | `current_version_id` → `id` | 1 : 0..N | RESTRICT |
| `platform_outbox` | `platform_command_audits` | `fk_platform_command_audit_event` | `event_id` → `id` | 1 : 0..N | SET NULL |
| `platform_outbox` | `platform_outbox_attempts` | `fk_platform_outbox_attempt_event` | `event_id` → `id` | 1 : N | CASCADE |
| `threads` | `posts` | `fk_posts_thread` | `thread_id` → `id` | 1 : N | RESTRICT |
| `threads` | `richtext_article_assets` | `fk_richtext_assets_thread` | `thread_id` → `id` | 1 : 0..N | RESTRICT |
| `roles` | `role_permissions` | `fk_v10_role_permissions_role` | `role_id` → `id` | 1 : N | RESTRICT |
| `permission_definitions` | `role_permissions` | `fk_v10_role_permissions_permission` | `permission_id` → `id` | 1 : N | RESTRICT |
| `route_operations` | `route_permission_bindings` | `fk_route_permission_operation` | `route_operation_id` → `id` | 1 : N | CASCADE |
| `permission_definitions` | `route_permission_bindings` | `fk_route_permission_definition` | `permission_id` → `id` | 1 : N | RESTRICT |
| `users` | `secondhand_details` | `fk_secondhand_details_created_by` | `created_by` → `id` | 1 : N | RESTRICT |
| `users` | `sessions` | `fk_sessions_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `users` | `threads` | `fk_threads_author` | `author_id` → `id` | 1 : N | RESTRICT |
| `users` | `user_roles` | `fk_user_roles_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `users` | `user_space_contents` | `fk_user_space_contents_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `users` | `user_spaces` | `fk_user_spaces_user` | `user_id` → `id` | 1 : N | RESTRICT |
| `webhook_endpoints` | `webhook_deliveries` | `fk_webhook_deliveries_endpoint` | `endpoint_id` → `id` | 1 : N | RESTRICT |
| `platform_outbox` | `webhook_deliveries` | `fk_webhook_deliveries_outbox` | `outbox_event_id` → `id` | 1 : 0..N | SET NULL |

## Logical M:N (through association entities)

| Entity A | Entity B | Association table | Basis |
|---|---|---|---|
| `roles` | `permission_definitions` | `role_permissions` | association-entity inferred |
| `route_operations` | `permission_definitions` | `route_permission_bindings` | association-entity inferred |

## Business domains

### Identity & Access
`accounts`, `api_keys`, `authorization_audits`, `identity_account_recovery_cases`, `identity_admin_accounts`, `identity_challenge_policies`, `identity_challenge_rate_limits`, `identity_email_challenges`, `identity_legacy_email_placeholders`, `identity_mfa_policies`, `identity_mfa_recovery_codes`, `identity_mfa_tickets`, `identity_mfa_totp_methods`, `identity_reserved_identifiers`, `permission_definitions`, `role_permissions`, `roles`, `sessions`, `user_roles`, `users`

### Community & Content
`categories`, `category_thread_type_policies`, `likes`, `mutual_aid_details`, `notifications`, `posts`, `richtext_article_assets`, `richtext_article_contents`, `secondhand_details`, `threads`

### Academic & Schedule
`academic_terms`, `user_schedule_preferences`, `user_schedule_terms`

### User Space & Documents
`personal_document_previews`, `personal_document_versions`, `personal_documents`, `user_space_contents`, `user_space_style_snapshots`, `user_spaces`

### Storage
`storage_objects`, `user_storage_accounts`, `user_storage_quotas`, `user_storage_reservations`

### Plugin Ecosystem & Authorization
`plugin_catalog_entries`, `plugin_file_metadata`, `plugin_install_requests`, `plugin_logs`, `plugin_market_audits`, `plugin_permissions`, `plugin_records`, `plugin_releases`, `plugin_user_grants`, `plugins`

### Platform Reliability & Integration
`ai_call_logs`, `audit_logs`, `builtin_feature_states`, `configurations`, `mcp_audit_logs`, `message_bindings`, `message_logs`, `outbox_consumer_receipts`, `webhook_deliveries`, `webhook_endpoints`

### Other
`content_moderation_actions`, `content_moderation_cases`, `content_revisions`, `permissions`, `platform_command_audits`, `platform_compatibility_usage`, `platform_operation_runs`, `platform_outbox`, `platform_outbox_attempts`, `platform_retention_runs`, `platform_worker_leases`, `route_operations`, `route_permission_bindings`, `tags`

## Reading the diagram

- **PK**: primary-key field; **FK**: foreign-key field; **UQ**: globally unique field; **NN**: NOT NULL.
- Solid gray edge: physical database FK.
- Purple dashed edge: inferred logical many-to-many relation, while the physical FK edges to the association entity remain visible.
- `DEL CASCADE / RESTRICT / SET NULL ...` on edges is taken from migration DDL.
- Cross-domain relations remain visible in the full diagram. Per-domain SVGs add compact external-reference stubs when necessary.
