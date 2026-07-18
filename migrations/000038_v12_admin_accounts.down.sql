DROP TRIGGER IF EXISTS trg_sync_identity_admin_account_from_role ON user_roles;
DROP FUNCTION IF EXISTS sync_identity_admin_account_from_role();
DROP FUNCTION IF EXISTS sync_identity_admin_account_for_user(BIGINT, BIGINT);
DROP TABLE IF EXISTS identity_admin_accounts;
