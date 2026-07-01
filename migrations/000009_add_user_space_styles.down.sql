DROP INDEX IF EXISTS idx_user_spaces_style_name;

ALTER TABLE user_spaces
    DROP COLUMN IF EXISTS style_manifest,
    DROP COLUMN IF EXISTS style_version,
    DROP COLUMN IF EXISTS style_name;
