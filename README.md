# CampusOS

CampusOS 是一个基于 Go + Vue 3 的校园社区系统，当前仓库已经从基础论坛演进到“社区核心 + 插件运行时 + 个人主页 + 管理后台集成中心 + 低风险外部集成”的开发基线。

截至当前代码与文档状态，`v0.5-dev` 任务已经完成到 `docs/进度/v0.5-dev/v0.5.12-dev.md`。README 只记录已经落地或当前可验证的能力；真实 IM 平台适配、标准 MCP 协议适配器、完整插件市场、完整 Docker 产品化封装、原生 Windows 实机兼容和 AI 内容审核插件仍属于后续或暂缓事项。

## 当前状态

| 模块 | 状态 | 说明 |
| --- | --- | --- |
| 用户前台 `web/` | 已实现并持续增强 | 默认端口 `3000`，覆盖注册、登录、首页插件配置、安全 HTML 首页片段、帖子列表、帖子详情、发帖、默认标签、回帖、个人主页、头像上传和风格包操作 |
| 管理后台 `admin/` | 已实现并持续增强 | 默认端口 `3001`，包含用户、帖子、版块默认标签、插件配置、AI、事件、平台日志、集成中心、Webhook、MCP 工具和 Message local 测试入口 |
| 后端 API | 已实现 | Go + Gin + pgx，提供认证、RBAC、社区、个人主页、插件、AI、Webhook、MCP-like 工具、Message、平台日志和 Metrics 接口 |
| 数据库迁移 | 已实现 | `scripts/migrate.sh` / `scripts/migrate.ps1` 自动扫描 `migrations/*.up.sql`，通过 `schema_migrations` 记录执行状态 |
| Docker 依赖服务 | 已实现 | PostgreSQL、Redis、NATS、pgAdmin 由 Docker Compose 提供 |
| 一键开发启动 | 已实现 | `make dev-all` 调用 `scripts/start-dev.sh`，可启动依赖服务、迁移、后端、用户前台和管理后台 |
| 插件 Runtime | 已实现基础闭环 | 支持 gRPC Runtime、Wasm Runtime(wazero)、Built-in Runtime、事件分发、Host API、插件日志和插件 KV |
| 插件包治理 | 已实现基础闭环 | 支持 `campusosctl plugin init/inspect/pack/install`、后台导入导出、配置表单、预检、checksum 和包大小记录 |
| 个人主页/风格包/个人空间文件 | 已实现核心闭环 | 支持公开个人主页、内容同步、风格包导入导出、受限 HTML 风格、预览、应用、回滚、恢复默认、头像上传和默认 10MB 本地个人空间 |
| AI Gateway | 已实现最小闭环 | OpenAI-compatible Provider、配置、限流和调用日志已存在；AI 内容审核插件暂缓 |
| Webhook | 已实现基础闭环 | 支持 endpoint、事件订阅、HMAC 签名、测试投递、失败记录和后台入口 |
| MCP-like 工具层 | 已实现受控实验闭环 | 当前是 CampusOS 内部 API 形态的只读工具层；标准 MCP 协议适配器仍是后续任务 |
| Message/IM | 已实现本地协议闭环 | 已有 CampusOS Message 协议和 local adapter；Discord、NapCat、AstrBot 等真实适配器后续再做 |
| CI/CD | 已配置 | CI 覆盖 Ubuntu 后端测试、迁移和前端构建；Deploy workflow 可构建发布包并通过 SSH 部署 |
| Windows 兼容 | 暂不作为主路径 | 当前推荐 Ubuntu 22.04+ 或 WSL2 + Docker Desktop；原生 Windows 实机完整部署暂缓 |

## 版本回顾

| 阶段 | 已完成重点 |
| --- | --- |
| v0.1 | 建立 Go 后端骨架、PostgreSQL schema、JWT 认证、用户/版块/帖子/回复基础能力、事件总线、插件框架雏形和 Vue 用户前台 |
| v0.2 | 拆分用户前台和管理后台，补充角色权限、插件表、API Key、缓存层、管理后台 UI、CI/CD 和 PR 模板 |
| v0.3-dev | 完成 Wasm Runtime、Host API 权限校验、插件日志、SDK/CLI 雏形、插件打包规范和工程稳定化 |
| v0.4-dev | 完成 AI Gateway、插件包导入导出、个人主页、风格包 P0 闭环，并修复数据库、迁移、登录、pgAdmin、版块创建等 UI 测试问题 |
| v0.5-dev | 完成管理后台集成中心、个人主页/风格包运营化、插件包治理、Webhook、MCP-like 只读工具、Message local adapter、Metrics、备份和运行文档收口 |

