# CampusOS Current Project Status

> Snapshot date: 2026-07-12
> Repository: `/home/jack/bbs/bbs01/CampusOS`
> Primary branch in recent work: `djw-update`

## 1. Current Baseline

CampusOS is a Go + Vue 3 campus community system. The project has moved beyond a basic forum into a platform baseline:

```text
community core
  + plugin runtime / Host API
  + user personal spaces and style packages
  + admin integration center
  + low-risk integrations: Webhook, MCP-like read-only tools, Message local adapter
```

Current README and progress docs state that v0.5 completed through `docs/进度/v0.5-dev/v0.5.36-dev.md`; v6 is complete, and the active development baseline is `v0.7.2-dev`. The v7 modular-monolith migration has completed the revised plan, frozen the `v0.6.26-dev` compatibility baseline, and accepted the Module Kernel with optional lifecycle interfaces. EventBus and Plugin Platform initialization use dependency-ordered lifecycle management while existing APIs and plugin contracts remain compatible.

The next planned stage is A2a infrastructure Bootstrap extraction. Follow `docs/项目计划v7/00-v7版本计划书.md`; do not present partially implemented A2a or later stages as accepted without the required v0.7 progress evidence.

## 2. Implemented Areas

| Area | Current state |
| --- | --- |
| User frontend `web/` | Registration, login, configurable homepage, thread list/detail, plain-thread create/edit/delete/private visibility, richtext articles, replies, owner-bound personal spaces, personal schedule, full-Web administrator themes, per-user local theme selection, and sandbox style effects |
| Admin frontend `admin/` | Users, threads/private status filters, richtext article governance, categories/default tags, system/user plugin lifecycle, config/import/export/logs/risk precheck, external official-docs/GitHub resource links, migration-synchronized data architecture visualization, events, platform logs, AI, integration center, Webhook, MCP tools, Message local test |
| Official docs frontend `docs-site/` | Independent VitePress site for project introduction, development deployment, configuration, release boundaries, current HTTP API, plugin creation, manifest, package import, lifecycle, and data layout |
| Backend API | Go + Gin + pgx APIs for auth, RBAC, category-scoped moderation, community, private-thread visibility, controlled richtext articles, spaces, personal schedule, plugins, AI, Webhook, MCP-like tools, Message, platform logs, metrics |
| Database | PostgreSQL migrations `000001` through `000018`; migration state recorded in `schema_migrations`; `make database-check` validates data and the core schema contract |
| RBAC role management | `member` is implicit, `guest` is anonymous-only, global extra roles are idempotent, and moderator grants are category-only with server-side scope checks |
| Category moderation | Built-in system plugin, admin assignment/config page, per-user multi-category scopes, user-side pin/lock/delete-reply controls, audit, cross-category denial, restart-based plugin lifecycle, and hot-updated action switches |
| Docker services | PostgreSQL, Redis, NATS, pgAdmin through Docker Compose |
| One-click dev startup | `make dev-all` -> `scripts/start-dev.sh`, starting API, Web, Admin, and docs frontend |
| Plugin runtime | gRPC runtime framework, Wasm runtime through wazero, built-in runtime, Host API, plugin logs, plugin KV, system-level restart lifecycle, and user-level hot loading/reloading |
| Plugin package governance | `campusosctl plugin init/inspect/pack/install`, admin import/export/config, precheck, checksum, package size, risk grading, version comparison, import audit, and manual rollback docs |
| Controlled richtext articles | Built-in `controlled-richtext-article` plugin for image-text drafts, edit, image upload, HTML sanitization, preview, publish, details, author operations, and admin governance; images live at `data/personal-space/<user_id>/img/richtext/` and share the personal-space quota |
| Personal spaces | Public user pages, thread/content sync, JSON style import/export/preview/apply, standard folder/zip page style packs, source-folder style-pack list/apply, safe custom HTML/CSS snippets, rollback, restore default, and per-user local storage at `data/personal-space/<user_id>/`; gated by `personal-space` plugin status |
| Personal schedule | Built-in `personal-schedule` plugin for independent year+spring/fall semester JSON schedules, saved-term selection/new term creation, first-week setup, week timetable and past/future calendar browsing, manual course editing, raw JSON editing, and targeted `.xls`/`.csv`/`.json` import; data is stored below `file/schedule/terms/<year>-<semester>.json` with `file/schedule/index.json` for the active term |
| Homepage customizer | Built-in `homepage-customizer` plugin controls the user homepage hero, category quick filter tags, safe custom HTML/CSS snippets, standard folder/zip homepage style packs, admin source-folder style-pack selection, and one-step rollback through plugin config |
| Web theme system | Built-in system `web-theme` plugin lists administrator-provided `target:web` packages; users select locally per account, CSS is root-scoped, JS runs in an isolated Worker/iframe, and CampusStyleSDK only proxies declared read-only capabilities with ownership/consent checks |
| Platform logs | Admin-only fixed-source SSE log reader for `.campusos/logs/api.log`, `web.log`, and `admin.log` |
| AI Gateway | OpenAI-compatible provider, config, rate limiting, call logs; AI content moderation plugin is deferred |
| Webhook | Endpoint management, event subscriptions, HMAC signature, test delivery, delivery records |
| MCP-like tools | Internal admin API shape for read-only tools and audit; not yet a full standard MCP protocol server |
| Message/IM | CampusOS Message model and local adapter; real Discord/NapCat/AstrBot adapters are deferred |
| Observability | Minimal API and integration metrics |
| CI/CD | CI and deploy workflows exist; current CI is Ubuntu-focused and frontend build focused |

