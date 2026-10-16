# CampusOS Current Project Status

> Snapshot date: 2026-10-16 20:34 (Asia/Shanghai); V12-03a complete exit
> Repository: active CampusOS workspace (Windows or Linux)
> Current release baseline: `v0.14.0`
> Latest implementation stage: `v1.2-dev` (V12-02a/02d/02b/02c completed in their planned scope; stage03a fully accepted;03b dependencies satisfied, not started)
> Migrations: immutable v1.1 baseline plus forward migrations `000001` through `000025`; next append-only version is `000026`

Current stage authority is `docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md`.
The accepted predecessor V12-02b exit is `docs/进度/v1.2-dev/v1.2.110-dev.md`, with the exact
144-route/eight-group resource responsibility inventory and `v12-02b-exit-linux.json` report.
At the 02b exit, 539 historical assertions plus 21 new/changed assertions were executed against that
stage's frozen source: 560 required top-level tests without FAIL/SKIP/race, 13 isolated PG16 databases, nine
000016–000024 original-data/typed-actor/non-reused-highwater preservation paths, and 17 real
cmd/server + current Admin/Web browser checks. Builds are separate from browser evidence.
User/Admin identity, private original resource reads and metadata/permission previews remain distinct.
Current Admin SPA uses its own HttpOnly refresh/CSRF, current profile and self MFA/logout. Resource
commands still reload actual facts and check their own Policy, current authority and required audit.
The static Admin architecture view covers 25 migrations, 102 business tables and two executor tables.
Runtime development branding remains `v1.1.0-dev`; bounded v1.2 phase work is not a v1.2 release.
Integration02c is complete: independent digest credentials in PG/Memory, current Admin permission/MFA/CSRF management, one-time issue, atomic rotate/revoke and exact channel/endpoint/resource whitelists. Explicit runtimev2 adds the existing Community public-thread Port while retaining frozenv1. Actual source ACLs and machine/source row locks compose until required typed audit/outbox commit; no User/Admin/Host identity fallback or plaintext persistence. Twelve routes and Admin lifecycle UI are delivered. Native30 tops without FAIL/SKIP/race,384 Go/oracle decisions,PG rollback/ABA/lock/rotation and18 real cmd/server+Admin+Chrome checks,54 Admin components and common gates passed; see v1.2.112-dev.md and v12-02c-exit-linux.json. Migration25 preserves all100 prior business tables, typed attribution and allocation highwater; nonempty identities or live permission references reject down. Knowledge producers09a/full public Knowledge MCP11,v5Dispatcher03c and Windows/whole-version release retain their own stages. Stage03a is now complete: shared full-Manifest DTO, actual host catalogs, bounded deterministic package snapshots, referenced digests, Ed25519/current publisher trust and revocation, in-process immutable release identities, primary CLI and rebuilt PDF/UI plus Echo/WASI examples. Old production discovery/adapter/CLI/import entry paths are retired; historical types remain for03b/data tests. Native11 tops/99 PASS events, CLI12 tops/15 events and SDK12 tops passed without FAIL/SKIP/race;64 Manifest checks and16 concurrent publications,23 Go/oracle decisions,43 actual CLI processes,5 actual cmd/server+owned PG16 assertions and13 common gates passed. See v1.2.114-dev.md and v12-03a-exit-linux.json. Current Host capability items are empty until03c; extension DTOs are not installed providers. The release identity index is in-process; durable catalog/install/operation/recovery is03b. UI/Bridge and Runner execution are04/05/06. The earliest incomplete stage is03b with03a/02a/02d dependencies met; no03b work started. Current25/next26 is unchanged. Earlier progress files and frozen G1 evidence remain historical;03a retirement uses an explicit overlay.

Historical v0.14 implementation audit remains in `docs/进度/v0.14-dev/` and
`docs/项目计划书v0/项目计划v0.14/01-v0.14计划逐项审查与项目回顾.md`. The v1.0 formal plan is
`docs/项目计划书v1/项目计划v1.0/00-v1.0版本计划书.md`; its clean database foundation is documented in
`01-v1.0数据库全面重构方案.md` and `docs/进度/v1.0-dev/v1.0.1-dev.md`. Do not treat the new plugin tables as proof that
AuthorizationService, Admin Grant UI, User Consent UI or Host API v3 are complete.

This file is an orientation snapshot. Before changing code, verify claims against the live worktree, generated contracts,
migrations, the latest progress evidence, and `docs/计划书总结/README.md`.

