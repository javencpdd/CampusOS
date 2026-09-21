-- v1.1 trusted plugin-market source allowlist and immutable request snapshots.
-- A source's Ed25519 public key is a verification certificate, not a Secret.
-- The platform never accepts a user-provided market URL as an install request.

CREATE TABLE plugin_market_sources (
    id varchar(128) PRIMARY KEY,
    display_name varchar(120) NOT NULL,
    catalog_url text NOT NULL,
    public_key text NOT NULL,
    status varchar(16) NOT NULL DEFAULT 'enabled',
    created_by varchar(64) NOT NULL DEFAULT '',
    updated_by varchar(64) NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_plugin_market_source_status CHECK (status IN ('enabled', 'disabled'))
);

ALTER TABLE plugin_install_requests
    ADD COLUMN market_source_id varchar(128),
    ADD COLUMN market_plugin_id varchar(128),
    ADD COLUMN market_listing_url text NOT NULL DEFAULT '',
    ADD COLUMN market_package_url text NOT NULL DEFAULT '',
    ADD COLUMN market_version varchar(64) NOT NULL DEFAULT '',
    ADD COLUMN market_publisher varchar(255) NOT NULL DEFAULT '';

ALTER TABLE plugin_install_requests
    ADD CONSTRAINT fk_plugin_install_requests_market_source
    FOREIGN KEY (market_source_id) REFERENCES plugin_market_sources(id)
    ON UPDATE CASCADE ON DELETE RESTRICT;

-- The old local-only key cannot distinguish equal plugin IDs from two trusted
-- sources. Existing legacy rows retain NULL source/plugin fields; new requests
-- always carry both identifiers after the application boundary validates them.
DROP INDEX uk_plugin_install_request_pending;

CREATE UNIQUE INDEX uk_plugin_install_request_market_pending
    ON plugin_install_requests (market_source_id, market_plugin_id, user_id)
    WHERE status = 'pending' AND market_source_id IS NOT NULL AND market_plugin_id IS NOT NULL;

CREATE INDEX idx_plugin_market_sources_status
    ON plugin_market_sources (status, display_name, id);

CREATE INDEX idx_plugin_install_requests_market_source
    ON plugin_install_requests (market_source_id, status, created_at DESC)
    WHERE market_source_id IS NOT NULL;
