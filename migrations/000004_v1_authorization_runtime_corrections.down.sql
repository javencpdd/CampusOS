-- Rollback restores the original v1 foundation. Undeclared-capability denial
-- rows cannot satisfy the former FK and are deliberately removed first.
DELETE FROM plugin_authorization_decisions d
WHERE NOT EXISTS (
    SELECT 1
    FROM plugin_capability_declarations c
    WHERE c.plugin_version_id = d.plugin_version_id
      AND c.capability_code = d.capability_code
);

ALTER TABLE plugin_authorization_decisions
    ADD CONSTRAINT fk_plugin_authorization_declaration
    FOREIGN KEY (plugin_version_id, capability_code)
    REFERENCES plugin_capability_declarations(plugin_version_id, capability_code)
    ON DELETE RESTRICT;

DROP INDEX IF EXISTS uk_plugin_secret_values_active;
CREATE UNIQUE INDEX uk_plugin_secret_values_active
    ON plugin_secret_values(plugin_id, owner_user_id, secret_name) NULLS NOT DISTINCT
    WHERE revoked_at IS NULL;
