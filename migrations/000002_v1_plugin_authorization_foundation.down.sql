ALTER TABLE plugins DROP CONSTRAINT IF EXISTS fk_plugins_publisher;
ALTER TABLE plugins DROP COLUMN IF EXISTS publisher_id;
DROP TABLE IF EXISTS plugin_authorization_decisions;
DROP TABLE IF EXISTS plugin_secret_values;
DROP TABLE IF EXISTS plugin_delegations;
DROP TABLE IF EXISTS plugin_user_consents;
DROP TABLE IF EXISTS plugin_admin_grants;
DROP TABLE IF EXISTS plugin_capability_declarations;
DROP TABLE IF EXISTS plugin_versions;
DROP TABLE IF EXISTS plugin_publishers;

