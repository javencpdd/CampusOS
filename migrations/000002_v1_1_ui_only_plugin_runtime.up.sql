-- v1.1 plugin package refactor: a declarative external package can provide
-- only verified UI artifacts and therefore has no executable backend runtime.
-- The `none` value is intentionally a runtime identity, not a database or
-- authorization bypass: the host still persists lifecycle and grant state.

ALTER TABLE plugins DROP CONSTRAINT IF EXISTS chk_plugins_runtime;
ALTER TABLE plugins
    ADD CONSTRAINT chk_plugins_runtime
    CHECK (runtime IN ('builtin', 'grpc', 'process', 'wasm', 'none'));
