-- v0.14 F1: metadata, persistent quota ledger and explicit reservations for
-- private Local Provider objects. Legacy user files remain untouched; their
-- adoption is an explicit later operation and never an implicit deletion.

CREATE TABLE IF NOT EXISTS user_storage_accounts (
    user_id        BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    used_bytes     BIGINT NOT NULL DEFAULT 0 CONSTRAINT chk_user_storage_accounts_used CHECK (used_bytes >= 0),
    reserved_bytes BIGINT NOT NULL DEFAULT 0 CONSTRAINT chk_user_storage_accounts_reserved CHECK (reserved_bytes >= 0),
    version        BIGINT NOT NULL DEFAULT 1 CONSTRAINT chk_user_storage_accounts_version CHECK (version >= 1),
    created_at     TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS storage_objects (
    id            BIGINT PRIMARY KEY,
    owner_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    namespace     VARCHAR(80) NOT NULL,
    purpose       VARCHAR(120) NOT NULL,
    provider      VARCHAR(32) NOT NULL DEFAULT 'local' CONSTRAINT chk_storage_objects_provider CHECK (provider = 'local'),
    storage_key   VARCHAR(255) NOT NULL,
    original_name VARCHAR(255) NOT NULL,
    mime_type     VARCHAR(160) NOT NULL,
    size_bytes    BIGINT NOT NULL DEFAULT 0 CONSTRAINT chk_storage_objects_size CHECK (size_bytes >= 0),
    sha256        VARCHAR(64) NOT NULL DEFAULT '',
    status        VARCHAR(20) NOT NULL CONSTRAINT chk_storage_objects_status CHECK (status IN ('pending', 'ready', 'deleting', 'deleted', 'quarantined', 'missing')),
    version       BIGINT NOT NULL DEFAULT 1 CONSTRAINT chk_storage_objects_version CHECK (version >= 1),
    created_at    TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMP NULL,
    CONSTRAINT chk_storage_objects_deleted_at CHECK ((status = 'deleted' AND deleted_at IS NOT NULL) OR (status <> 'deleted')),
    CONSTRAINT chk_storage_objects_ready_payload CHECK ((status <> 'ready') OR (storage_key <> '' AND size_bytes >= 0 AND sha256 <> '')),
    CONSTRAINT uk_storage_objects_provider_key UNIQUE (provider, storage_key)
);

CREATE INDEX IF NOT EXISTS idx_storage_objects_owner_id_desc
    ON storage_objects(owner_user_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_storage_objects_owner_namespace_purpose_id_desc
    ON storage_objects(owner_user_id, namespace, purpose, id DESC);
CREATE INDEX IF NOT EXISTS idx_storage_objects_status_updated_at
    ON storage_objects(status, updated_at ASC);

CREATE TABLE IF NOT EXISTS user_storage_reservations (
    id             BIGINT PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    object_id      BIGINT NOT NULL REFERENCES storage_objects(id) ON DELETE CASCADE,
    reserved_bytes BIGINT NOT NULL CONSTRAINT chk_user_storage_reservations_bytes CHECK (reserved_bytes >= 0),
    status         VARCHAR(20) NOT NULL CONSTRAINT chk_user_storage_reservations_status CHECK (status IN ('pending', 'committed', 'released', 'expired')),
    expires_at     TIMESTAMP NOT NULL,
    created_at     TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT uk_user_storage_reservations_object UNIQUE (object_id)
);

CREATE INDEX IF NOT EXISTS idx_user_storage_reservations_pending_expiry
    ON user_storage_reservations(status, expires_at ASC)
    WHERE status = 'pending';
