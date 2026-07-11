# Campus Welcome

This built-in reference plugin demonstrates the complete v0.6 UI runtime path:

1. Core reads `lifecycle` and `ui` from `plugin.yaml`.
2. `/api/v1/ui/runtime-manifest` exposes only contributions visible to the current subject.
3. Web registers the route, navigation, Surface, and Action without rebuilding.
4. The declarative renderer uses Campus UI primitives only.
5. The Action calls `GET /api/v1/extensions/campus-welcome/welcome`.
6. Core injects trusted caller context, applies limits and timeout, dispatches to the builtin Runtime, and writes an audit log.

The backend uses restart activation because it is built into the Core process. Its frontend contribution uses hot activation and stays independently discoverable during backend restart or degradation.