详细记录：

| 文档 | 说明 |
| --- | --- |
| [v0.3-dev 计划书](./docs/项目计划v3/02-v0.3-dev计划书.md) | v0.3-dev 总体计划 |
| [v4 实现状态与后续规划总结](./docs/项目计划v4/02-v4实现状态与后续规划总结.md) | v4 完成度和后续拆分判断 |
| [v5 版本计划书](./docs/项目计划v5/00-v5版本计划书.md) | v0.5-dev 规划和边界 |
| [v0.5-dev 进度目录](./docs/进度/v0.5-dev/) | v0.5.0 到 v0.5.12 实施记录 |

## 架构概览

```text
┌─────────────────────┐      ┌─────────────────────┐
│ web 用户前台          │      │ admin 管理后台        │
│ Vue 3 / Vite         │      │ Vue 3 / Vite         │
│ http://localhost:3000│      │ http://localhost:3001│
└──────────┬──────────┘      └──────────┬──────────┘
           │                            │
           └────────────┬───────────────┘
                        │ HTTP / JSON
┌───────────────────────▼────────────────────────┐
│ CampusOS API Server                             │
│ Go / Gin / pgx                                  │
│ http://localhost:8080/api/v1                    │
│                                                 │
│ Auth / RBAC / Community / Space / Plugin / AI   │
│ Webhook / MCP-like Tools / Message / Metrics    │
└───────────────┬──────────────┬──────────────────┘
                │              │
                │              │ Host API
                │              │ http://127.0.0.1:18080/api/host/{Method}
                │              │
┌───────────────▼──────┐   ┌───▼──────────────────┐
│ PostgreSQL / Redis   │   │ Plugin Runtime        │
│ NATS / pgAdmin       │   │ gRPC / Wasm(wazero)   │
│ Docker Compose       │   │ data/plugins/*        │
└──────────────────────┘   └──────────────────────┘
```

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 后端 | Go `1.25.0`、Gin、pgx、JWT、wazero、modernc SQLite |
| 用户前台 | Vue 3、TypeScript、Vite、Pinia、Element Plus、Axios |
| 管理后台 | Vue 3、TypeScript、Vite、Pinia、Element Plus、Axios |
| 数据库 | PostgreSQL 16 |
| 缓存 | Redis 7 |
| 消息 | NATS 2，启用 JetStream |
| 插件 | gRPC Runtime、Wasm Runtime(wazero)、Built-in Runtime、Host API、Go SDK、campusosctl |
| 工程化 | Docker Compose、Makefile、GitHub Actions、pnpm |

## 本地环境要求

推荐优先使用 Ubuntu 22.04+。Windows 用户建议使用 WSL2 + Docker Desktop。

| 工具 | 建议版本 | 用途 |
| --- | --- | --- |
| Go | 按 `go.mod`，当前为 `1.25.0` | 后端、CLI、SDK、测试 |
| Node.js | 22 LTS | 前端开发和构建；CI 当前使用 Node 22 |
| pnpm | 8.x | 前端依赖管理 |
| Docker / Docker Compose | 当前稳定版 | 启动 PostgreSQL、Redis、NATS、pgAdmin |
| PostgreSQL client `psql` | 可选 | `migrate.sh` 可自动回退到 `docker exec`，但安装后调试更方便 |
| jq | 可选 | API smoke 和命令行调试 |

## 快速开始

### 1. 准备环境变量

```bash
cp .env.example .env
```

默认关键配置：

```text
SERVER_PORT=8080
HOST_API_ENABLED=true
HOST_API_ADDR=127.0.0.1:18080
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5432/campusos?sslmode=disable
DB_HOST=localhost
DB_PORT=5432
POSTGRES_PORT=5432
REDIS_ADDR=localhost:6379
NATS_URL=nats://localhost:4222
PLUGINS_DIR=data/plugins
PLUGIN_DATA_DIR=data/plugin_data
AUTH_PASSWORD_HASH_ENABLED=true
AI_ENABLED=false
```

