-- CampusOS v1.0 reference data.
-- No user, account, administrator credential, category, email address or test row is seeded here.

INSERT INTO roles (id, name, description, is_system, created_at, updated_at)
VALUES
    (1, 'admin', '系统管理员，拥有全部权限', TRUE, NOW(), NOW()),
    (2, 'moderator', '版主，管理帖子和用户', TRUE, NOW(), NOW()),
    (3, 'member', '普通会员，发帖回帖', TRUE, NOW(), NOW()),
    (4, 'guest', '未登录用户，只读浏览', TRUE, NOW(), NOW());

INSERT INTO permission_definitions
    (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level)
VALUES
(900000000000000001, 'ai.ai.read', 'ai', 'ai', 'read', 'ai.ai.read', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000002, 'appearance.homepage.configure', 'appearance', 'homepage', 'configure', 'appearance.homepage.configure', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000000003, 'community.category.delete', 'community', 'category', 'delete', 'community.category.delete', 'high', '["global", "category"]'::jsonb, 'required'),
    (900000000000000004, 'community.category.read', 'community', 'category', 'read', 'community.category.read', 'low', '["global", "category"]'::jsonb, 'standard'),
    (900000000000000005, 'community.category.write', 'community', 'category', 'write', 'community.category.write', 'medium', '["global", "category"]'::jsonb, 'standard'),
    (900000000000000006, 'community.post.delete', 'community', 'post', 'delete', 'community.post.delete', 'high', '["global", "category"]'::jsonb, 'required'),
    (900000000000000007, 'community.post.read', 'community', 'post', 'read', 'community.post.read', 'low', '["global", "category"]'::jsonb, 'standard'),
    (900000000000000008, 'community.post.write', 'community', 'post', 'write', 'community.post.write', 'medium', '["global", "category"]'::jsonb, 'standard'),
    (900000000000000009, 'community.richtext.moderate', 'community', 'richtext', 'moderate', 'community.richtext.moderate', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000010, 'community.thread.delete', 'community', 'thread', 'delete', 'community.thread.delete', 'high', '["global", "category"]'::jsonb, 'required'),
    (900000000000000011, 'community.thread.lock', 'community', 'thread', 'lock', 'community.thread.lock', 'medium', '["global", "category"]'::jsonb, 'standard'),
    (900000000000000012, 'community.thread.pin', 'community', 'thread', 'pin', 'community.thread.pin', 'medium', '["global", "category"]'::jsonb, 'standard'),
    (900000000000000013, 'community.thread.read', 'community', 'thread', 'read', 'community.thread.read', 'low', '["global", "category"]'::jsonb, 'standard'),
    (900000000000000014, 'community.thread.write', 'community', 'thread', 'write', 'community.thread.write', 'medium', '["global", "category"]'::jsonb, 'standard'),
    (900000000000000015, 'identity.role.assign', 'identity', 'role', 'assign', 'identity.role.assign', 'high', '["global"]'::jsonb, 'required'),
    (900000000000000016, 'identity.role.manage', 'identity', 'role', 'manage', 'identity.role.manage', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000017, 'identity.role.read', 'identity', 'role', 'read', 'identity.role.read', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000018, 'identity.role.revoke', 'identity', 'role', 'revoke', 'identity.role.revoke', 'high', '["global"]'::jsonb, 'required'),
    (900000000000000019, 'identity.user.delete', 'identity', 'user', 'delete', 'identity.user.delete', 'high', '["global"]'::jsonb, 'required'),
    (900000000000000020, 'identity.user.read', 'identity', 'user', 'read', 'identity.user.read', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000021, 'identity.user.suspend', 'identity', 'user', 'suspend', 'identity.user.suspend', 'high', '["global"]'::jsonb, 'required'),
    (900000000000000022, 'identity.user.write', 'identity', 'user', 'write', 'identity.user.write', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000000023, 'integration.integration.read', 'integration', 'integration', 'read', 'integration.integration.read', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000024, 'integration.mcp.call', 'integration', 'mcp', 'call', 'integration.mcp.call', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000025, 'integration.mcp.configure', 'integration', 'mcp', 'configure', 'integration.mcp.configure', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000000026, 'integration.mcp.read', 'integration', 'mcp', 'read', 'integration.mcp.read', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000027, 'integration.message.read', 'integration', 'message', 'read', 'integration.message.read', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000028, 'integration.message.write', 'integration', 'message', 'write', 'integration.message.write', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000000029, 'integration.webhook.execute', 'integration', 'webhook', 'execute', 'integration.webhook.execute', 'high', '["global"]'::jsonb, 'required'),
    (900000000000000030, 'integration.webhook.read', 'integration', 'webhook', 'read', 'integration.webhook.read', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000031, 'integration.webhook.write', 'integration', 'webhook', 'write', 'integration.webhook.write', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000000032, 'personal_space.space.manage', 'personal_space', 'space', 'manage', 'personal_space.space.manage', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000033, 'platform.metrics.read', 'platform', 'metrics', 'read', 'platform.metrics.read', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000034, 'platform.platform_log.read', 'platform', 'platform_log', 'read', 'platform.platform_log.read', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000035, 'plugin.plugin.configure', 'plugin', 'plugin', 'configure', 'plugin.plugin.configure', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000000036, 'plugin.plugin.install', 'plugin', 'plugin', 'install', 'plugin.plugin.install', 'high', '["global"]'::jsonb, 'required'),
    (900000000000000037, 'plugin.plugin.lifecycle', 'plugin', 'plugin', 'lifecycle', 'plugin.plugin.lifecycle', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000000038, 'plugin.plugin.read', 'plugin', 'plugin', 'read', 'plugin.plugin.read', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000000039, 'plugin.plugin.uninstall', 'plugin', 'plugin', 'uninstall', 'plugin.plugin.uninstall', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001001, 'community.thread.take_down', 'community', 'thread', 'take_down', '下架帖子', 'high', '["global", "category"]'::jsonb, 'required'),
    (900000000000001002, 'community.thread.review', 'community', 'thread', 'review', '审核帖子', 'high', '["global", "category"]'::jsonb, 'required'),
    (900000000000001003, 'community.thread.direct_restore', 'community', 'thread', 'direct_restore', '直接恢复帖子', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001004, 'community.thread.restore', 'community', 'thread', 'restore', '从回收站恢复帖子', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001005, 'community.thread.purge', 'community', 'thread', 'purge', '永久清除帖子', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001006, 'community.thread.trash', 'community', 'thread', 'trash', '移入帖子回收站', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001007, 'identity.role.create', 'identity', 'role', 'create', '创建自定义角色', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001008, 'identity.role.update_permissions', 'identity', 'role', 'update_permissions', '调整角色权限矩阵', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001009, 'identity.role.read_audit', 'identity', 'role', 'read_audit', '查看授权审计', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001010, 'platform.reliability.read', 'platform', 'reliability', 'read', '查看可靠任务状态', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001011, 'platform.reliability.replay', 'platform', 'reliability', 'replay', '重放可靠任务', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001012, 'platform.retention.preview', 'platform', 'retention', 'preview', '执行保留策略预演', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001020, 'identity.account.recovery.override', 'identity', 'account', 'recovery_override', '创建或取消管理员辅助账号恢复', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001021, 'identity.session.read', 'identity', 'session', 'read', '查看指定用户的安全会话投影', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001022, 'identity.session.revoke', 'identity', 'session', 'revoke', '撤销指定用户的全部会话', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001023, 'platform.email_delivery.read', 'platform', 'email_delivery', 'read', '查看邮件投递的脱敏运行状态', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000001024, 'identity.challenge_policy.read', 'identity', 'challenge_policy', 'read', '查看验证码请求频率策略', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001025, 'identity.challenge_policy.update', 'identity', 'challenge_policy', 'update', '修改验证码请求频率策略', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001026, 'identity.admin_account.read', 'identity', 'admin_account', 'read', '查看管理平面准入账号状态', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001027, 'identity.admin_account.suspend', 'identity', 'admin_account', 'suspend', '暂停管理平面准入并撤销会话', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001028, 'identity.admin_account.restore', 'identity', 'admin_account', 'restore', '恢复被暂停的管理平面准入', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001029, 'identity.admin_account.read_audit', 'identity', 'admin_account', 'read_audit', '查看管理平面准入变更审计', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001030, 'community.category.create', 'community', 'category', 'create', '创建版块或分组', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001031, 'community.category.update', 'community', 'category', 'update', '更新版块展示与基础设置', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001032, 'community.category.move', 'community', 'category', 'move', '移动版块层级', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001033, 'community.category.archive', 'community', 'category', 'archive', '归档版块或分组', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001034, 'community.category.restore', 'community', 'category', 'restore', '恢复已归档版块或分组', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001035, 'community.category.configure_thread_types', 'community', 'category', 'configure_thread_types', '配置板块允许发布的帖子类型', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001040, 'identity.mfa_policy.read', 'identity', 'mfa_policy', 'read', '查看管理员 MFA 强制策略与聚合覆盖率', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001041, 'identity.mfa_policy.update', 'identity', 'mfa_policy', 'update', '修改管理员 MFA 强制策略', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001042, 'identity.mfa.local_recovery', 'identity', 'mfa', 'local_recovery', '本机受控 MFA 恢复审计代码，不授予 Web 角色', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001060, 'schedule.academic_term.read', 'schedule', 'academic_term', 'read', '查看系统学期目录', 'medium', '["global"]'::jsonb, 'standard'),
    (900000000000001061, 'schedule.academic_term.manage', 'schedule', 'academic_term', 'manage', '创建、修改、关闭、开放或设定默认学期', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001062, 'schedule.academic_term.delete', 'schedule', 'academic_term', 'delete', '删除未被课表引用的学期', 'high', '["global"]'::jsonb, 'required'),
    (900000000000001101, 'platform.feature.read', 'platform', 'feature', 'read', '查看内置功能与核心模块配置', 'low', '["global"]'::jsonb, 'standard'),
    (900000000000001102, 'platform.feature.configure', 'platform', 'feature', 'configure', '修改内置功能或核心策略配置', 'medium', '["global"]'::jsonb, 'required'),
    (900000000000001103, 'platform.feature.lifecycle', 'platform', 'feature', 'lifecycle', '调整内置功能目标启停状态', 'high', '["global"]'::jsonb, 'required');

