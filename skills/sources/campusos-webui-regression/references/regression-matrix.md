# CampusOS WebUI Regression Matrix

> 更新时间：2026-08-03

## Surface routing

| Symptom | Inspect first | Required controls |
| --- | --- | --- |
| Group/board list is empty or mixed | `CampusApp.vue`, `ThreadListView.vue`, Community handler/service/repository, categories + threads | Group aggregate request, one child-board request, empty group, typed Thread coverage |
| Mutual Aid/Secondhand form/list/detail | Feature Vue/API, Feature service/store, base Community Thread, detail migration | Feature enabled, board policy, detail exists, public states, edit ownership |
| Decimal price | Secondhand editor/model/service/store, OpenAPI | Two decimal conversion, min/max, integer minor-unit persistence, overflow rejection |
| Missing Admin user email/storage | Admin component/API, protected admin projection, permission/admission middleware | Public endpoint still redacts, admin permission negative test, unbound value |
| Missing reply/moderation notification | Thread/Post service, notification repository, transaction/outbox | Other-user, self, duplicate recipient, rollback-on-write-failure |
| Avatar history/quota/image behavior | Personal Space page/service/file store, User Storage quota | Three-source FIFO, switch does not reorder, upload eviction, compressed byte accounting |
| First module click looks like refresh | Router preload/runtime registry, auth/manifest sync, Network `document` requests | Static/runtime path collision, repeated manifest, concurrent sync, no explicit reload |
| Docker platform logs stale | Platform log source/SSE, `deploy/docker/dev-*`, mounted `.campusos/logs` | Container stdout and file stream, reconnect, source allowlist |

## Common cross-layer failures

- A caught non-2xx response leaves the initial empty array, so the UI looks like valid “no data.” Inspect status and body.
- JavaScript-safe string IDs reach PostgreSQL BIGINT columns. Array filters must use a compatible `bigint[]`, not `text[]`.
- Specialized Feature endpoints and the generic `/threads` list may impose different filters; verify both when group navigation
  should include typed Threads.
- Public and Admin projections intentionally differ. Adding a field to one must not leak it through the other.
- Docker bind mounts can hot-reload source while an old image still owns dependencies/entrypoints. Determine which input
  changed before choosing `up` or `rebuild`.

## Existing test entry points

- Backend: owning package tests under `internal/modules/**`; run focused packages before `go test ./...`.
- Web unit: `web/tests/category-navigation.spec.ts`, `structured-content.spec.ts`, `notification-center.spec.ts`,
  `router-runtime.spec.ts`, and task-specific Vitest files.
- Browser workflows: `web/tests/*browser*.mjs` and `responsive-workflow.mjs` when prerequisites are available.
- Admin unit: `admin/tests/identity-user-api.spec.ts`, `router-auth.spec.ts`, and owning module tests.
- API/OpenAPI: `make contracts-check`, generated-file checks, and the exact live HTTP request.
- Docker: `make docker-dev-test`, platform logs, health endpoints, and `docker-dev.* ps`.

Do not claim an end-to-end visual pass from unit/API tests alone. Conversely, do not stop at a screenshot when a repository,
transaction, permission, or schema defect needs deterministic coverage.
