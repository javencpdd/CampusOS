-- Preserve runnable semantics when reverting to the baseline vocabulary.
UPDATE plugins SET runtime = 'grpc' WHERE runtime = 'process';
ALTER TABLE plugins DROP CONSTRAINT IF EXISTS chk_plugins_runtime;
ALTER TABLE plugins
    ADD CONSTRAINT chk_plugins_runtime
    CHECK (runtime IN ('builtin', 'grpc', 'wasm'));
