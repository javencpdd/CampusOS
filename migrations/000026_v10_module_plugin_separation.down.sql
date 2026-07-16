-- Restore the historical plugin projection for an application rollback. New
-- Feature rows/config are retained so this down migration never loses data.

INSERT INTO builtin_feature_states (feature_id, desired_enabled, effective_enabled, pending_restart, config)
SELECT 'web-theme', desired_enabled, effective_enabled, pending_restart,
       COALESCE(config -> 'web_theme', '{}'::jsonb)
FROM builtin_feature_states
WHERE feature_id = 'appearance'
ON CONFLICT (feature_id) DO UPDATE SET
    desired_enabled = EXCLUDED.desired_enabled,
    effective_enabled = EXCLUDED.effective_enabled,
    pending_restart = EXCLUDED.pending_restart,
    config = EXCLUDED.config,
    updated_at = NOW();

UPDATE plugins p
SET deleted_at = NULL,
    status = CASE WHEN s.desired_enabled THEN 'running' ELSE 'stopped' END,
    config = CASE
        WHEN p.name = 'web-theme' THEN COALESCE(a.config -> 'web_theme', p.config)
        WHEN p.name = 'homepage-customizer' THEN COALESCE(a.config -> 'homepage', p.config)
        ELSE COALESCE(s.config, p.config)
    END,
    updated_at = NOW()
FROM builtin_feature_states s
LEFT JOIN builtin_feature_states a ON a.feature_id = 'appearance'
WHERE p.runtime = 'builtin'
  AND p.name IN (
      'category-moderation', 'personal-space', 'controlled-richtext-article',
      'personal-schedule', 'homepage-customizer', 'web-theme'
  )
  AND s.feature_id = CASE p.name
      WHEN 'category-moderation' THEN 'moderation'
      WHEN 'web-theme' THEN 'appearance'
      WHEN 'homepage-customizer' THEN 'appearance'
      ELSE p.name
  END;

DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permission_definitions
    WHERE code IN ('platform.feature.read', 'platform.feature.configure', 'platform.feature.lifecycle')
);
DELETE FROM permission_definitions
WHERE code IN ('platform.feature.read', 'platform.feature.configure', 'platform.feature.lifecycle');
DELETE FROM permissions
WHERE role_id = 1 AND resource = 'feature' AND action IN ('read', 'configure', 'lifecycle');
