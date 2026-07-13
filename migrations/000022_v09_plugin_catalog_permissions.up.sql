ALTER TABLE plugin_catalog_entries
    ADD COLUMN IF NOT EXISTS user_permissions JSONB NOT NULL DEFAULT '[]'::jsonb;
