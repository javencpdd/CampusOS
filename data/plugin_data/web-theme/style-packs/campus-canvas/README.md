# Campus Canvas

Administrator-provided `target: web` example for the complete CampusOS user frontend.

- CSS is scoped to `.app-container[data-campusos-web]`.
- The effect runs in `sandbox-worker.v1` and cannot access the page DOM, JWT, browser storage or arbitrary network resources.
- `community.threads.read` is a public, read-only CampusStyleSDK capability used only to tune the background activity density.
- Users select this package locally from the user avatar menu. Installing or editing source packages remains an administrator operation.
