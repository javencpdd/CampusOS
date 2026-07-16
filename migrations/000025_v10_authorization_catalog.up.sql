-- v10 authorization catalog. The legacy permissions(role_id, resource,
-- action) table remains intact during the compatibility period.

CREATE TABLE IF NOT EXISTS permission_definitions (
    id                  BIGINT PRIMARY KEY,
    code                VARCHAR(160) NOT NULL,
    domain              VARCHAR(64) NOT NULL,
    resource            VARCHAR(64) NOT NULL,
    action              VARCHAR(64) NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    risk_level          VARCHAR(16) NOT NULL DEFAULT 'low',
    allowed_scope_types JSONB NOT NULL DEFAULT '["global"]'::jsonb,
    audit_level         VARCHAR(16) NOT NULL DEFAULT 'standard',
    deprecated_at       TIMESTAMP NULL,
    created_at          TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_permission_definition_code CHECK (code ~ '^[a-z0-9_]+(\.[a-z0-9_]+){2,}$'),
    CONSTRAINT chk_permission_definition_risk CHECK (risk_level IN ('low', 'medium', 'high')),
    CONSTRAINT chk_permission_definition_audit CHECK (audit_level IN ('standard', 'required'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_permission_definitions_code
    ON permission_definitions(code);

CREATE TABLE IF NOT EXISTS role_permissions (
    id            BIGINT PRIMARY KEY,
    role_id       BIGINT NOT NULL,
    permission_id BIGINT NOT NULL,
    created_by    VARCHAR(64) NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMP NULL,
    CONSTRAINT fk_v10_role_permissions_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE RESTRICT,
    CONSTRAINT fk_v10_role_permissions_permission FOREIGN KEY (permission_id) REFERENCES permission_definitions(id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_role_permissions_active
    ON role_permissions(role_id, permission_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_role_permissions_role
    ON role_permissions(role_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS route_operations (
    id             BIGINT PRIMARY KEY,
    operation_code VARCHAR(200) NOT NULL,
    module_owner   VARCHAR(128) NOT NULL,
    method         VARCHAR(12) NOT NULL,
    path_template  TEXT NOT NULL,
    audience       VARCHAR(32) NOT NULL,
    legacy_aliases JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at     TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_route_operation_code CHECK (operation_code ~ '^[a-z0-9_]+(\.[a-z0-9_]+){2,}$')
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_route_operations_code
    ON route_operations(operation_code);
CREATE INDEX IF NOT EXISTS idx_route_operations_owner
    ON route_operations(module_owner, audience);

CREATE TABLE IF NOT EXISTS route_permission_bindings (
    id                 BIGINT PRIMARY KEY,
    route_operation_id BIGINT NOT NULL,
    permission_id      BIGINT NOT NULL,
    created_at         TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at         TIMESTAMP NULL,
    CONSTRAINT fk_route_permission_operation FOREIGN KEY (route_operation_id) REFERENCES route_operations(id) ON DELETE CASCADE,
    CONSTRAINT fk_route_permission_definition FOREIGN KEY (permission_id) REFERENCES permission_definitions(id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_route_permission_bindings_active
    ON route_permission_bindings(route_operation_id, permission_id) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS authorization_audits (
    id              BIGINT PRIMARY KEY,
    request_id      VARCHAR(128) NOT NULL DEFAULT '',
    actor_id        BIGINT NULL,
    permission_code VARCHAR(160) NOT NULL DEFAULT '',
    operation_code  VARCHAR(200) NOT NULL DEFAULT '',
    scope_type      VARCHAR(32) NOT NULL DEFAULT '',
    scope_id        BIGINT NULL,
    resource_type   VARCHAR(64) NOT NULL DEFAULT '',
    resource_id     VARCHAR(128) NOT NULL DEFAULT '',
    outcome         VARCHAR(16) NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    ip_address      VARCHAR(64) NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_authorization_audits_outcome CHECK (outcome IN ('allow', 'deny', 'error'))
);
CREATE INDEX IF NOT EXISTS idx_authorization_audits_created
    ON authorization_audits(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_authorization_audits_actor
    ON authorization_audits(actor_id, created_at DESC);

-- Backfill an independent catalog from the legacy role-bound rows. It is
-- deliberately additive: reads can compare both representations before the
-- old table is deprecated in a later release.
WITH legacy_permissions AS (
    SELECT DISTINCT resource, action,
        CASE resource
            WHEN 'user' THEN 'identity'
            WHEN 'role' THEN 'identity'
            WHEN 'thread' THEN 'community'
            WHEN 'post' THEN 'community'
            WHEN 'category' THEN 'community'
            WHEN 'richtext' THEN 'community'
            WHEN 'space' THEN 'personal_space'
            WHEN 'homepage' THEN 'appearance'
            WHEN 'plugin' THEN 'plugin'
            WHEN 'webhook' THEN 'integration'
            WHEN 'mcp' THEN 'integration'
            WHEN 'message' THEN 'integration'
            WHEN 'integration' THEN 'integration'
            WHEN 'metrics' THEN 'platform'
            WHEN 'platform_log' THEN 'platform'
            WHEN 'ai' THEN 'ai'
            ELSE 'platform'
        END AS domain
    FROM permissions
    WHERE deleted_at IS NULL
), catalog AS (
    SELECT resource, action, domain, domain || '.' || resource || '.' || action AS code,
        ROW_NUMBER() OVER (ORDER BY domain, resource, action) AS row_number
    FROM legacy_permissions
)
INSERT INTO permission_definitions (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, created_at, updated_at)
SELECT 900000000000000000 + row_number, code, domain, resource, action, code,
       CASE WHEN action IN ('delete','suspend','assign','revoke','install','uninstall','execute') THEN 'high'
            WHEN action IN ('write','configure','lifecycle','lock','pin') THEN 'medium'
            ELSE 'low' END,
       CASE WHEN resource IN ('thread','post','category') THEN '["global","category"]'::jsonb ELSE '["global"]'::jsonb END,
       CASE WHEN action IN ('delete','suspend','assign','revoke','install','uninstall','execute') THEN 'required' ELSE 'standard' END,
       NOW(), NOW()
FROM catalog
ON CONFLICT (code) DO NOTHING;

-- High-risk content and role actions are intentionally separate from the
-- historical broad write/delete permissions. They remain mapped to the old
-- role grants only for the migration compatibility window.
INSERT INTO permission_definitions (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, created_at, updated_at) VALUES
    (900000000000001001, 'community.thread.take_down', 'community', 'thread', 'take_down', '下架帖子', 'high', '["global","category"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001002, 'community.thread.review', 'community', 'thread', 'review', '审核帖子', 'high', '["global","category"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001003, 'community.thread.direct_restore', 'community', 'thread', 'direct_restore', '直接恢复帖子', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001004, 'community.thread.restore', 'community', 'thread', 'restore', '从回收站恢复帖子', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001005, 'community.thread.purge', 'community', 'thread', 'purge', '永久清除帖子', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001006, 'community.thread.trash', 'community', 'thread', 'trash', '移入帖子回收站', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001007, 'identity.role.create', 'identity', 'role', 'create', '创建自定义角色', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001008, 'identity.role.update_permissions', 'identity', 'role', 'update_permissions', '调整角色权限矩阵', 'high', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001009, 'identity.role.read_audit', 'identity', 'role', 'read_audit', '查看授权审计', 'medium', '["global"]'::jsonb, 'standard', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

WITH legacy_bindings AS (
    SELECT p.role_id, pd.id AS permission_id,
        ROW_NUMBER() OVER (ORDER BY p.role_id, pd.id) AS row_number
    FROM permissions p
    INNER JOIN permission_definitions pd ON pd.code = (
        CASE p.resource
            WHEN 'user' THEN 'identity'
            WHEN 'role' THEN 'identity'
            WHEN 'thread' THEN 'community'
            WHEN 'post' THEN 'community'
            WHEN 'category' THEN 'community'
            WHEN 'richtext' THEN 'community'
            WHEN 'space' THEN 'personal_space'
            WHEN 'homepage' THEN 'appearance'
            WHEN 'plugin' THEN 'plugin'
            WHEN 'webhook' THEN 'integration'
            WHEN 'mcp' THEN 'integration'
            WHEN 'message' THEN 'integration'
            WHEN 'integration' THEN 'integration'
            WHEN 'metrics' THEN 'platform'
            WHEN 'platform_log' THEN 'platform'
            WHEN 'ai' THEN 'ai'
            ELSE 'platform'
        END || '.' || p.resource || '.' || p.action
    )
    WHERE p.deleted_at IS NULL
)
INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000100000 + row_number, role_id, permission_id, 'v10-backfill', NOW()
FROM legacy_bindings
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;

WITH special_bindings AS (
    SELECT r.id AS role_id, pd.id AS permission_id,
        ROW_NUMBER() OVER (ORDER BY r.id, pd.id) AS row_number
    FROM roles r
    INNER JOIN permission_definitions pd ON (
        (r.name IN ('admin','moderator') AND pd.code='community.thread.take_down')
        OR (r.name='admin' AND pd.code IN ('community.thread.review','community.thread.direct_restore','community.thread.restore','community.thread.purge','community.thread.trash','identity.role.create','identity.role.update_permissions','identity.role.read_audit'))
    )
    WHERE r.deleted_at IS NULL
)
INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000200000 + row_number, role_id, permission_id, 'v10-backfill', NOW()
FROM special_bindings
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;
