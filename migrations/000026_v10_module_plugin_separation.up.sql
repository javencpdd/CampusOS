-- v10 physical separation of compiled modules, external plugins, and resource
-- packages. Existing Built-in Feature state/config remains authoritative;
-- historical plugin rows are only a one-time migration source.

WITH legacy AS (
    SELECT CASE name
               WHEN 'category-moderation' THEN 'moderation'
               ELSE name
           END AS feature_id,
           status <> 'stopped' AS enabled,
           COALESCE(config, '{}'::jsonb) AS config
    FROM plugins
    WHERE name IN ('category-moderation', 'personal-space', 'controlled-richtext-article', 'personal-schedule')
      AND deleted_at IS NULL
)
INSERT INTO builtin_feature_states (feature_id, desired_enabled, effective_enabled, pending_restart, config)
SELECT feature_id,
       CASE WHEN feature_id = 'moderation' THEN TRUE ELSE enabled END,
       CASE WHEN feature_id = 'moderation' THEN TRUE ELSE enabled END,
       FALSE,
       config
FROM legacy
ON CONFLICT (feature_id) DO UPDATE SET
    config = CASE
        WHEN builtin_feature_states.config = '{}'::jsonb THEN EXCLUDED.config
        ELSE builtin_feature_states.config
    END;

WITH appearance_config AS (
    SELECT
        (SELECT config FROM plugins WHERE name = 'homepage-customizer' AND deleted_at IS NULL LIMIT 1) AS homepage,
        (SELECT config FROM plugins WHERE name = 'web-theme' AND deleted_at IS NULL LIMIT 1) AS web_theme,
        COALESCE((SELECT status <> 'stopped' FROM plugins WHERE name = 'web-theme' AND deleted_at IS NULL LIMIT 1), TRUE) AS enabled
), normalized AS (
    SELECT enabled,
           CASE WHEN homepage IS NULL AND web_theme IS NULL THEN '{}'::jsonb
                ELSE jsonb_build_object(
                    'homepage', jsonb_build_object(
                        'hero_title', '欢迎来到 CampusOS',
                        'hero_subtitle', '下一代校园社区引擎 - 事件驱动、AI Native 的社区操作系统',
                        'background_image', '',
                        'background_overlay', 'rgba(15, 23, 42, 0.45)',
                        'show_category_tags', TRUE,
                        'category_tag_limit', 8,
                        'custom_html_enabled', FALSE,
                        'custom_html', '',
                        'custom_css', '',
                        'active_style_pack', '',
                        'style_pack_version', ''
                    ) || COALESCE(homepage, '{}'::jsonb),
                    'web_theme', jsonb_build_object(
                        'default_style_pack', 'campus-canvas',
                        'allow_user_switch', TRUE
                    ) || COALESCE(web_theme, '{}'::jsonb)
                )
           END AS config
    FROM appearance_config
)
INSERT INTO builtin_feature_states (feature_id, desired_enabled, effective_enabled, pending_restart, config)
SELECT 'appearance', enabled, enabled, FALSE, config FROM normalized
ON CONFLICT (feature_id) DO UPDATE SET
    config = CASE
        WHEN builtin_feature_states.config = '{}'::jsonb THEN EXCLUDED.config
        ELSE builtin_feature_states.config
    END;

-- Fill descriptors that had no historical record only after legacy state has
-- had the opportunity to seed the Feature Store. Existing Feature rows remain
-- authoritative and are never reset to the defaults below.
INSERT INTO builtin_feature_states (feature_id, desired_enabled, effective_enabled, pending_restart, config) VALUES
    ('moderation', TRUE, TRUE, FALSE, '{}'::jsonb),
    ('personal-space', TRUE, TRUE, FALSE, '{}'::jsonb),
    ('controlled-richtext-article', TRUE, TRUE, FALSE, '{}'::jsonb),
    ('personal-schedule', TRUE, TRUE, FALSE, '{}'::jsonb),
    ('appearance', TRUE, TRUE, FALSE, '{}'::jsonb)
ON CONFLICT (feature_id) DO NOTHING;

-- The obsolete v0.7 compatibility row used web-theme as a feature ID. Its
-- effective state/config now belongs to appearance.
DELETE FROM builtin_feature_states WHERE feature_id = 'web-theme';

-- Keep historical rows for rollback and audit, but remove them from the active
-- external Plugin Catalog. No user content or module data is deleted.
UPDATE plugins
SET deleted_at = COALESCE(deleted_at, NOW()), updated_at = NOW()
WHERE name IN (
    'category-moderation', 'personal-space', 'controlled-richtext-article',
    'personal-schedule', 'homepage-customizer', 'web-theme'
  )
  AND runtime = 'builtin'
  AND deleted_at IS NULL;

-- Feature administration is no longer authorized through plugin lifecycle
-- permissions. Legacy role-bound rows remain additive during the v10 window.
INSERT INTO permissions (id, role_id, resource, action) VALUES
    (56, 1, 'feature', 'read'),
    (57, 1, 'feature', 'configure'),
    (58, 1, 'feature', 'lifecycle')
ON CONFLICT DO NOTHING;

INSERT INTO permission_definitions
    (id, code, domain, resource, action, description, risk_level, allowed_scope_types, audit_level, created_at, updated_at)
VALUES
    (900000000000001101, 'platform.feature.read', 'platform', 'feature', 'read', '查看内置功能与核心模块配置', 'low', '["global"]'::jsonb, 'standard', NOW(), NOW()),
    (900000000000001102, 'platform.feature.configure', 'platform', 'feature', 'configure', '修改内置功能或核心策略配置', 'medium', '["global"]'::jsonb, 'required', NOW(), NOW()),
    (900000000000001103, 'platform.feature.lifecycle', 'platform', 'feature', 'lifecycle', '调整内置功能目标启停状态', 'high', '["global"]'::jsonb, 'required', NOW(), NOW())
ON CONFLICT (code) DO NOTHING;

WITH admin_bindings AS (
    SELECT r.id AS role_id, pd.id AS permission_id,
           ROW_NUMBER() OVER (ORDER BY pd.code) AS row_number
    FROM roles r
    JOIN permission_definitions pd ON pd.code IN (
        'platform.feature.read', 'platform.feature.configure', 'platform.feature.lifecycle'
    )
    WHERE r.name = 'admin' AND r.deleted_at IS NULL
)
INSERT INTO role_permissions (id, role_id, permission_id, created_by, created_at)
SELECT 900000000000301100 + row_number, role_id, permission_id, 'v10-module-separation', NOW()
FROM admin_bindings
ON CONFLICT (role_id, permission_id) WHERE deleted_at IS NULL DO NOTHING;
