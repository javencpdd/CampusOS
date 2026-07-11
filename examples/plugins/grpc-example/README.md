# Process plugin template

This template builds the executable expected by the current `grpc` process Runtime. The Runtime name is retained for compatibility; event transport is still experimental and must not be treated as a stable protobuf contract.

```bash
go run ./cmd/campusosctl plugin dev examples/plugins/grpc-example
go run ./cmd/campusosctl plugin pack examples/plugins/grpc-example
```