## 3. Version Recap

| Stage | Summary |
| --- | --- |
| v0.1 | Go backend skeleton, PostgreSQL schema, JWT auth, users/categories/threads/posts, event bus, plugin early shape, user frontend |
| v0.2 | User/admin frontend split, RBAC tables, plugin tables, API key, cache layer, admin UI, CI/CD, PR template |
| v0.3-dev | Wasm runtime, Host API permission checks, plugin logs, SDK/CLI early version, plugin packaging rules, engineering stabilization |
| v0.4-dev | AI Gateway, plugin import/export, personal spaces, style packages, and UI/database/login migration fixes |
| v0.5-dev | Integration center, personal space operations, per-user personal-space file storage/plugin gate, personal schedule plugin with term/calendar browsing, richtext images in personal space, safe HTML/CSS snippets, page style-pack folder/zip standard, homepage customizer/rollback, controlled richtext article plugin/admin governance, ordinary plain-thread edit/delete/private visibility, category default tags, platform logs, system/user plugin lifecycle, plugin governance/config/risk precheck, Webhook, MCP-like read-only tools, Message local adapter, smoke scripts, metrics, backup docs |
| v0.6-dev | Complete through v0.6.24: P0/P1 contracts, permission/database integrity, SDK/tooling/recovery plus the P0-Highest lifecycle, Extension Gateway, dynamic UI runtime, v2 style packs and unified release gates. Standard external MCP/Message adapters and production HA were explicitly deferred. |

## 4. Current Migrations

| Version | Name | Purpose |
| --- | --- | --- |
| `000001` | `init_schema` | Initial identity, community, notification, audit, config tables |
| `000002` | `add_roles` | Roles, permissions, user roles |
| `000003` | `add_plugins` | Plugin tables and plugin API/event structures |
| `000004` | `seed_admin` | Default admin and default category |
| `000005` | `plugin_schema_alignment` | Plugin schema alignment |
| `000006` | `add_ai_call_logs` | AI call logs |
| `000007` | `add_user_spaces` | User personal spaces |
| `000008` | `add_user_space_contents` | Personal space content sync |
| `000009` | `add_user_space_styles` | Personal space style packages |
| `000010` | `fix_admin_seed_password` | Default admin password hash correction |
| `000011` | `v05_operational_features` | v0.5 operational fields, style snapshots, plugin checksum, Webhook, MCP audit, Message tables |
| `000012` | `category_default_tags` | Category default tags for automatic tag merging during thread creation |
| `000013` | `controlled_richtext_article` | Richtext article content and image asset tables |
| `000014` | `role_assignment_permissions` | Repair role assignment IDs/global uniqueness and add `role:read`/`assign`/`revoke` |
| `000015` | `category_moderation_scope` | Remove legacy global moderator grants, enforce scope shape, and add thread lock permission |
| `000016` | `v06_core_integrity` | Add first validated core foreign keys, status/counter checks, and lifecycle indexes |
| `000017` | `v06_admin_permission_split` | Replace unrelated `role:manage` checks with explicit admin-domain permissions |
| `000018` | `plugin_ui_runtime` | Persist plugin backend/frontend/health states and UI revision |

## 5. Important Directories