-- Administrators receive the complete non-deprecated platform permission catalog.
INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 910000000000000000 + ROW_NUMBER() OVER (ORDER BY pd.id), 1, pd.id, 'v1-baseline', NOW()
FROM permission_definitions pd
WHERE pd.deprecated_at IS NULL;

-- Non-admin defaults are deliberately minimal; additional assignments are explicit admin actions.
WITH grants(role_name, permission_code) AS (
    VALUES
        ('moderator', 'community.post.delete'),
        ('moderator', 'community.post.read'),
        ('moderator', 'community.thread.lock'),
        ('moderator', 'community.thread.pin'),
        ('moderator', 'community.thread.read'),
        ('moderator', 'community.thread.take_down'),
        ('moderator', 'identity.user.read'),
        ('member', 'community.post.read'),
        ('member', 'community.post.write'),
        ('member', 'community.thread.read'),
        ('member', 'community.thread.write'),
        ('guest', 'community.category.read'),
        ('guest', 'community.post.read'),
        ('guest', 'community.thread.read')
), resolved AS (
    SELECT r.id AS role_id, pd.id AS permission_id,
           ROW_NUMBER() OVER (ORDER BY r.id, pd.id) AS ordinal
    FROM grants g
    JOIN roles r ON r.name = g.role_name AND r.deleted_at IS NULL
    JOIN permission_definitions pd ON pd.code = g.permission_code AND pd.deprecated_at IS NULL
)
INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 920000000000000000 + ordinal, role_id, permission_id, 'v1-baseline', NOW()
FROM resolved;

INSERT INTO identity_challenge_policies
    (id, email_window_minutes, email_max_requests, ip_window_minutes, ip_max_requests, version, updated_at)
VALUES ('email_verification', 10, 5, 60, 10, 1, NOW());

INSERT INTO identity_mfa_policies (id, mode, grace_ends_at, version, updated_at)
VALUES ('admin', 'off', NULL, 1, NOW());

