-- Rollback is intentionally blocked while a UI-only release is installed.
-- Removing the constraint first would leave the old runtime model inconsistent.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM plugins WHERE runtime = 'none') THEN
        RAISE EXCEPTION 'cannot roll back 000002 while runtime=none plugins are installed; uninstall them first';
    END IF;
END $$;

ALTER TABLE plugins DROP CONSTRAINT IF EXISTS chk_plugins_runtime;
ALTER TABLE plugins
    ADD CONSTRAINT chk_plugins_runtime
    CHECK (runtime IN ('builtin', 'grpc', 'process', 'wasm'));
