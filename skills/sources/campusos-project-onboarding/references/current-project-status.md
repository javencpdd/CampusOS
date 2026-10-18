# CampusOS Current Project Status

> Snapshot date: 2026-10-18 16:56 (Asia/Shanghai); V12-03c full bounded host-contract exit
> Repository: active CampusOS workspace (Windows or Linux)
> Current release baseline: `v0.14.0`
> Latest implementation stage: `v1.2-dev` (V12-02a/02d/02b/02c completed in their planned scope; stages03a/03b/03c fully accepted in their planned scope; earliest incomplete stage04)
> Migrations: immutable v1.1 baseline plus forward migrations `000001` through `000028`; next append-only version is `000029`

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
The static Admin architecture view covers28 migrations,112 business tables and two executor tables.
Runtime development branding remains `v1.1.0-dev`; bounded v1.2 phase work is not a v1.2 release.
Historical03b snapshot (2026-10-18 05:06 Asia/Shanghai): G0/G1,01a/01b,02a/02d/02b/02c,03a and03b have passed their scoped exits. Full03b now has durable signed bytes/catalog, separate global/user installation, version grants/consents/configuration, monotonic generations/leases and atomic Operation/audit/Outbox; production legacy controls are retired. Current27/next28,111 business tables plus2 executor tables. Native29 top-level/117 PASS events, full target33 top-level/126 events with no required target FAIL/SKIP/race; PostgreSQL restart,103 old tables/sequences,12 host CAS and actual cmd/server current Admin/User HTTP passed, alongside13 common gates. See v1.2.116-dev.md and v12-03b-exit-linux.json. The earliest incomplete stage is03c. Desired enable stays pending/runtime_unavailable in production; Dispatcher/publication, resource ACL, UI Host and real runners remain03c/04/05/06. Old .115 and G1/03a evidence are historical and unchanged.

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
| Plugin platform | v5 signed packages, durable separate installations/Grant/Consent/config/Operation, bounded managed dispatcher/publication/credentials and independent safe metadata delivery; production legacy authority retired; actual UI/Runner remain04/05/06 |
| Operations | AI Gateway, Webhook, MCP-like read-only tools, Message Local, platform logs, low-cardinality metrics, Prometheus boundary and Admin architecture view |
| Delivery | Native development plus Docker development; separate API/Web/Admin/Docs production images; aggregate and per-component Compose; backup/restore and migration |
| Database | 112 application tables plus two checksum/lock metadata tables across migrations `000001`–`000028`; no default users or credentials; old `000001-000049` test chain requires an explicit development/test reset |

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

Historical03c first slice (2026-10-18 11:49 Asia/Shanghai): host-only typed contribution directory/selection is implemented and directed race passed; native Linux/owned PG16 target passed with7 top-level/24 PASS events and no required FAIL/SKIP/race; all8 common gates passed. Production port `plugin.v5.contributions` derives current signed-package and03b installation/grant/owner/ready-generation facts, refuses conflicts/unsupported versions and stale bindings, and closes on startup reconciliation failure. Discovery is bounded to20 pages of50 families. No Dispatcher, runtime publication, feature/resource ACL invocation, cancellation/drain or event delivery yet; no built-in AI/RAG dependency. See v1.2.117-dev.md and v12-03c-directory-linux.json.03c remains earliest incomplete stage.

Historical03c Chat slice (2026-10-18 12:32 Asia/Shanghai): strict prompt-only host-to-contribution ai.chat Port is implemented with current User/owner/consumer Feature and contribution binding checks before/after IO, bounded bytes/deadline/process-local concurrency, and zero response on revoked/stale facts. Directed race passed; native Linux/owned PG target passed (13 top-level/62 PASS events including directory regression, no required FAIL/SKIP/race); all8 common gates passed. A compiled consumer must bind its real Feature and exact-generation Transport; production binds neither and remains runtime_unavailable. AI Gateway has no switchable FeatureID; do not invent one or make built-in AI/RAG depend on plugins. No host resource access/public context, full bidirectional envelope, plugin-to-Host credentials/capabilities, managed publication, drain/cancel or events yet. See v1.2.118-dev.md and v12-03c-chat-dispatch-linux.json.

Current03c completed host-contract exit (2026-10-18 16:56 Asia/Shanghai): all three typed extension calls, opaque short-lived instance credentials, typed Host capability/grant/current Core public-resource checks, publication withdrawal and cooperative cancel/drain are accepted. Production has no runner or fake ready observation; built-in AI/RAG stays independent. Preview/Knowledge consumers are bounded contract adapters, with actual UI/Knowledge bindings still04/09. Migration28 adds only the independent metadata queue: committed Outbox proof, whitelisted references/state/revision, no body/Prompt/Secret/raw payload; separate worker IO, fenced leases, five attempts/dead letters, nonempty down with exclusive writer lock. Memory audit rollback and concurrent claim/ACK fencing are verified. Current28/next29,112 business plus2 executor tables;111 old tables and11 old sequences preserved. Native/assembly/PG/restart 59 required top-level/265 PASS events, no required FAIL/SKIP/race,13 common gates passed. See v1.2.119-dev.md, v12-03c-exit-linux.json and scripts/v12-03c-exit-drill.sh. Earliest incomplete stage04 has satisfied03c/01b dependencies but was not started. Old G1/.116/.117/.118 evidence remains historical and unchanged. No shared reset, commit or push.
