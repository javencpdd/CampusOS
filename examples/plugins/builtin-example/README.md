# Built-in plugin template

Built-in plugins are compiled and registered with CampusOS. Lifecycle changes take effect after a server restart. Use this template for trusted code that needs in-process integration.

```bash
go run ./cmd/campusosctl plugin dev examples/plugins/builtin-example
go run ./cmd/campusosctl plugin pack examples/plugins/builtin-example
```
