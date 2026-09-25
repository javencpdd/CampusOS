-- This rollback preserves request provenance: it refuses to remove the source
-- and snapshot columns while any marketplace-backed request still exists.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM plugin_install_requests WHERE market_source_id IS NOT NULL)
       OR EXISTS (SELECT 1 FROM plugin_market_sources) THEN
        RAISE EXCEPTION 'cannot roll back 000003 while trusted marketplace sources or requests exist';
    END IF;
END $$;

DROP INDEX IF EXISTS idx_plugin_install_requests_market_source;
DROP INDEX IF EXISTS idx_plugin_market_sources_status;
DROP INDEX IF EXISTS uk_plugin_install_request_market_pending;

ALTER TABLE plugin_install_requests
    DROP CONSTRAINT IF EXISTS fk_plugin_install_requests_market_source,
    DROP COLUMN market_publisher,
    DROP COLUMN market_version,
    DROP COLUMN market_package_url,
    DROP COLUMN market_listing_url,
    DROP COLUMN market_plugin_id,
    DROP COLUMN market_source_id;

DROP TABLE plugin_market_sources;

CREATE UNIQUE INDEX uk_plugin_install_request_pending
    ON plugin_install_requests (plugin_name, user_id)
    WHERE status = 'pending';
