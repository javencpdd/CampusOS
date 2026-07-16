# Campus Welcome external plugin

This reference plugin demonstrates the complete dynamic UI and Extension
Gateway path without using an in-process Built-in Runtime:

1. Build the independent loopback process as `plugin`.
2. Inspect or package the directory as a normal External Plugin.
3. CampusOS loads its declarative route, navigation, surface, and action.
4. The action calls `/api/v1/extensions/campus-welcome/welcome`.
5. Core injects caller context, timeout, size limits, and audit metadata before
   dispatching to the process endpoint.

```bash
cd examples/plugins/campus-welcome
go test ./...
go build -o plugin .
cd ../../..
go run ./cmd/campusosctl plugin dev examples/plugins/campus-welcome
```

The process receives no database connection, JWT signing key, user token, or
unrestricted host filesystem capability.