如果宿主机 `5432` 已被其他 PostgreSQL 占用，推荐让 Docker PostgreSQL 映射到 `5433`，并同时修改 `.env`：

```env
POSTGRES_PORT=5433
DB_PORT=5433
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5433/campusos?sslmode=disable
```

`.env.example` 只是模板，不会自动生效；实际启动时读取的是 `.env` 和当前 shell 环境变量。说明见 [关于 env 文件配置](./docs/help/系统设计相关/关于env文件配置.md)。

如果本地已有旧 `.env`，请确认 `PLUGINS_DIR` 和 `PLUGIN_DATA_DIR` 已同步为 `data/plugins`、`data/plugin_data`；否则后端会继续扫描旧插件目录。

### 2. 一键启动开发环境

```bash
make dev-all
```

该命令会调用 `scripts/start-dev.sh`：

1. 启动 Docker 依赖服务。
2. 执行数据库迁移。
3. 启动后端 API。
4. 启动用户前台。
5. 启动管理后台。

默认访问地址：

```text
用户前台：http://localhost:3000
管理后台：http://localhost:3001
后端 API：http://localhost:8080/api/v1
```

常用启动参数：

```bash
STOP_EXISTING=true make dev-all     # 自动停止占用 8080/3000/3001 的旧进程
SKIP_INFRA=true make dev-all        # 跳过 Docker 服务启动
SKIP_MIGRATE=true make dev-all      # 跳过 migration
```

日志目录：

```text
.campusos/logs/api.log
.campusos/logs/web.log
.campusos/logs/admin.log
```

一键启动脚本说明见 [一键启动前后端脚本说明](./docs/help/系统设计相关/一键启动前后端脚本说明.md)。

### 3. 手动启动方式

如果需要分开启动各服务，可以按下面顺序执行。

启动依赖服务：

```bash
make docker-up
```

执行迁移：

```bash
make migrate-up
make migrate-status
```

启动后端：

```bash
make run
```

启动用户前台：

```bash
cd web
pnpm install
pnpm dev
```

启动管理后台：

```bash
cd admin
pnpm install
pnpm dev
```

## 服务地址与默认账号

| 服务 | 地址 | 说明 |
| --- | --- | --- |
| API Server | `http://localhost:8080/api/v1` | 后端 REST API |
| Host API | `http://127.0.0.1:18080/api/host/{Method}` | 插件调用宿主能力，默认仅绑定本机回环地址 |
| 用户前台 | `http://localhost:3000` | `web/` |
| 管理后台 | `http://localhost:3001` | `admin/` |
| PostgreSQL | `localhost:${POSTGRES_PORT:-5432}` | 用户 `campusos`，数据库 `campusos`，本地密码 `campusos_dev` |
| Redis | `localhost:${REDIS_PORT:-6379}` | 本地缓存服务 |
| NATS | `localhost:${NATS_CLIENT_PORT:-4222}` | NATS client 端口 |
| NATS Monitor | `http://localhost:${NATS_MONITOR_PORT:-8222}` | NATS 监控页 |
| pgAdmin | `http://localhost:${PGADMIN_PORT:-5050}` | 数据库管理页面 |

默认账号：

| 服务 | 账号 | 密码 |
| --- | --- | --- |
| CampusOS 管理员 | `admin@campusos.local` | `Admin@123456` |
| PostgreSQL | `campusos` | `campusos_dev` |
| pgAdmin | `admin@campusos.dev` | `pgadmin123` |

pgAdmin 运行在容器内时，连接 Docker PostgreSQL 应使用：

```text
Host: postgres
Port: 5432
Database: campusos
Username: campusos
Password: campusos_dev
```

数据库连接差异和 pgAdmin 说明见 [数据库管理指南](./docs/help/系统设计相关/数据库管理指南.md)。

## 数据库迁移

迁移文件位于 `migrations/`。当前迁移清单：

