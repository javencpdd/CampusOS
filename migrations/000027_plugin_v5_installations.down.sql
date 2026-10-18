-- Preserve all v5 history and prior schema data. The generation sequence
-- survives even an empty-domain down/up so failed/issued epochs cannot repeat.
LOCK TABLE plugin_packages, plugin_package_releases, plugin_installations, plugin_user_installations, plugin_v5_admin_grants, plugin_v5_user_consents, plugin_v5_configurations, plugin_operations IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM plugin_packages) OR EXISTS(SELECT 1 FROM plugin_package_releases) OR EXISTS(SELECT 1 FROM plugin_installations) OR EXISTS(SELECT 1 FROM plugin_user_installations) OR EXISTS(SELECT 1 FROM plugin_v5_admin_grants) OR EXISTS(SELECT 1 FROM plugin_v5_user_consents) OR EXISTS(SELECT 1 FROM plugin_v5_configurations) OR EXISTS(SELECT 1 FROM plugin_operations) THEN
  RAISE EXCEPTION 'cannot roll back nonempty v5 installation domain';
 END IF;
END $$;
DROP TABLE plugin_operations;
DROP TABLE plugin_v5_configurations;
DROP TABLE plugin_v5_user_consents;
DROP TABLE plugin_v5_admin_grants;
DROP TABLE plugin_user_installations;
DROP TABLE plugin_installations;
DROP TABLE plugin_package_releases;
DROP TABLE plugin_packages;
DROP FUNCTION guard_plugin_v5_state();
ALTER SEQUENCE plugin_v5_generations OWNED BY NONE;
