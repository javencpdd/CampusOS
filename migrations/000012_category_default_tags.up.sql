ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS default_tags TEXT[] NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_categories_default_tags
    ON categories USING GIN(default_tags)
    WHERE deleted_at IS NULL;
