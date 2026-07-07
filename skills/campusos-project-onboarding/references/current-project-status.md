# CampusOS Current Project Status

> Snapshot date: 2026-07-07
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

Current README and progress docs state that `v0.5-dev` has completed through `docs/进度/v0.5-dev/v0.5.8-dev.md`.

The next recommended work mode is regression testing, defect fixing, and careful planning for the next version stage. Do not present v0.6 or later ideas as already implemented.

## 2. Implemented Areas

| Area | Current state |
| --- | --- |
| User frontend `web/` | Registration, login, thread list/detail, create thread, replies, personal space, style packages |
| Admin frontend `admin/` | Users, threads, categories, plugins, events, AI, integration center, Webhook, MCP tools, Message local test |
| Backend API | Go + Gin + pgx APIs for auth, RBAC, community, spaces, plugins, AI, Webhook, MCP-like tools, Message, metrics |
| Database | PostgreSQL migrations `000001` through `000011`; migration state recorded in `schema_migrations` |
| Docker services | PostgreSQL, Redis, NATS, pgAdmin through Docker Compose |
| One-click dev startup | `make dev-all` -> `scripts/start-dev.sh` |
| Plugin runtime | gRPC runtime framework, Wasm runtime through wazero, built-in runtime, Host API, plugin logs, plugin KV |
| Plugin package governance | `campusosctl plugin init/inspect/pack/install`, admin import/export, precheck, checksum, package size |
| Personal spaces | Public user pages, thread/content sync, style import/export/preview/apply, rollback, restore default |
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
| v0.5-dev | Integration center, personal space operations, plugin governance, Webhook, MCP-like read-only tools, Message local adapter, metrics, backup docs |

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

## 5. Important Directories

| Path | Purpose |
| --- | --- |
| `cmd/server/` | API server entrypoint |
| `cmd/campusosctl/` | Plugin CLI |
| `internal/community/` | Categories, threads, replies, events |
| `internal/core/identity/` | Users, accounts, roles, permissions |
| `internal/plugin/` | Plugin manager, runtime, repository, package logic |
| `internal/plugin/builtin/` | Built-in plugin runtime for bundled feature metadata and static assets |
| `internal/plugin/hostapi/` | Host API bridge for plugins |
| `internal/plugin/wasm/` | Wasm runtime |
| `internal/space/` | Personal space, content sync, style packages |
| `internal/ai/` | AI Gateway |
| `internal/integration/` | Admin integration overview |
| `internal/webhook/` | Webhook service and handlers |
| `internal/mcp/` | MCP-like internal read-only tool layer |
| `internal/message/` | Message protocol and local adapter |
| `pkg/observability/` | Minimal metrics |
| `web/src/` | User frontend |
| `admin/src/` | Admin frontend |
| `data/` | Default local data root for plugins, plugin data, images, dist, config, and local skills |
| `data/plugins/` | Installed and built-in plugins |
| `data/plugins/personal-space/styles/` | Built-in personal space style packages |
| `sdk/go/` | Go plugin SDK |
| `skills/` | Project-local Codex skills |

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
| Create commit helper | `./sh/git_commit.sh "message"` |
| Create PR helper | `./sh/git_pr.sh -t "PR title"` |

## 7. Services and Accounts

| Service | Default address |
| --- | --- |
| User frontend | `http://localhost:3000` |
| Admin frontend | `http://localhost:3001` |
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
| `README.md` | Current repository overview and commands |
| `docs/项目计划v5/00-v5版本计划书.md` | v0.5 plan and boundaries |
| `docs/进度/v0.5-dev/` | v0.5 implementation progress |
| `docs/项目计划v4/02-v4实现状态与后续规划总结.md` | v4 completion and follow-up decisions |
| `docs/help/系统设计相关/v0.5集成中心与低风险集成指南.md` | v0.5 integration help |
| `docs/help/系统设计相关/备份恢复说明.md` | Backup and restore |
| `docs/help/系统设计相关/数据库管理指南.md` | Docker PostgreSQL and pgAdmin usage |
| `docs/help/插件相关/` | Plugin and style package docs |
| `docs/help/github使用相关/` | GitHub Actions, PR, ID, PR script docs |
| `docs/help/skills相关/` | Skill usage docs |

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
| Arbitrary JavaScript user homepages | Not open; needs a security model before enabling |

## 10. Onboarding Checklist For Agents

1. Run `git status --short` before editing.
2. Read this reference and the current `README.md`.
3. If continuing version work, read the latest file in `docs/进度/v0.5-dev/`.
4. If changing behavior, inspect routes in `internal/server/server.go`.
5. If changing schema, read all related migrations and `scripts/migrate.sh`.
6. If touching frontend, inspect existing patterns in `web/src` or `admin/src`.
7. After changes, run validation commands scoped to the touched areas.
8. Update docs when behavior, setup, scripts, migrations, or version status changes.