| 版本 | 名称 | 说明 |
| --- | --- | --- |
| `000001` | `init_schema` | 初始化用户、账号、会话、版块、帖子、回复、通知、审计等基础表 |
| `000002` | `add_roles` | 角色、权限和用户角色关系 |
| `000003` | `add_plugins` | 插件、插件 API Key、插件事件等基础表 |
| `000004` | `seed_admin` | 默认管理员和默认版块 |
| `000005` | `plugin_schema_alignment` | 插件 schema 对齐 |
| `000006` | `add_ai_call_logs` | AI 调用日志 |
| `000007` | `add_user_spaces` | 用户个人主页 |
| `000008` | `add_user_space_contents` | 个人主页内容同步 |
| `000009` | `add_user_space_styles` | 个人主页风格包 |
| `000010` | `fix_admin_seed_password` | 修复管理员种子密码哈希 |
| `000011` | `v05_operational_features` | v0.5 运营化字段、风格快照、插件 checksum、Webhook、MCP audit、Message 表 |
| `000012` | `category_default_tags` | 版块默认标签，用于发帖时自动合并版块标签和用户自定义标签 |

常用命令：

```bash
make migrate-up
make migrate-down
make migrate-reset
make migrate-status
```

`scripts/migrate.sh` 会：

| 机制 | 说明 |
| --- | --- |
| 配置来源 | 优先读取 `.env`，否则使用脚本默认值 |
| psql 模式 | `PSQL_MODE=auto` 时优先使用宿主机 `psql`，找不到时回退到 `docker exec campusos-postgres psql` |
| 文件发现 | `up` 扫描 `migrations/*.up.sql`，`down` 扫描 `migrations/*.down.sql` 并逆序执行 |
| 状态记录 | 通过 `schema_migrations(version, name, applied_at)` 记录已执行版本 |
| 幂等跳过 | 已执行的 up migration 会跳过，不重复执行 |

迁移教程见 [make migrate-up 教程](./docs/help/系统设计相关/make-migrate-up教程.md)。

## 核心功能

### 社区

| 能力 | 状态 |
| --- | --- |
| 注册、登录、JWT 会话 | 已实现 |
| 用户资料、用户列表 | 已实现 |
| 版块列表、创建、更新、删除 | 已实现，支持每个版块配置默认标签 |
| 帖子列表、详情、创建、更新、删除 | 已实现，发帖时自动合并版块默认标签和用户自定义标签 |
| 管理员置顶、锁定、解锁帖子 | 已实现 |
| 回帖、引用回复、楼层号 | 已实现 |
| 浏览数、回复数同步 | 已实现 |
| 事件日志 | 已实现基础查询 |

### 个人主页和风格包

| 能力 | 状态 |
| --- | --- |
| 公开个人主页 `/u/:username` | 已实现 |
| 发帖同步到个人主页内容 | 已实现 |
| 用户侧主页设置 | 已实现 |
| 风格包导入、导出、校验、预览、应用 | 已实现 |
| 受限 HTML 风格编辑、检测、示例生成和应用 | 已实现，禁止脚本、事件处理器和不安全 URL |
| 风格应用前快照、回滚、恢复默认 | 已实现 |
| 管理员禁用/启用个人主页 | 已实现 |
| 个人空间文件默认插件配置 | 已实现，`personal-space` 默认每用户 10MB 本地空间 |
| 头像上传和源文件保留 | 已实现，头像存入个人空间并默认保留最近 3 个源文件 |
| 示例风格包 | 已提供，位于 `data/plugins/personal-space/styles/` |
| 任意 JavaScript 个人主页代码 | 未开放，当前仅支持后端检测通过的受限 HTML 子集 |

说明文档：

| 文档 | 说明 |
| --- | --- |
| [个人主页 Space 说明](./docs/help/插件相关/个人主页Space说明.md) | 个人主页能力和数据同步 |
| [个人主页风格包说明](./docs/help/插件相关/个人主页风格包说明.md) | 风格包结构和使用 |

### 插件系统

示例插件：

```text
data/plugins/hello-plugin
data/plugins/hello-wasm
data/plugins/personal-space
data/plugins/homepage-customizer
```

当前插件能力：

