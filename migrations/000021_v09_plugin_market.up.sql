-- v0.9 host-managed plugin records, user consent, file metadata, local catalog,
-- search documents and release governance. Plugin code never receives direct
-- access to these tables.

CREATE TABLE IF NOT EXISTS plugin_records (
    id          BIGINT PRIMARY KEY,
    plugin_name VARCHAR(128) NOT NULL,
    owner_type  VARCHAR(16) NOT NULL,
    owner_id    VARCHAR(64) NOT NULL,
    collection  VARCHAR(64) NOT NULL,
    record_key  VARCHAR(128) NOT NULL,
    data        JSONB NOT NULL DEFAULT '{}'::jsonb,
    search_text TEXT NOT NULL DEFAULT '',
    version     BIGINT NOT NULL DEFAULT 1,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMP NULL,
    CONSTRAINT chk_plugin_records_owner_type CHECK (owner_type IN ('system', 'user')),
    CONSTRAINT chk_plugin_records_version CHECK (version > 0)
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_plugin_records_active_key
    ON plugin_records(plugin_name, owner_type, owner_id, collection, record_key)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_plugin_records_owner_collection_updated
    ON plugin_records(plugin_name, owner_type, owner_id, collection, updated_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_plugin_records_search
    ON plugin_records(plugin_name, owner_type, owner_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS plugin_file_metadata (
    id            BIGINT PRIMARY KEY,
    plugin_name   VARCHAR(128) NOT NULL,
    owner_id      VARCHAR(64) NOT NULL,
    original_name TEXT NOT NULL,
    stored_name   VARCHAR(255) NOT NULL,
    content_type  VARCHAR(255) NOT NULL,
    size_bytes    BIGINT NOT NULL,
    storage_key   TEXT NOT NULL,
    retention     VARCHAR(32) NOT NULL DEFAULT 'user-deletable',
    created_at    TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMP NULL,
    CONSTRAINT chk_plugin_file_size CHECK (size_bytes >= 0),
    CONSTRAINT chk_plugin_file_retention CHECK (retention IN ('retained', 'user-deletable'))
);
CREATE INDEX IF NOT EXISTS idx_plugin_file_metadata_owner
    ON plugin_file_metadata(plugin_name, owner_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS plugin_user_grants (
    id          BIGINT PRIMARY KEY,
    plugin_name VARCHAR(128) NOT NULL,
    user_id     VARCHAR(64) NOT NULL,
    version     VARCHAR(64) NOT NULL,
    permissions JSONB NOT NULL DEFAULT '[]'::jsonb,
    status      VARCHAR(16) NOT NULL,
    granted_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMP NULL,
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_plugin_user_grants_status CHECK (status IN ('enabled', 'revoked'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_plugin_user_grants_identity
    ON plugin_user_grants(plugin_name, user_id);
CREATE INDEX IF NOT EXISTS idx_plugin_user_grants_user
    ON plugin_user_grants(user_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS plugin_catalog_entries (
    plugin_name       VARCHAR(128) PRIMARY KEY,
    display_name      VARCHAR(255) NOT NULL DEFAULT '',
    description       TEXT NOT NULL DEFAULT '',
    version           VARCHAR(64) NOT NULL,
    runtime           VARCHAR(32) NOT NULL,
    visibility        VARCHAR(16) NOT NULL DEFAULT 'draft',
    package_checksum  VARCHAR(128) NOT NULL DEFAULT '',
    risk_level        VARCHAR(16) NOT NULL DEFAULT '',
    data_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_plugin_catalog_visibility CHECK (visibility IN ('draft', 'published', 'hidden'))
);
CREATE INDEX IF NOT EXISTS idx_plugin_catalog_visibility
    ON plugin_catalog_entries(visibility, plugin_name);

CREATE TABLE IF NOT EXISTS plugin_install_requests (
    id          BIGINT PRIMARY KEY,
    plugin_name VARCHAR(128) NOT NULL,
    user_id     VARCHAR(64) NOT NULL,
    message     TEXT NOT NULL DEFAULT '',
    status      VARCHAR(16) NOT NULL DEFAULT 'pending',
    reviewed_by VARCHAR(64) NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    reviewed_at TIMESTAMP NULL,
    CONSTRAINT chk_plugin_install_request_status CHECK (status IN ('pending', 'approved', 'rejected'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_plugin_install_request_pending
    ON plugin_install_requests(plugin_name, user_id)
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_plugin_install_requests_status
    ON plugin_install_requests(status, created_at DESC);

CREATE TABLE IF NOT EXISTS plugin_releases (
    id              BIGINT PRIMARY KEY,
    plugin_name     VARCHAR(128) NOT NULL,
    version         VARCHAR(64) NOT NULL,
    checksum        VARCHAR(128) NOT NULL,
    signature_state VARCHAR(32) NOT NULL,
    channel         VARCHAR(32) NOT NULL DEFAULT 'stable',
    rollout_state   VARCHAR(32) NOT NULL DEFAULT 'pending',
    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_plugin_releases_version
    ON plugin_releases(plugin_name, version, checksum);

CREATE TABLE IF NOT EXISTS plugin_market_audits (
    id          BIGINT PRIMARY KEY,
    plugin_name VARCHAR(128) NOT NULL,
    actor_id    VARCHAR(64) NOT NULL DEFAULT '',
    action      VARCHAR(128) NOT NULL,
    outcome     VARCHAR(32) NOT NULL,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_plugin_market_audits_plugin_created
    ON plugin_market_audits(plugin_name, created_at DESC);
