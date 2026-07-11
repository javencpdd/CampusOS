# Wasm plugin template

`plugin.wasm` is a minimal precompiled module with a `handle_event` export. Replace it with output from a WASI-compatible toolchain while keeping the Manifest entrypoint and timeout explicit.

```bash
go run ./cmd/campusosctl plugin dev examples/plugins/wasm-example
go run ./cmd/campusosctl plugin pack examples/plugins/wasm-example
```