| 能力 | 状态 |
| --- | --- |
| 插件 manifest | 已实现 |
| gRPC Runtime 框架 | 已实现 |
| Wasm Runtime(wazero) | 已实现 |
| Built-in Runtime | 已实现，用于内置默认插件元数据和静态资产 |
| Host API 权限校验 | 已实现 |
| 插件日志 | 已实现 |
| 插件 KV 存储 | 已实现，默认 SQLite-backed |
| Admin 插件配置表单 | 已实现，按 `config_schema` 渲染并写回插件配置 |
| 插件包导入/导出 | 已实现 |
| 插件包预检、checksum、包大小 | 已实现 |
| 插件签名、插件市场 | 未完成，后续专项 |

Host API 默认监听：

```text
http://127.0.0.1:18080/api/host/{Method}
```

插件调用时需要带上插件身份 header：

```text
X-CampusOS-Plugin: <plugin-name>
```

当前 Host API 方法包括：

| 方法 | 说明 |
| --- | --- |
| `GetUser` | 读取用户信息 |
| `GetThread` | 读取主题信息 |
| `GetReply` | 读取回复信息 |
| `QueryThreads` | 查询主题列表 |
| `PublishEvent` | 发布事件 |
| `SendNotification` | 发送通知 |
| `GetConfig` / `SetConfig` | 读取和更新插件配置 |
| `CheckPermission` | 宿主 RBAC 检查 |
| `Log` | 写入插件日志 |
| `StorageGet` / `StorageSet` / `StorageDelete` | 插件 KV 存储 |

插件文档：

| 文档 | 说明 |
| --- | --- |
| [插件保存位置与当前插件作用汇总](./docs/help/插件相关/插件保存位置与当前插件作用汇总.md) | 插件目录和现有插件作用 |
| [标准插件包导入导出要求](./docs/help/插件相关/标准插件包导入导出要求.md) | 插件包格式 |
| [插件配置 Schema 说明](./docs/help/插件相关/插件配置Schema说明.md) | manifest 配置 schema |
| [hello-wasm README](./data/plugins/hello-wasm/README.md) | Wasm 插件示例 |
| [personal-space README](./data/plugins/personal-space/README.md) | 内置个人主页、个人空间文件和默认风格包 |
| [homepage-customizer README](./data/plugins/homepage-customizer/README.md) | 内置用户前台首页配置插件 |
| [Go SDK README](./sdk/go/README.md) | Go 插件 SDK |

### campusosctl CLI

CLI 入口位于 `cmd/campusosctl`。

创建插件脚手架：

```bash
go run ./cmd/campusosctl plugin init hello-demo --runtime wasm --dir /tmp/hello-demo
```

检查插件 manifest：

```bash
go run ./cmd/campusosctl plugin inspect data/plugins/hello-wasm
```

打包插件：

```bash
go run ./cmd/campusosctl plugin pack data/plugins/hello-wasm \
  --out /tmp/hello-wasm.campusos-plugin.tar.gz
```

安装插件包：

```bash
go run ./cmd/campusosctl plugin install /tmp/hello-wasm.campusos-plugin.tar.gz \
  --dir data/plugins
```

### AI Gateway

AI Gateway 当前是最小可用闭环：

| 能力 | 状态 |
| --- | --- |
| OpenAI-compatible Provider | 已实现 |
| base URL、model、timeout、限流配置 | 已实现 |
| 调用日志 | 已实现 |
| 管理接口和后台入口 | 已实现基础能力 |
| AI 内容审核插件 | 暂缓，不纳入当前已完成能力 |

默认 `.env.example` 中 `AI_ENABLED=false`，需要配置 Provider 和 API Key 后才会调用外部 AI 服务。说明见 [AI-Gateway 说明](./docs/help/系统设计相关/AI-Gateway说明.md)。

### v0.5 集成能力

