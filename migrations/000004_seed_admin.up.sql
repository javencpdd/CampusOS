-- 种子数据：默认管理员账号
-- 管理员登录信息：
--   邮箱：admin@campusos.local
--   密码：Admin@123456
--   角色：admin（系统管理员）

-- 注意：bcrypt 哈希对应密码 "Admin@123456"
-- $2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy

-- 插入管理员用户（雪花 ID：1000000000000000001）
INSERT INTO users (id, username, nickname, email, avatar, bio, status)
VALUES (1000000000000000001, 'admin', '系统管理员', 'admin@campusos.local', '', 'CampusOS 系统管理员', 'active')
ON CONFLICT DO NOTHING;

-- 插入管理员账号凭据
INSERT INTO accounts (id, user_id, type, identifier, credential, verified)
VALUES (1000000000000000002, 1000000000000000001, 'email', 'admin@campusos.local', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy', TRUE)
ON CONFLICT DO NOTHING;

-- 分配 admin 角色给管理员用户
INSERT INTO user_roles (id, user_id, role_id, scope_type)
VALUES (1000000000000000003, 1000000000000000001, 1, 'global')
ON CONFLICT DO NOTHING;