| Path | Purpose |
| --- | --- |
| `cmd/server/` | API server entrypoint |
| `cmd/campusosctl/` | Plugin CLI |
| `internal/community/` | Categories, threads, replies, events |
| `internal/core/identity/` | Users, accounts, roles, permissions |
| `internal/moderation/` | Category moderator assignments, scoped actions, plugin gate, handlers, and audit |
| `internal/plugin/` | Plugin manager, runtime, repository, package logic |
| `internal/plugin/builtin/` | Built-in plugin runtime for bundled feature metadata and static assets |
| `internal/plugin/hostapi/` | Host API bridge for plugins |
| `internal/plugin/wasm/` | Wasm runtime |
| `internal/space/` | Personal space, content sync, style packages |
| `internal/stylepack/` | `page-style-pack.v1` folder/zip loader, validator, standard source package support, example zip builder |
| `internal/webtheme/` | Public catalog and runtime package boundary for administrator-provided full-Web themes |
| `internal/ai/` | AI Gateway |
| `internal/integration/` | Admin integration overview |
| `internal/webhook/` | Webhook service and handlers |
| `internal/mcp/` | MCP-like internal read-only tool layer |
| `internal/message/` | Message protocol and local adapter |
| `internal/platformlog/` | Admin-only platform log sources and SSE streaming |
| `internal/richtext/` | Controlled richtext article plugin service, sanitizer, asset store, handlers |
| `internal/schedule/` | Personal schedule plugin service, file storage, `.xls`/`.csv`/`.json` import, handlers |
| `scripts/smoke/` | v0.5 API smoke plus v0.6 authenticated Chrome workflow for user/admin/permission regression |
| `pkg/observability/` | Minimal metrics |
| `web/src/` | User frontend |
| `admin/src/` | Admin frontend |
| `docs-site/` | Independent official documentation frontend for users and plugin developers |
| `data/` | Default local data root for plugins, plugin data, images, dist, config, and local skills |
| `data/personal-space/<user_id>/` | Local user data root; `file/`, `img/`, `excel/`, `word/`, and `pdf/` classify stored data by purpose or extension; richtext images use `img/richtext/` |
| `data/plugins/` | Installed and built-in plugins |
| `data/plugin_data/personal-space/styles/` | Built-in personal space JSON style data |
| `data/plugin_data/personal-space/style-packs/` | Personal-space source packages (`clean-blog`, advanced `kinetic-journal`) |
| `data/plugin_data/homepage-customizer/style-packs/` | Built-in/source-folder homepage page style packs (`campus-hero`) |
| `data/plugin_data/web-theme/style-packs/` | Administrator-provided full user-Web themes (`campus-canvas`) |
| `data/plugins/homepage-customizer/` | Built-in user homepage configuration plugin |
| `data/plugins/controlled-richtext-article/` | Built-in controlled richtext article plugin manifest and README |
| `data/plugins/personal-schedule/` | Built-in personal schedule plugin manifest and README |
| `data/plugins/category-moderation/` | Built-in category moderation plugin manifest and README |
| `data/plugins/web-theme/` | Built-in full-Web theme provider manifest and README |
| `sdk/go/` | Go plugin SDK |
| `skills/` | Project-local Codex skills |
| `skills/campusos-data-architecture-sync/` | Sync workflow and checker for migrations, system schema table, storage boundaries, and Admin `/architecture` view |

## 6. Run Commands

| Task | Command |
| --- | --- |
| One-click dev startup | `make dev-all` |
| Start Docker services | `make docker-up` |
| Stop Docker services | `make docker-down` |
| Run migrations | `make migrate-up` |
| Show migration status | `make migrate-status` |
| Run backend | `make run` |
| Run backend tests | `GOCACHE=/tmp/campusos-go-cache go test ./... -count=1` |
| Build user frontend | `cd web && pnpm build` |
| Build admin frontend | `cd admin && pnpm build` |
| Build official docs | `cd docs-site && pnpm build` |
| Run v0.5 read-only smoke | `python3 scripts/smoke/v05_smoke.py` |
| Run v0.5 write smoke | `python3 scripts/smoke/v05_smoke.py --write` |
| Create commit helper | `./sh/git_commit.sh "message"` |
| Create PR helper | `./sh/git_pr.sh -t "PR title"` |

## 7. Services and Accounts

| Service | Default address |
| --- | --- |
| User frontend | `http://localhost:3000` |
| Admin frontend | `http://localhost:3001` |
| Official docs | `http://localhost:3002` |
| API | `http://localhost:8080/api/v1` |
| Host API | `http://127.0.0.1:18080/api/host/{Method}` |
| PostgreSQL | `localhost:${POSTGRES_PORT:-5432}` |
| Redis | `localhost:${REDIS_PORT:-6379}` |
| NATS | `localhost:${NATS_CLIENT_PORT:-4222}` |
| NATS monitor | `http://localhost:${NATS_MONITOR_PORT:-8222}` |
| pgAdmin | `http://localhost:${PGADMIN_PORT:-5050}` |