| 能力 | 状态 | 当前边界 |
| --- | --- | --- |
| 集成中心 | 已实现 | 管理后台 `/integrations` 汇总 AI、插件、个人主页、风格包、数据库、Webhook、MCP、Message 和 metrics |
| Webhook | 已实现基础闭环 | 支持事件订阅、HMAC 签名、测试投递和投递记录；Dead Letter 和 secret 轮换后续增强 |
| MCP-like 工具层 | 已实现受控实验闭环 | 当前通过管理 API 调用只读工具；标准 MCP 协议适配器后续再做 |
| Message 协议 | 已实现本地闭环 | 只包含 local adapter 和 `ping -> pong` 验证；真实 IM 平台适配后置 |
| Metrics | 已实现最小集 | API 请求、状态码、错误、路由计数和外部集成调用计数 |
| 平台日志 | 已实现开发期只读闭环 | 管理后台 `/platform-logs` 可实时查看 `.campusos/logs/api.log`、`web.log`、`admin.log` |
| 备份恢复文档 | 已补齐 | PostgreSQL、插件数据、上传文件、配置和风格包纳入说明 |

说明见 [v0.5 集成中心与低风险集成指南](./docs/help/系统设计相关/v0.5集成中心与低风险集成指南.md)。

## API 快速测试

健康检查：

```bash
curl http://localhost:8080/api/v1/health
```

注册用户：

```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"demo","nickname":"Demo","email":"demo@example.com","password":"123456"}'
```

登录并保存 token：

```bash
TOKEN="$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"demo@example.com","password":"123456"}' | jq -r '.data.access_token')"
```

获取当前用户：

```bash
curl -H "Authorization: Bearer ${TOKEN}" \
  http://localhost:8080/api/v1/auth/me
```

获取默认版块 ID：

```bash
CATEGORY_ID="$(curl -s http://localhost:8080/api/v1/categories | jq -r '.data[0].id')"
```

创建帖子并保存帖子 ID：

```bash
THREAD_ID="$(curl -s -X POST http://localhost:8080/api/v1/threads \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"title\":\"Hello CampusOS\",\"content\":\"第一篇测试帖子\",\"category_id\":\"${CATEGORY_ID}\"}" | jq -r '.data.id')"
```

发表回复：

```bash
curl -X POST "http://localhost:8080/api/v1/threads/${THREAD_ID}/posts" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"content":"第一条回复"}'
```

查看帖子详情和回复列表：

```bash
curl "http://localhost:8080/api/v1/threads/${THREAD_ID}"
curl "http://localhost:8080/api/v1/threads/${THREAD_ID}/posts"
```

管理员登录：

```bash
ADMIN_TOKEN="$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@campusos.local","password":"Admin@123456"}' | jq -r '.data.access_token')"
```

查看集成中心概览：

```bash
curl -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  http://localhost:8080/api/v1/integrations/overview
```

## 常用命令

| 命令 | 说明 |
| --- | --- |
| `make dev-all` | 一键启动 Docker 依赖、迁移、后端、用户前台和管理后台 |
| `make build` | 编译后端到 `bin/campusos-server` |
| `make run` | 编译并启动后端 |
| `make dev` | 使用 `air` 启动后端热重载开发模式 |
| `make test` | 执行 `go test ./... -v -count=1` |
| `make test-coverage` | 生成 Go 覆盖率报告 |
| `make lint` | 执行 `golangci-lint run ./...` |
| `make clean` | 清理构建和覆盖率产物 |
| `make migrate-up` | 执行所有未应用的 up migration |
| `make migrate-down` | 按逆序执行 down migration |
| `make migrate-reset` | 先 down 再 up |
| `make migrate-status` | 查看 `schema_migrations` 状态 |
| `make docker-up` | 启动 PostgreSQL、Redis、NATS、pgAdmin |
| `make docker-infra-up` | `make docker-up` 的别名 |
| `make docker-tools-up` | 启动 pgAdmin |
| `make docker-down` | 停止 Docker Compose 服务 |
| `make web-dev` | 进入 `web/` 启动前台开发服务 |
| `make web-build` | 构建 `web/` |
| `cd admin && pnpm dev` | 启动管理后台开发服务 |
| `cd admin && pnpm build` | 构建管理后台 |

## 测试与构建

后端测试：

```bash
GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
```

Docker Compose 配置检查：

```bash
docker compose config -q
```

迁移状态：

```bash
make migrate-status
```

用户前台构建：

```bash
cd web
pnpm build
```

管理后台构建：

```bash
cd admin
pnpm build
```

README 本地链接检查：

```bash
python3 skills/campusos-readme-update/scripts/check_readme_links.py README.md
```

本次 README 更新验证结果，日期为 2026-07-08：

