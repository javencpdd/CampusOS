-- RBAC 角色系统
-- 角色表
CREATE TABLE IF NOT EXISTS roles (
    id          BIGINT PRIMARY KEY,
    name        VARCHAR(32) NOT NULL,
    description VARCHAR(255) DEFAULT '',
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_roles_name ON roles(name) WHERE deleted_at IS NULL;

-- 用户-角色关联表
CREATE TABLE IF NOT EXISTS user_roles (
    id          BIGINT PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    role_id     BIGINT NOT NULL,
    scope_type  VARCHAR(20) DEFAULT 'global',
    scope_id    BIGINT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_user_roles ON user_roles(user_id, role_id, scope_type, scope_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_roles_user_id ON user_roles(user_id) WHERE deleted_at IS NULL;

-- 权限表
CREATE TABLE IF NOT EXISTS permissions (
    id          BIGINT PRIMARY KEY,
    role_id     BIGINT NOT NULL,
    resource    VARCHAR(64) NOT NULL,
    action      VARCHAR(32) NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_permissions ON permissions(role_id, resource, action) WHERE deleted_at IS NULL;

-- 初始化角色
INSERT INTO roles (id, name, description, is_system) VALUES
(1, 'admin',     '系统管理员，拥有全部权限', TRUE),
(2, 'moderator', '版主，管理帖子和用户',     TRUE),
(3, 'member',    '普通会员，发帖回帖',       TRUE),
(4, 'guest',     '未登录用户，只读浏览',     TRUE)
ON CONFLICT DO NOTHING;

-- admin 权限
INSERT INTO permissions (id, role_id, resource, action) VALUES
(1,  1, 'user',      'read'),
(2,  1, 'user',      'write'),
(3,  1, 'user',      'delete'),
(4,  1, 'user',      'suspend'),
(5,  1, 'thread',    'read'),
(6,  1, 'thread',    'write'),
(7,  1, 'thread',    'delete'),
(8,  1, 'thread',    'pin'),
(9,  1, 'post',      'read'),
(10, 1, 'post',      'write'),
(11, 1, 'post',      'delete'),
(12, 1, 'category',  'read'),
(13, 1, 'category',  'write'),
(14, 1, 'category',  'delete'),
(15, 1, 'role',      'manage')
ON CONFLICT DO NOTHING;

-- moderator 权限
INSERT INTO permissions (id, role_id, resource, action) VALUES
(16, 2, 'user',      'read'),
(17, 2, 'user',      'suspend'),
(18, 2, 'thread',    'read'),
(19, 2, 'thread',    'write'),
(20, 2, 'thread',    'delete'),
(21, 2, 'thread',    'pin'),
(22, 2, 'post',      'read'),
(23, 2, 'post',      'delete')
ON CONFLICT DO NOTHING;

-- member 权限
INSERT INTO permissions (id, role_id, resource, action) VALUES
(24, 3, 'thread',    'read'),
(25, 3, 'thread',    'write'),
(26, 3, 'post',      'read'),
(27, 3, 'post',      'write')
ON CONFLICT DO NOTHING;

-- guest 权限（仅浏览）
INSERT INTO permissions (id, role_id, resource, action) VALUES
(28, 4, 'thread',    'read'),
(29, 4, 'post',      'read'),
(30, 4, 'category',  'read')
ON CONFLICT DO NOTHING;
