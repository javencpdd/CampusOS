# CampusOS Current Project Status

> Snapshot date: 2026-07-16
> Repository: `/home/jack/bbs/bbs01/CampusOS`
> Current development baseline: `v0.10.0` plus the v10 module/plugin/resource separation follow-up

## 1. System Shape

CampusOS is a Go + Vue 3 modular monolith. One API process does not mean one
module. The current architecture distinguishes:

| Type | Descriptor/implementation | Lifecycle |
| --- | --- | --- |
| Core Module | `modules/core/*/module.yaml`, `internal/modules/core/*` | always-on, not installable or removable |
| Built-in Feature | `modules/features/*/module.yaml`, `internal/modules/features/*` | restart or hot-gated; disable preserves data |
| External Plugin | `data/plugins/*/plugin.yaml`, Wasm or managed process | installable, upgradable and removable through Plugin Platform |
| Resource Package | `data/resources/*/*/resource.json` | validated/imported/applied data; no business runtime |

`runtime: builtin` is parse-only migration compatibility. Plugin Manager, CLI,
package import and directory scanning reject it and reject names reserved by a
module, feature or compatibility alias.

## 2. Current Functional Baseline

| Area | Implemented baseline |
| --- | --- |
| Community | Categories/default tags, plain and rich-text threads, replies/floors, private content, unified publication/moderation/deletion state, revisions, review, takedown, resubmission, trash and purge |
| Identity/RBAC | JWT, stable Permission Code, route operations, global/category scope, custom roles, moderator assignments, authorization audits, self-escalation denial and last-admin atomic protection |
| Moderation | Always-on Core policy, category scope, pin/lock/delete-reply action limits and audit; `category-moderation` is only a compatibility alias |
| User Storage | Safe per-user paths, ownership, quota and asset Provider under `data/personal-space/<user-id>` |
| Personal Space | Owner-bound public pages, avatar history, style settings and Community `ContentQuery`; Built-in Feature |
| RichText | Draft/edit/preview/publish, image assets and sanitization; Built-in Feature using User Storage |
| Schedule | Year + spring/fall terms, first week, week/calendar views, manual editing and XLS/CSV/JSON import; Built-in Feature using User Storage |
| Appearance | Homepage, system theme, personal-space style, safe HTML/CSS, sandbox effects and CampusStyleSDK through one Built-in Feature Facade |
| External Plugin Platform | Manifest v1/v2, Wasm and managed-process runtimes, Host API/Gateway, event/UI registry, config, logs, package governance, catalog, user grants, managed records/files and signatures |
| Integrations | AI Gateway, Webhook, internal MCP-like read-only tools, Message local adapter, integration overview and platform logs |
| Frontends | Web `:3000`, Admin `:3001`, independent VitePress docs `:3002`; adaptive layout and browser regression paths |

The current system does not claim a standard MCP Server, protobuf gRPC plugin
protocol, production Discord/OneBot adapter, remote public marketplace or
production high-availability deployment.

## 3. Important Directories

```text
cmd/server/                         API entrypoint
internal/server/                    bootstrap and application assembly
internal/platform/module/           module kernel
internal/platform/feature/          authoritative Built-in Feature registry/API
modules/                            compiled module descriptors
internal/modules/core/              Identity, Community, Moderation, User Storage
internal/modules/features/          Personal Space, RichText, Schedule, Appearance and integrations
internal/plugin/                    External Plugin Platform only
data/plugins/                       External Plugin implementation only
data/plugin_data/                   External Plugin private runtime data only
data/module_data/                   Built-in Feature local mutable data
data/resources/                     runtime-free resource packages
data/personal-space/<user-id>/      User Storage files
web/ admin/ docs-site/              user, admin and documentation frontends
migrations/                         ordered PostgreSQL migrations 000001..000026
```

Current checked-in resource examples:

- `data/resources/themes/{campus-canvas,aurora-campus}`
- `data/resources/homepage-packs/campus-hero`
- `data/resources/space-style-packs/{clean-blog,kinetic-journal}`
- `data/module_data/personal-space/styles/*.space-style.json`

Current external plugin examples:

- `data/plugins/hello-wasm`
- `examples/plugins/{grpc-example,campus-welcome,v2-managed-example,schedule-helper,wasm-example}`

## 4. Database and Compatibility

- Migrations are sequential `000001` through `000026`; historical migrations
  are read-only.
- `000023` adds v10 content governance, `000025` adds authorization catalog,
  and `000026` separates historical Built-in plugin state/config from the
  External Plugin Catalog.
- `000026` keeps existing Feature Store rows authoritative, merges old
  Homepage/Web Theme config into Appearance, soft-deletes historical Built-in
  plugin rows and adds `platform.feature.*` permissions.
- Old `/plugins/<builtin-alias>` single-item routes remain compatibility
  projections for one transition window. `/plugins` lists only External
  Plugin; new clients use `/features` for compiled features.
- The file-layout migration is conflict-safe and reversible:

```bash
./scripts/migrate-v10-module-plugin-layout.sh check
./scripts/migrate-v10-module-plugin-layout.sh apply backups/v10-layout-before
./scripts/migrate-v10-module-plugin-layout.sh rollback backups/v10-layout-before
```

## 5. Admin and User Entry Points

| Capability | Entry |
| --- | --- |
| Built-in Feature state/config | Admin `/features` |
| Appearance and Resource Packages | Admin `/appearance`; user `/appearance` and `/space/settings` |
| External Plugin management | Admin `/plugins` |
| External Plugin catalog/governance | Admin `/plugin-center` |
| Unified extension inventory | Admin `/extensions` |
| Roles and permissions | Admin `/permissions` |
| Category moderators | Admin `/moderators` |
| Personal schedule | User `/schedule`; Admin sees feature state, not private course data |

Runtime Manifest returns Built-in UI contributions in `modules[]` and External
Plugin contributions in `plugins[]`.

## 6. Required Validation

Always start with `git status --short` and never revert unrelated dirty-tree
changes. For a full v10 acceptance run:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
GOCACHE=/tmp/campusos-go-cache go run ./cmd/campusos-contracts --check
make architecture-check
make data-governance-check
make database-check
make docs-links
make readme-check
make version-check
make generated-files-check
pnpm --dir web lint
pnpm --dir web format:check
pnpm --dir web build
pnpm --dir admin test:component
pnpm --dir admin build
pnpm --dir docs-site build
pnpm --dir sdk/typescript build
git diff --check
```

Release closure additionally uses:

```bash
STOP_EXISTING=true make dev-all
RUN_RESTORE_DRILL=true RUN_BROWSER_SMOKE=true make release-check
```

Do not weaken permission, audit, path, quota, content-safety, checksum or
database checks to make validation pass.

## 7. Read Before Editing

1. `README.md`
2. `docs/项目计划v10/00-v10版本计划书.md`
3. Latest file under `docs/进度/v0.10-dev/`
4. `docs/architecture/v10模块插件资源物理隔离.md`
5. `docs/architecture/v10当前系统清点与治理.md`
6. The relevant module descriptor and README under `modules/`

When behavior, paths, migrations, APIs or manifests change, update contracts,
the current architecture/help docs, v10 plan evidence and a new progress file.