| 命令 | 结果 |
| --- | --- |
| `git diff --check -- README.md` | 通过 |
| `python3 skills/campusos-readme-update/scripts/check_readme_links.py README.md` | 通过 |
| `GOCACHE=/tmp/campusos-go-cache go test ./... -count=1` | 通过 |
| `docker compose config -q` | 通过 |
| `make migrate-up && make migrate-status` | 通过，`000001` 到 `000012` 均已应用 |
| `cd web && pnpm build` | 通过，有 Vite chunk size 警告 |
| `cd admin && pnpm build` | 通过，有 Vite chunk size 警告 |

## CI/CD

当前 workflow：

| 文件 | 说明 |
| --- | --- |
| `.github/workflows/ci_test.yml` | CI：Ubuntu 后端测试、数据库迁移、后端构建、`web` 和 `admin` 前端构建 |
| `.github/workflows/deploy.yml` | CD：tag `v*` 或手动触发时构建发布包，并通过 SSH 部署到远程服务器 |

注意：

| 项目 | 当前状态 |
| --- | --- |
| Windows Go CI | workflow 中保留过设计，但当前 `windows-go` job 被注释，不能视为当前启用 |
| Deploy secrets | 需要在 GitHub Secrets 配置 `HOST`、`USERNAME`、`SSH_KEY`，可选 `SSH_PORT` |
| Deploy variables | 推荐在 GitHub Variables 配置 `DEPLOY_PATH` 和 `DEPLOY_RESTART_COMMAND` |

说明文档：

| 文档 | 说明 |
| --- | --- |
| [GitHub Actions CI-CD 使用说明](<./docs/help/github使用相关/GitHub Actions CI-CD使用说明.md>) | CI/CD 配置与变量 |
| [PR 模板与 CI 自测说明](./docs/help/github使用相关/PR模板与CI自测说明.md) | PR 模板和本地自测 |

## 项目结构

```text
CampusOS/
├── admin/                         # 管理后台 Vue 应用
├── cmd/
│   ├── campusosctl/               # 插件开发 CLI
│   └── server/                    # 后端服务入口
├── docs/                          # 计划、进度、帮助文档
├── data/
│   ├── plugins/                   # 已安装插件和内置默认插件
│   ├── plugin_data/               # 插件运行期 KV 数据
│   ├── images/                    # 本地图片、头像和上传资源目录
│   ├── dist/                      # 本地发布产物预留目录
│   ├── config/                    # 本地运行配置预留目录
│   └── skills/                    # 本地 runtime/imported skills 预留目录
├── internal/
│   ├── ai/                        # AI Gateway
│   ├── community/                 # 版块、主题、回复等社区领域
│   ├── core/identity/             # 用户、角色、权限
│   ├── integration/               # 管理后台集成中心聚合接口
│   ├── mcp/                       # MCP-like 只读工具层和审计
│   ├── message/                   # CampusOS Message 协议和 local adapter
│   ├── platformlog/               # 管理后台平台日志读取和 SSE 输出
│   ├── plugin/                    # 插件 Manager、Runtime、Host API、仓储
│   ├── space/                     # 个人主页、内容同步、风格包
│   ├── webhook/                   # Webhook endpoint、投递和记录
│   └── server/                    # 路由、依赖装配、服务启动
├── migrations/                    # SQL migration
├── pkg/                           # auth/cache/config/database/eventbus/idgen/middleware/response/observability
├── scripts/                       # dev/docker/migration/hash/start 脚本
├── sdk/go/                        # Go 插件 SDK
├── skills/                        # 项目内 Codex workflow skills
├── web/                           # 用户前台 Vue 应用
├── docker-compose.yml             # 本地基础设施
├── Makefile                       # 常用命令入口
└── README.md
```

## 跨平台说明

当前推荐路线：

| 场景 | 建议 |
| --- | --- |
| Ubuntu 22.04+ 本地开发 | 推荐。当前脚本、Docker Compose、迁移和验证主要按 Linux 工作流维护 |
| Windows + WSL2 + Docker Desktop | 推荐给 Windows 用户，开发体验更接近 Ubuntu |
| 原生 Windows + Docker Desktop + PowerShell | 暂不作为主路径；已有 `.ps1` 脚本，但缺少完整实机验证 |
| 完整 Docker 产品化封装 | 暂缓；当前只维护开发期 Docker Compose 依赖服务 |

