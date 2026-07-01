-- 个人主页风格包应用状态

ALTER TABLE user_spaces
    ADD COLUMN IF NOT EXISTS style_name VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS style_version VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS style_manifest JSONB NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_user_spaces_style_name
    ON user_spaces(style_name)
    WHERE deleted_at IS NULL AND style_name != '';
