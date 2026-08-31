# CampusOS Current Project Status

> Snapshot date: 2026-08-31 01:32（Asia/Shanghai）
> Repository: active CampusOS workspace (Windows or Linux)
> Current release baseline: `v0.13.0`
> Active implementation stage: `v0.14-dev` (G0, AcademicTerm, Storage Object, Schedule Guard, historical adoption/reconciliation, Personal Documents MVP, shared Content Editor Core and low-cardinality operational summaries implemented; Final gates remain)
> Migrations: `000001` through `000049`

The latest implementation evidence is `docs/进度/v0.14-dev/v0.14.9-dev.md` and
`docs/项目计划v14/01-v14计划逐项审查与项目回顾.md`: they record the plan audit, batched recoverable reconciliation,
the personal-document preview safe-fallback receipt, and documentation synchronization. P0/P1 implementation evidence remains in
`v0.14.5-dev.md`; v0.14 Final gates remain unchanged.

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
| Identity | Verified email registration, Challenge/Ticket, password recovery, email binding, revocable sessions, separate admin admission, TOTP MFA and recovery |
| Authorization | Stable Permission Codes, route Operations, global/category scopes, custom roles, moderator bounds, last-admin protection and required audit |
| Community | Two-level categories with group aggregation, tags, text/rich/Mutual Aid/Secondhand posts, revisions, content governance, replies/floors, notifications and batch admin actions |
| Structured content | Built-in Mutual Aid and Secondhand modules on the reliable Community transaction boundary |
| Reliability | TxKernel, Outbox, lease/fencing Worker, retry, dead-letter, controlled replay, receipts, retention and failure-injection tests |
| User features | Personal Space, default 50 MB User Storage with per-user admin quota, compressed images, three-source avatar FIFO, governed Schedule, versioned Personal Documents, RichText and Appearance |
| Appearance | Themes, homepage packs and space style packs under Resource Package governance, dual PC/mobile delivery contracts and browser matrix |
| Plugin platform | Manifest v1/v2, Wasm and managed-process runtimes, Host API, Extension Gateway, managed records/files, Catalog, user Grant and package governance |
| Operations | AI Gateway, Webhook, MCP-like read-only tools, Message Local, platform logs, low-cardinality metrics, Prometheus boundary and Admin architecture view |
| Delivery | Native development plus Docker development; separate API/Web/Admin/Docs production images; aggregate and per-component Compose; backup/restore and migration |

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
| v13 plan and audit | `docs/项目计划v13/00-v13版本计划书.md`, `docs/项目计划v13/02-v13最终专业审计与后续路线.md` |
| v14 implementation plan | `docs/项目计划v14/00-v14版本计划书.md` |
| Current evidence | latest files in `docs/进度/v0.14-dev/` |
| v13 final baseline | `docs/项目计划v13/02-v13最终专业审计与后续路线.md` and `docs/进度/v0.13-dev/` |

Historical plan files and versioned Help snapshots remain useful for traceability, but they are not current operation
instructions. Planned capabilities must not be reported as implemented without live code and test evidence.

## 8. Onboarding Checklist

1. Run `git status --short`; preserve unrelated user changes.
2. Run `scripts/context_snapshot.sh` from this Skill.
3. Classify the task as Core, Built-in Feature, External Plugin or Resource Package.
4. Inspect the owning module, Port, migration and frontend route before editing.
5. Keep API/data compatibility unless the task provides a migration and rollback path.
6. Run impact-specific checks plus full Go tests.
7. Update the canonical guide, current progress evidence and generated contracts when behavior changes.
