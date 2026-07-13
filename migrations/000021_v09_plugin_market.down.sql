-- This down migration is intended only for disposable development databases.
-- Production rollback retains v0.9 user data and reverts application code.
DROP TABLE IF EXISTS plugin_market_audits;
DROP TABLE IF EXISTS plugin_releases;
DROP TABLE IF EXISTS plugin_install_requests;
DROP TABLE IF EXISTS plugin_catalog_entries;
DROP TABLE IF EXISTS plugin_user_grants;
DROP TABLE IF EXISTS plugin_file_metadata;
DROP TABLE IF EXISTS plugin_records;
