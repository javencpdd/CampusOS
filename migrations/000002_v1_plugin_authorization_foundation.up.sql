-- CampusOS v1.0 plugin identity, version and three-layer authorization foundation.
-- Existing plugin market/runtime tables remain current operational models; these
-- normalized tables provide the target write model for the v1 authorization work.

CREATE TABLE plugin_publishers (
    id BIGINT PRIMARY KEY,
    slug VARCHAR(128) NOT NULL,
    display_name VARCHAR(255) NOT NULL,
    trust_status VARCHAR(16) NOT NULL DEFAULT 'pending',
    signing_key_id VARCHAR(255) NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT chk_plugin_publishers_slug CHECK (slug ~ '^[a-z0-9][a-z0-9._-]{1,126}[a-z0-9]$'),
    CONSTRAINT chk_plugin_publishers_trust CHECK (trust_status IN ('pending', 'trusted', 'suspended', 'revoked')),
    CONSTRAINT chk_plugin_publishers_metadata CHECK (jsonb_typeof(metadata) = 'object')
);
CREATE UNIQUE INDEX uk_plugin_publishers_slug_active ON plugin_publishers(slug) WHERE deleted_at IS NULL;
CREATE INDEX idx_plugin_publishers_created_by ON plugin_publishers(created_by);

ALTER TABLE plugins
    ADD COLUMN publisher_id BIGINT,
    ADD CONSTRAINT fk_plugins_publisher FOREIGN KEY (publisher_id) REFERENCES plugin_publishers(id) ON DELETE SET NULL;
CREATE INDEX idx_plugins_publisher ON plugins(publisher_id);

