CREATE TABLE IF NOT EXISTS builtin_feature_states (
    feature_id        VARCHAR(128) PRIMARY KEY,
    desired_enabled   BOOLEAN NOT NULL,
    effective_enabled BOOLEAN NOT NULL,
    pending_restart   BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at        TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Existing builtin plugin records seed the Feature Store once. Future writes
-- belong to builtin_feature_states; the plugin record remains a compatibility
-- projection for old Admin/API clients.
INSERT INTO builtin_feature_states (feature_id, desired_enabled, effective_enabled, pending_restart)
SELECT name, status <> 'stopped', status <> 'stopped', FALSE
FROM plugins
WHERE name IN ('personal-space', 'controlled-richtext-article', 'personal-schedule', 'web-theme')
  AND deleted_at IS NULL
ON CONFLICT (feature_id) DO NOTHING;
