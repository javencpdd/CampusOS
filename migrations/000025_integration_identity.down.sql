-- Refuse to silently erase issued identities or revocation history. Remove
-- application data only under a separately authorized recovery procedure.
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM integration_principals) OR EXISTS(SELECT 1 FROM integration_credentials) THEN
        RAISE EXCEPTION 'integration identity data exists; rollback requires explicit archival';
    END IF;
    IF EXISTS(SELECT 1 FROM admin_role_permissions WHERE permission_code IN('identity.integration.read','identity.integration.manage') AND NOT(role_id='security_admin' AND created_by='system:migration-25'))
       OR EXISTS(SELECT 1 FROM role_permissions WHERE permission_id IN(900000000000002501,900000000000002502))
       OR EXISTS(SELECT 1 FROM route_permission_bindings WHERE permission_id IN(900000000000002501,900000000000002502)) THEN
        RAISE EXCEPTION 'integration management references exist; rollback requires explicit archival';
    END IF;
END $$;
DELETE FROM admin_role_permissions WHERE role_id='security_admin' AND permission_code IN('identity.integration.read','identity.integration.manage') AND created_by='system:migration-25';
DELETE FROM permission_definitions WHERE id IN(900000000000002501,900000000000002502) AND code IN('identity.integration.read','identity.integration.manage');
DROP TABLE integration_credentials;
DROP TABLE integration_principals;
DROP FUNCTION advance_integration_facts();
-- Intentionally preserve the non-reusing facts sequence across down/up.
