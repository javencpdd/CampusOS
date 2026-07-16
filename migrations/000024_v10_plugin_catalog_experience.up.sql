ALTER TABLE plugin_catalog_entries
    ADD COLUMN IF NOT EXISTS experience JSONB NOT NULL DEFAULT '{}'::jsonb;
