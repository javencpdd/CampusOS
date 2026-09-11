-- Manifest v3 names the external process runtime explicitly. Historical
-- `grpc` remains accepted as a compatibility alias during the v1 window.
ALTER TABLE plugins DROP CONSTRAINT IF EXISTS chk_plugins_runtime;
ALTER TABLE plugins
    ADD CONSTRAINT chk_plugins_runtime
    CHECK (runtime IN ('builtin', 'grpc', 'process', 'wasm'));