## 1. Current Shape

CampusOS is a Go and Vue 3 modular monolith with one API process and three separately built frontends:

```text
API modular monolith
├── Core Modules
├── Built-in Feature Modules
├── External Plugin Platform
└── Resource Package Repository

frontends
├── web       user application
├── admin     management application
└── docs-site public documentation
```

The four extension categories are not interchangeable:

| Category | Lifecycle | Main locations |
| --- | --- | --- |
| Core Module | always on, not installable or removable | `modules/core/`, `internal/modules/core/` |
| Built-in Feature | compiled in, restart or hot-gated, data retained when disabled | `modules/features/`, `internal/modules/features/`, `data/module_data/` |
| External Plugin | installable/upgradable/removable through governed runtimes | `data/plugins/`, `data/plugin_data/`, `internal/plugin/` |
| Resource Package | validated data with no business runtime | `data/resources/` |

## 2. Implemented Baseline

| Area | Verified baseline |
| --- | --- |
| Identity | User registration/session/MFA and independent Admin accounts, credentials, roles, sessions and MFA/recovery storage; Admin API rejects User Tokens; user roles no longer create Admin admission; remaining recovery HTTP and route MFA gates are in v1.2.44 |
| Authorization | Stable Permission Codes, route Operations, global/category scopes, custom roles, moderator bounds, last-admin protection and required audit |
| Community | Two-level categories with group aggregation, tags, text/rich/Mutual Aid/Secondhand posts, revisions, content governance, replies/floors, notifications and batch admin actions |
| Structured content | Built-in Mutual Aid and Secondhand modules on the reliable Community transaction boundary |
| Reliability | TxKernel, Outbox, lease/fencing Worker, retry, dead-letter, controlled replay, receipts, retention and failure-injection tests |
| User features | Personal Space, default 50 MB User Storage with per-user admin quota, compressed images, three-source avatar FIFO, governed Schedule, versioned Personal Documents, RichText and Appearance |
| Appearance | Themes, homepage packs and space style packs under Resource Package governance, dual PC/mobile delivery contracts and browser matrix |
| Plugin platform | Manifest v1/v2, Wasm and managed-process runtimes, Host API, Extension Gateway, managed records/files, Catalog, user Grant and package governance |
| Operations | AI Gateway, Webhook, MCP-like read-only tools, Message Local, platform logs, low-cardinality metrics, Prometheus boundary and Admin architecture view |
| Delivery | Native development plus Docker development; separate API/Web/Admin/Docs production images; aggregate and per-component Compose; backup/restore and migration |
| Database | 102 application tables plus two checksum/lock metadata tables across migrations `000001`–`000025`; no default users or credentials; old `000001-000049` test chain requires an explicit development/test reset |

## 3. Important Boundaries

Do not overstate these capabilities:

- MCP is currently an internal controlled MCP-like integration, not a complete standard MCP Server.
- Historical `runtime: grpc` is a managed process using restricted loopback HTTP Extension/Event contracts, not
  standard protobuf gRPC.
- Discord, OneBot and other real production adapters are not implemented.
- The plugin market is local and governed; there is no remote public marketplace, payment, rating or federation.
- Docker delivery is single-host. It does not include HA, automatic TLS, multi-node failover or autoscaling.
- Development evidence covers Linux `amd64` and Windows Docker Desktop Linux Containers on `amd64`; Windows production
  release and Linux `arm64` still need target-runner certification.
- Agent contracts exist, but there is no unrestricted Agent Center, code executor or host-wide Agent Runner.

## 4. Key Directories

| Path | Purpose |
| --- | --- |
| `cmd/server/`, `internal/server/` | API entry and bootstrap composition |
| `modules/` | machine-readable Core and Built-in Feature descriptors |
| `internal/modules/core/` | Identity, Community, Moderation, User Storage, AcademicTerm and other Core implementations |
| `internal/modules/features/` | Personal Space, Schedule, Personal Documents, RichText, Appearance and other Built-in Features |
| `internal/platform/` | module, feature, transaction, reliability, resource, route, runtime and version kernels |
| `internal/plugin/` | External Plugin catalog, runtime, lifecycle, Host API and package governance |
| `internal/transport/httpapi/` | owned route and HTTP contract composition |
| `web/src/modules/` | user frontend feature modules |
| `admin/src/modules/` | admin frontend feature modules |
| `docs-site/` | public VitePress documentation |
| `data/plugins/` | External Plugin implementation and manifest |
| `data/plugin_data/` | External Plugin private runtime data |
| `data/module_data/` | Built-in Feature local mutable data |
| `data/resources/` | themes, homepage/space packs, Skills, Prompt, Persona and knowledge metadata |
| `data/personal-space/<user-id>/` | user-owned files, images, schedules and authorized plugin attachments |
| `sdk/`, `examples/plugins/` | Go/TypeScript SDKs and verifiable plugin examples |