CREATE TABLE plugin_versions (
    id BIGINT PRIMARY KEY,
    plugin_id BIGINT NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
    version VARCHAR(64) NOT NULL,
    package_digest CHAR(64) NOT NULL,
    signature_state VARCHAR(16) NOT NULL DEFAULT 'unsigned',
    channel VARCHAR(16) NOT NULL DEFAULT 'stable',
    lifecycle_status VARCHAR(16) NOT NULL DEFAULT 'staged',
    manifest_api_version VARCHAR(32) NOT NULL,
    host_api_version VARCHAR(32) NOT NULL,
    permission_fingerprint CHAR(64) NOT NULL,
    manifest JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activated_at TIMESTAMPTZ,
    retired_at TIMESTAMPTZ,
    CONSTRAINT chk_plugin_versions_version CHECK (version ~ '^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$'),
    CONSTRAINT chk_plugin_versions_digest CHECK (package_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_plugin_versions_permission_fingerprint CHECK (permission_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_plugin_versions_signature CHECK (signature_state IN ('unsigned', 'verified', 'rejected', 'revoked')),
    CONSTRAINT chk_plugin_versions_channel CHECK (channel IN ('stable', 'beta', 'canary')),
    CONSTRAINT chk_plugin_versions_lifecycle CHECK (lifecycle_status IN ('staged', 'active', 'retired', 'rejected')),
    CONSTRAINT chk_plugin_versions_manifest CHECK (jsonb_typeof(manifest) = 'object'),
    CONSTRAINT chk_plugin_versions_metadata CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT uk_plugin_versions_version UNIQUE (plugin_id, version),
    CONSTRAINT uk_plugin_versions_digest UNIQUE (plugin_id, package_digest)
);
CREATE INDEX idx_plugin_versions_plugin_status ON plugin_versions(plugin_id, lifecycle_status, created_at DESC);
CREATE INDEX idx_plugin_versions_created_by ON plugin_versions(created_by);
CREATE UNIQUE INDEX uk_plugin_versions_active ON plugin_versions(plugin_id) WHERE lifecycle_status = 'active';

CREATE TABLE plugin_capability_declarations (
    id BIGINT PRIMARY KEY,
    plugin_version_id BIGINT NOT NULL REFERENCES plugin_versions(id) ON DELETE CASCADE,
    capability_code VARCHAR(160) NOT NULL,
    purpose TEXT NOT NULL,
    risk_level VARCHAR(16) NOT NULL DEFAULT 'low',
    required BOOLEAN NOT NULL DEFAULT FALSE,
    resource_scope JSONB NOT NULL DEFAULT '{}'::jsonb,
    data_classification VARCHAR(16) NOT NULL DEFAULT 'internal',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_plugin_capability_code CHECK (capability_code ~ '^[a-z0-9_]+(\.[a-z0-9_]+){2,}$'),
    CONSTRAINT chk_plugin_capability_risk CHECK (risk_level IN ('low', 'medium', 'high')),
    CONSTRAINT chk_plugin_capability_scope CHECK (jsonb_typeof(resource_scope) = 'object'),
    CONSTRAINT chk_plugin_capability_data_class CHECK (data_classification IN ('public', 'internal', 'sensitive', 'restricted')),
    CONSTRAINT uk_plugin_capability_declaration UNIQUE (plugin_version_id, capability_code)
);
CREATE INDEX idx_plugin_capability_risk ON plugin_capability_declarations(risk_level, plugin_version_id);

CREATE TABLE plugin_admin_grants (
    id BIGINT PRIMARY KEY,
    plugin_version_id BIGINT NOT NULL,
    capability_code VARCHAR(160) NOT NULL,
    status VARCHAR(16) NOT NULL,
    granted_scope JSONB NOT NULL DEFAULT '{}'::jsonb,
    policy_revision BIGINT NOT NULL DEFAULT 1,
    decided_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    reason TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    superseded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_plugin_admin_grant_declaration FOREIGN KEY (plugin_version_id, capability_code)
        REFERENCES plugin_capability_declarations(plugin_version_id, capability_code) ON DELETE CASCADE,
    CONSTRAINT chk_plugin_admin_grants_status CHECK (status IN ('granted', 'denied', 'revoked')),
    CONSTRAINT chk_plugin_admin_grants_scope CHECK (jsonb_typeof(granted_scope) = 'object'),
    CONSTRAINT chk_plugin_admin_grants_revision CHECK (policy_revision > 0)
);
CREATE UNIQUE INDEX uk_plugin_admin_grants_current ON plugin_admin_grants(plugin_version_id, capability_code) WHERE superseded_at IS NULL;
CREATE INDEX idx_plugin_admin_grants_declaration ON plugin_admin_grants(plugin_version_id, capability_code);
CREATE INDEX idx_plugin_admin_grants_status_expiry ON plugin_admin_grants(status, expires_at);
CREATE INDEX idx_plugin_admin_grants_decided_by ON plugin_admin_grants(decided_by);

CREATE TABLE plugin_user_consents (
    id BIGINT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plugin_version_id BIGINT NOT NULL,
    capability_code VARCHAR(160) NOT NULL,
    status VARCHAR(16) NOT NULL,
    consent_scope JSONB NOT NULL DEFAULT '{}'::jsonb,
    purpose_hash CHAR(64) NOT NULL,
    policy_revision BIGINT NOT NULL DEFAULT 1,
    decided_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    superseded_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT fk_plugin_user_consent_declaration FOREIGN KEY (plugin_version_id, capability_code)
        REFERENCES plugin_capability_declarations(plugin_version_id, capability_code) ON DELETE CASCADE,
    CONSTRAINT chk_plugin_user_consents_status CHECK (status IN ('granted', 'denied', 'revoked')),
    CONSTRAINT chk_plugin_user_consents_scope CHECK (jsonb_typeof(consent_scope) = 'object'),
    CONSTRAINT chk_plugin_user_consents_purpose_hash CHECK (purpose_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_plugin_user_consents_revision CHECK (policy_revision > 0),
    CONSTRAINT chk_plugin_user_consents_metadata CHECK (jsonb_typeof(metadata) = 'object')
);
CREATE UNIQUE INDEX uk_plugin_user_consents_current ON plugin_user_consents(user_id, plugin_version_id, capability_code) WHERE superseded_at IS NULL;
CREATE INDEX idx_plugin_user_consents_user_status ON plugin_user_consents(user_id, status, expires_at);
CREATE INDEX idx_plugin_user_consents_declaration ON plugin_user_consents(plugin_version_id, capability_code);

CREATE TABLE plugin_delegations (
    id BIGINT PRIMARY KEY,
    plugin_version_id BIGINT NOT NULL REFERENCES plugin_versions(id) ON DELETE CASCADE,
    subject_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_digest CHAR(64) NOT NULL,
    granted_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    resource_scope JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    not_before TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_plugin_delegations_digest CHECK (token_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_plugin_delegations_capabilities CHECK (jsonb_typeof(granted_capabilities) = 'array'),
    CONSTRAINT chk_plugin_delegations_scope CHECK (jsonb_typeof(resource_scope) = 'object'),
    CONSTRAINT chk_plugin_delegations_status CHECK (status IN ('active', 'expired', 'revoked')),
    CONSTRAINT chk_plugin_delegations_window CHECK (expires_at > not_before)
);
CREATE UNIQUE INDEX uk_plugin_delegations_token_digest ON plugin_delegations(token_digest);
CREATE INDEX idx_plugin_delegations_subject_status ON plugin_delegations(subject_user_id, status, expires_at);
CREATE INDEX idx_plugin_delegations_version ON plugin_delegations(plugin_version_id);
CREATE INDEX idx_plugin_delegations_created_by ON plugin_delegations(created_by);

CREATE TABLE plugin_secret_values (
    id BIGINT PRIMARY KEY,
    plugin_id BIGINT NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
    owner_user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    secret_name VARCHAR(128) NOT NULL,
    key_version VARCHAR(64) NOT NULL,
    algorithm VARCHAR(32) NOT NULL,
    ciphertext BYTEA NOT NULL,
    nonce BYTEA NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT chk_plugin_secret_values_name CHECK (secret_name ~ '^[A-Za-z][A-Za-z0-9_.-]{0,127}$'),
    CONSTRAINT chk_plugin_secret_values_status CHECK (status IN ('active', 'rotated', 'revoked')),
    CONSTRAINT chk_plugin_secret_values_payload CHECK (octet_length(ciphertext) > 0 AND octet_length(nonce) >= 12),
    CONSTRAINT chk_plugin_secret_values_metadata CHECK (jsonb_typeof(metadata) = 'object')
);
CREATE UNIQUE INDEX uk_plugin_secret_values_active ON plugin_secret_values(plugin_id, owner_user_id, secret_name) NULLS NOT DISTINCT WHERE revoked_at IS NULL;
CREATE INDEX idx_plugin_secret_values_plugin_status ON plugin_secret_values(plugin_id, status);
CREATE INDEX idx_plugin_secret_values_owner ON plugin_secret_values(owner_user_id);
CREATE INDEX idx_plugin_secret_values_created_by ON plugin_secret_values(created_by);

CREATE TABLE plugin_authorization_decisions (
    id BIGINT PRIMARY KEY,
    request_id UUID NOT NULL,
    plugin_version_id BIGINT NOT NULL REFERENCES plugin_versions(id) ON DELETE RESTRICT,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    capability_code VARCHAR(160) NOT NULL,
    operation_code VARCHAR(160) NOT NULL,
    resource_scope JSONB NOT NULL DEFAULT '{}'::jsonb,
    admin_grant_id BIGINT REFERENCES plugin_admin_grants(id) ON DELETE SET NULL,
    user_consent_id BIGINT REFERENCES plugin_user_consents(id) ON DELETE SET NULL,
    delegation_id BIGINT REFERENCES plugin_delegations(id) ON DELETE SET NULL,
    outcome VARCHAR(16) NOT NULL,
    reason_code VARCHAR(80) NOT NULL,
    policy_revision BIGINT NOT NULL,
    trace_id VARCHAR(64) NOT NULL DEFAULT '',
    context JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_plugin_authorization_outcome CHECK (outcome IN ('allow', 'deny', 'error')),
    CONSTRAINT fk_plugin_authorization_declaration FOREIGN KEY (plugin_version_id, capability_code)
        REFERENCES plugin_capability_declarations(plugin_version_id, capability_code) ON DELETE RESTRICT,
    CONSTRAINT chk_plugin_authorization_scope CHECK (jsonb_typeof(resource_scope) = 'object'),
    CONSTRAINT chk_plugin_authorization_context CHECK (jsonb_typeof(context) = 'object'),
    CONSTRAINT chk_plugin_authorization_revision CHECK (policy_revision > 0),
    CONSTRAINT uk_plugin_authorization_request UNIQUE (request_id)
);
CREATE INDEX idx_plugin_authorization_plugin_created ON plugin_authorization_decisions(plugin_version_id, created_at DESC);
CREATE INDEX idx_plugin_authorization_declaration ON plugin_authorization_decisions(plugin_version_id, capability_code);
CREATE INDEX idx_plugin_authorization_user_created ON plugin_authorization_decisions(user_id, created_at DESC) WHERE user_id IS NOT NULL;
CREATE INDEX idx_plugin_authorization_denied_created ON plugin_authorization_decisions(created_at DESC) WHERE outcome <> 'allow';
CREATE INDEX idx_plugin_authorization_admin_grant ON plugin_authorization_decisions(admin_grant_id);
CREATE INDEX idx_plugin_authorization_user_consent ON plugin_authorization_decisions(user_consent_id);
CREATE INDEX idx_plugin_authorization_delegation ON plugin_authorization_decisions(delegation_id);