Windows 的主要难点不在 Go 或 pnpm 本身，而在 Docker Desktop 网络、端口映射、PowerShell 脚本、`psql` PATH、路径权限和卷挂载差异。当前不建议同时把主线开发目标扩大为 Ubuntu 与原生 Windows 完整兼容，应先在 Ubuntu/WSL2 路径稳定后再做专项。

## 当前限制与后续重点

| 项目 | 当前限制 | 后续方向 |
| --- | --- | --- |
| MCP-like 工具层 | 目前是内部 API 形态，标准 MCP 协议适配器未完成 | 后续增加标准协议适配、服务 token、安全文档 |
| Message/IM | 只有 local adapter，本地验证 `ping -> pong` | 后续以插件或 adapter 接入 Discord、NapCat、AstrBot 等真实平台 |
| Webhook | 基础投递、签名、记录已完成 | 后续增加 Dead Letter、secret 轮换、投递详情页和重放 |
| 插件包治理 | 已有预检和 checksum | 后续增加签名、版本回滚、插件市场和审核流程 |
| 个人主页 | 已支持风格包、受限 HTML 风格、回滚、头像上传和默认本地空间 | 后续再评估更强编辑器、个人云盘接入；任意 JS 主页暂不开放 |
| 首页自定义 | 已支持 `homepage-customizer` 结构化配置和受限 HTML 片段 | 后续如开放任意 HTML/JS，需要先补审核、CSP、版本回滚和更完整的隔离机制 |
| AI Gateway | 最小闭环已完成 | AI 内容审核插件暂缓，等待人工复核和样本评估体系 |
| 部署 | CD 可构建发布包并 SSH 部署 | 后续补充 systemd、反向代理、TLS、备份演练和回滚策略 |
| 原生 Windows | 未完成实机验证 | 后续作为兼容性专项，不作为当前主线 |

## 重要文档索引

| 文档 | 说明 |
| --- | --- |
| [v1 项目介绍](./docs/项目计划v1/00-项目介绍.md) | 早期项目定位 |
| [v2 项目概览](./docs/项目计划v2/00-项目概览与v2修订说明.md) | v2 架构和能力 |
| [v0.3-dev 计划书](./docs/项目计划v3/02-v0.3-dev计划书.md) | v0.3-dev 插件运行时计划 |
| [v4 版本计划书](./docs/项目计划v4/00-v4版本计划书.md) | v4 原计划 |
| [v4 实现状态与后续规划总结](./docs/项目计划v4/02-v4实现状态与后续规划总结.md) | v4 完成度和后续拆分 |
| [v5 版本计划书](./docs/项目计划v5/00-v5版本计划书.md) | v0.5-dev 计划 |
| [v0.5-dev 进度目录](./docs/进度/v0.5-dev/) | v0.5-dev 实施记录 |
| [数据库管理指南](./docs/help/系统设计相关/数据库管理指南.md) | Docker PostgreSQL、pgAdmin、端口和数据一致性说明 |
| [PostgreSQL 双实例数据不一致说明](./docs/help/系统设计相关/PostgreSQL双实例数据不一致说明.md) | 宿主机 PG 与 Docker PG 混用说明 |
| [环境配置与 Host API 端口说明](./docs/help/系统设计相关/环境配置与HostAPI端口说明.md) | `.env`、Host API 和端口 |
| [账号认证与管理员登录说明](./docs/help/系统设计相关/账号认证与管理员登录说明.md) | 管理员账号、密码哈希和调试模式 |
| [备份恢复说明](./docs/help/系统设计相关/备份恢复说明.md) | PostgreSQL、插件数据、上传文件和配置备份 |
| [CampusOS dev 流程 Skill 使用说明](./docs/help/skills相关/CampusOS-dev流程Skill使用说明.md) | 通用开发 workflow skill |
| [CampusOS README 更新 Skill 使用说明](./docs/help/skills相关/CampusOS-README更新Skill使用说明.md) | README 更新 workflow skill |
| [ID 策略说明](./docs/help/github使用相关/ID策略说明.md) | ID 生成和迁移策略 |

## License

MIT