| Account | Username | Password |
| --- | --- | --- |
| CampusOS admin | `admin@campusos.local` | `Admin@123456` |
| PostgreSQL | `campusos` | `campusos_dev` |
| pgAdmin | `admin@campusos.dev` | `pgadmin123` |

## 8. Documentation Map

| Document | Use |
| --- | --- |
| `README.md` | Concise current repository baseline, quick start, and documentation entry |
| `docs/README.md` | Documentation portal for operations, architecture, API, plugins, plans, and progress |
| `docs-site/` | Public-facing project, deployment, API, and plugin-development documentation frontend |
| `docs/项目计划v5/00-v5版本计划书.md` | v0.5 original plan and boundaries |
| `docs/项目计划v5/01-v5版本计划书第二版.md` | v0.5 current baseline and follow-up plan after v0.5.36-dev |
| `docs/skills/README.md` | Project Skill usage, maintenance, validation, and synchronization index |
| `docs/help/skills相关/CampusOS-数据架构同步Skill使用说明.md` | Legacy-path data architecture synchronization guide linked from the Skills index |
| `docs/项目计划v6/00-v1-v5计划审查.md` | Strict historical-plan audit: completed, partial, and deferred work |
| `docs/项目计划v6/01-v6版本计划书.md` | Planned v0.6-dev API/authorization, database integrity, plugin SDK/tooling, recovery, and acceptance work |
| `docs/help/系统设计相关/RBAC权限与版主管理说明.md` | Current roles, moderator permissions, admin assignment flow, category scope enforcement, and plugin lifecycle |
| `docs/help/系统设计相关/开发运行与验证指南.md` | Local startup, test/build commands, and contribution helper scripts |
| `docs/api/API索引.md` | Current API groups, schedule API, and contract-documentation boundary |
| `docs/help/插件相关/插件分级与生命周期说明.md` | System/user plugin scope, lifecycle, and admin operation guide |
| `docs/help/系统设计相关/接口协议适配器标准说明.md` | Discord/OneBot-style protocol adapter design |
| `docs/help/系统设计相关/v0.5回归Smoke测试说明.md` | v0.5 read-only/write smoke usage |
| `docs/help/系统设计相关/插件包治理与回滚说明.md` | Plugin package risk precheck, audit, rollback, and signature pre-research |
| `docs/进度/v0.5-dev/` | v0.5 implementation progress |
| `docs/项目计划v4/02-v4实现状态与后续规划总结.md` | v4 completion and follow-up decisions |
| `docs/help/系统设计相关/v0.5集成中心与低风险集成指南.md` | v0.5 integration help |
| `docs/help/系统设计相关/备份恢复说明.md` | Backup and restore |
| `docs/help/系统设计相关/数据库管理指南.md` | Docker PostgreSQL and pgAdmin usage |
| `docs/help/插件相关/` | Plugin and style package docs |
| `docs/help/github使用相关/` | GitHub Actions, PR, ID, PR script docs |
| `docs/skills/` | Canonical Skill usage and maintenance docs |
| `docs/help/skills相关/` | Legacy Skill docs retained until compatibility migration is complete |

## 9. Boundaries To Preserve

Do not overstate these items:

| Item | Current boundary |
| --- | --- |
| Standard MCP protocol | Not complete; current implementation is an internal MCP-like read-only tool layer |
| Real IM adapters | Not complete; only Message protocol and local adapter exist |
| Plugin marketplace | Not complete; plugin package governance exists, but signature/market/review are future work |
| AI content moderation plugin | Deferred due to model stability and review workflow concerns |
| Full Docker product packaging | Deferred; current Docker Compose mainly supports development dependencies |
| Native Windows deployment | Deferred; recommended Windows path is WSL2 + Docker Desktop |
| Arbitrary JavaScript or unsandboxed HTML user/homepage rendering | Not open; only backend-validated restricted HTML/CSS snippets and screened page style-pack files are rendered, while scripts, event handlers, unsafe URLs, unsafe paths, and unsafe CSS are rejected |

## 10. Onboarding Checklist For Agents

1. Run `git status --short` before editing.
2. Read this reference and the current `README.md`.
3. If continuing version work, read the latest file in the active `docs/进度/v0.6-dev/` directory and the latest completed v0.5 record when needed for regression context.
4. If changing behavior, inspect routes in `internal/server/server.go`.
5. If changing schema, read all related migrations and `scripts/migrate.sh`.
6. If touching frontend, inspect existing patterns in `web/src` or `admin/src`.
7. After changes, run validation commands scoped to the touched areas.
8. Update docs when behavior, setup, scripts, migrations, or version status changes.
