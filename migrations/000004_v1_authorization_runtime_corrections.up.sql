-- Forward-only v1 authorization corrections discovered during runtime integration.
-- Keep the frozen 000001-000003 checksums intact for already-created dev databases.

DROP INDEX IF EXISTS uk_plugin_secret_values_active;
CREATE UNIQUE INDEX uk_plugin_secret_values_active
    ON plugin_secret_values(plugin_id, owner_user_id, secret_name) NULLS NOT DISTINCT
    WHERE status = 'active';

-- Denied attempts for a catalog-known but undeclared capability are security
-- evidence. Retain the plugin-version FK, but allow that denial to be recorded.
ALTER TABLE plugin_authorization_decisions
    DROP CONSTRAINT IF EXISTS fk_plugin_authorization_declaration;