## 5. Run Modes

Native development:

```bash
cp .env.example .env
STOP_EXISTING=true make dev-all
```

First Docker development setup on Linux/WSL2/Git Bash:

```bash
./scripts/docker-dev.sh setup
# edit deploy/docker/.env.dev.local
./scripts/docker-dev.sh setup --start
```

PowerShell uses `.\scripts\docker-dev.ps1 setup` and then `.\scripts\docker-dev.ps1 setup -Start`. Daily `up` reuses
existing images; use `rebuild` only when Dockerfiles, package manifests/lockfiles, Compose build inputs, or development
container entrypoints change. Ordinary Go/Vue/VitePress source uses hot reload.

Single-host aggregate deployment:

```bash
./scripts/docker-deploy.sh init
./scripts/docker-deploy.sh up
```

Independent components use `./scripts/docker-component.sh up <infra|api|web|admin|docs>`.

| Service | Default URL |
| --- | --- |
| Web | `http://localhost:3000` |
| Admin | `http://localhost:3001` |
| Docs | `http://localhost:3002` |
| API | `http://localhost:8080/api/v1` |

## 6. Validation

Run targeted Go tests first and full tests when the implementation impact warrants them:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
```

On Windows PowerShell, put `GOCACHE` under the workspace (for example `.cache\go-build`) when the user profile cache is not
writable.

Common gates:

```bash
make readme-check
make docs-links
make architecture-check
make data-governance-check
make database-check
make v1-database-baseline-check
make docker-deploy-check
```

Frontend checks:

```bash
cd web && pnpm lint && pnpm format:check && pnpm build
cd ../admin && pnpm test:component && pnpm build
cd ../docs-site && pnpm build
```

Release/high-risk changes use the applicable database, restore and browser form of `make release-check`.

## 7. Documentation Authority

Read these before a new task:

| Need | Document |
| --- | --- |
| Root entry | `README.md` |
| Documentation portal | `docs/README.md` |
| Help lifecycle and stale-doc map | `docs/help/README.md` |
| Version evolution and plan validity | `docs/计划书总结/README.md` |
| Current candidate roadmap | `docs/计划书总结/01-当前项目规划与后续路线.md` |
| Current architecture | `docs/architecture/当前架构概览.md` |
| v0.13 plan and audit | `docs/项目计划书v0/项目计划v0.13/00-v0.13版本计划书.md`, `docs/项目计划书v0/项目计划v0.13/02-v0.13最终专业审计与后续路线.md` |
| v0.14 implementation plan | `docs/项目计划书v0/项目计划v0.14/00-v0.14版本计划书.md` |
| v1.0 formal plan and DB design | `docs/项目计划书v1/项目计划v1.0/00-v1.0版本计划书.md`, `01-v1.0数据库全面重构方案.md` |
| Current evidence | Verify the v1.2 main plan, `docs/进度/v1.2-dev/v1.2.110-dev.md`, actual source/log digests and reports in `docs/项目计划书v1/项目计划v1.2/evidence/`; do not infer completion from filenames or old summaries |
| v0.13 final baseline | `docs/项目计划书v0/项目计划v0.13/02-v0.13最终专业审计与后续路线.md` and `docs/进度/v0.13-dev/` |

Historical plan files and versioned Help snapshots remain useful for traceability, but they are not current operation
instructions. Planned capabilities must not be reported as implemented without live code and test evidence.

## 8. Onboarding Checklist

1. Run `git status --short`; preserve unrelated user changes.
2. Run `scripts/context_snapshot.sh` from this Skill.
3. Classify the task as Core, Built-in Feature, External Plugin or Resource Package.
4. Inspect the owning module, Port, migration and frontend route before editing.
5. Keep API/data compatibility unless the task explicitly authorizes a reset boundary and supplies a replacement migration/rollback path.
6. Run impact-specific checks plus full Go tests.
7. Update the canonical guide, current progress evidence and generated contracts when behavior changes.
