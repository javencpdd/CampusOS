DROP INDEX IF EXISTS idx_categories_default_tags;

ALTER TABLE categories
    DROP COLUMN IF EXISTS default_tags;
