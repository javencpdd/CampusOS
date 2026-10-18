-- Losing a known identity would allow reusing its version for different bytes.
LOCK TABLE plugin_package_identities IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM plugin_package_identities) THEN
        RAISE EXCEPTION 'cannot roll back nonempty plugin package identities';
    END IF;
END $$;
DROP TABLE plugin_package_identities;
DROP FUNCTION reject_plugin_package_identity_mutation();
