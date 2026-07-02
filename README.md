# CampusOS

CampusOS 是一个基于 Go + Vue 3 的校园社区系统。当前项目已经从基础论坛功能扩展到 v0.3-dev 的插件运行时阶段：后端提供社区 API、认证、RBAC、事件总线、插件管理和 Host API；前端拆分为用户前台与管理后台；基础设施主要通过 Docker Compose 启动。

本 README 记录当前代码仓库的真实状态，优先面向本地开发、测试和后续 v0.3-dev 插件系统迭代。

## 当前状态

| 模块 | 状态 | 说明 |
| --- | --- | --- |
| 用户前台 `web/` | 已实现 | Vue 3 + Vite，默认端口 `3000`，覆盖注册、登录、帖子列表、帖子详情、发帖等基础流程 |
| 管理后台 `admin/` | 已实现并持续增强 | Vue 3 + Vite，默认端口 `3001`，包含用户、帖子、版块、插件、事件、插件日志等管理入口 |
| 后端 API | 已实现 | Go + Gin + pgx，提供认证、用户、社区内容、管理和插件相关 API |
| 数据库迁移 | 已实现 | `scripts/migrate.sh` / `scripts/migrate.ps1` 自动扫描 `migrations/*.up.sql`，通过 `schema_migrations` 记录执行状态 |
| 事件总线 | 已实现 | 支持 NATS，具备内存实现用于测试和回退场景 |
| 插件 Runtime | v0.3-dev 已完成首轮 | gRPC Runtime 框架存在，Wasm Runtime 已接入 wazero，支持事件分发、超时、trap 隔离和 payload ABI |
| Host API | v0.4-dev 前置收尾已增强 | 支持读取用户/主题/回复、查询主题、发布事件、发送通知、配置持久化、权限检查、插件日志、SQLite-backed 插件 KV 等能力 |
| Go SDK | v0.4-dev 前置收尾已增强 | `sdk/go` 提供 Host API HTTP Client、事件/manifest 类型和本地测试 Harness |
| CLI | v0.4-dev 前置收尾已增强 | `cmd/campusosctl` 支持 `plugin init`、`plugin inspect`、`plugin pack`、`plugin install` |
| CI/CD | 已配置 | GitHub Actions 包含 Ubuntu 后端测试、前端构建、Windows Go 测试和 tag 部署流程 |
| Windows 兼容 | 代码级支持推进中 | 已提供 `.ps1` 脚本和 Windows Go CI；真实 Windows + Docker Desktop 实机验证尚未完成 |

## v0.1 到 v0.3-dev 更新回顾

| 阶段 | 已完成内容 |
| --- | --- |
| v0.1 | 建立 Go 后端骨架、PostgreSQL schema、JWT 认证、用户/版块/帖子/回复基础能力、NATS/Memory Event Bus、插件框架雏形、Vue 用户前台 |
| v0.2 | 拆分用户前台和管理后台，补充角色与权限表、插件表、API Key、缓存层、管理后台 UI、CI/CD 和 PR 模板等工程化能力 |
| v0.3-dev 前置稳定化 | 修复注册 ID、发帖认证、迁移脚本、文档结构、GitHub Actions、分支命名和 v0.3 工作流 |
| v0.3-dev 首轮 | 完成 Wasm Runtime、Host API 权限校验、插件日志、hello-wasm 示例、SDK/CLI 雏形、插件打包规范、Windows Go CI、Windows 脚本验证边界记录 |
| v0.4-dev 前置收尾 | 完成 `SetConfig` 持久化、SQLite-backed 插件 KV、`campusosctl plugin pack/install`、SDK 本地测试 Harness；Windows 实机验证本轮跳过 |

详细记录见：

| 文档 | 说明 |
| --- | --- |
| [`docs/项目计划v3/02-v0.3-dev计划书.md`](./docs/项目计划v3/02-v0.3-dev计划书.md) | v0.3-dev 总计划 |
| [`docs/进度/v0.3-dev/前置稳定化完成说明.md`](./docs/进度/v0.3-dev/前置稳定化完成说明.md) | v0.3-dev 前置稳定化总结 |
| [`docs/进度/v0.3-dev/02-v0.3-dev首轮任务清单.md`](./docs/进度/v0.3-dev/02-v0.3-dev首轮任务清单.md) | v0.3-dev 首轮任务清单 |
| [`docs/进度/v0.3-dev/`](./docs/进度/v0.3-dev/) | v0.3-dev 每次任务进度记录 |
| [`docs/help/`](./docs/help/) | 迁移、CI/CD、PR 模板和工作流 Skill 使用说明 |

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
│ Auth / RBAC / Community / Admin / Plugin APIs   │
│ Event Bus / Plugin Manager / Host API Bridge    │
└───────────────┬──────────────┬──────────────────┘
                │              │
                │              │ Host API
                │              │ http://127.0.0.1:18080/api/host/{Method}
                │              │
┌───────────────▼──────┐   ┌───▼──────────────────┐
│ PostgreSQL / Redis   │   │ Plugin Runtime        │
│ NATS JetStream       │   │ gRPC / Wasm(wazero)   │
│ Docker Compose       │   │ examples/plugins/*    │
└──────────────────────┘   └──────────────────────┘
```

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 后端 | Go `1.25.0`、Gin、pgx、JWT、wazero |
| 用户前台 | Vue 3、TypeScript、Vite、Pinia、Element Plus、Axios |
| 管理后台 | Vue 3、TypeScript、Vite、Pinia、Element Plus、Axios |
| 数据库 | PostgreSQL 16 |
| 缓存 | Redis 7 |
| 消息 | NATS 2，启用 JetStream |
| 插件 | gRPC Runtime、Wasm Runtime(wazero)、Host API |
| 工程化 | Docker Compose、Makefile、GitHub Actions、pnpm |

## 本地环境要求

推荐优先使用 Ubuntu 22.04 或更新版本进行本地开发。

| 工具 | 建议版本 | 用途 |
| --- | --- | --- |
| Go | 按 `go.mod`，当前为 `1.25.0` | 后端、CLI、SDK 和测试 |
| Node.js | 22 LTS | 前端构建；CI 当前使用 Node 22 |
| pnpm | 8.x | 前端依赖管理 |
| Docker / Docker Compose | 当前稳定版 | 启动 PostgreSQL、Redis、NATS、pgAdmin |
| PostgreSQL client | 16 或兼容版本 | 执行 `psql` 迁移命令 |
| jq | 可选 | 调试 API 响应 |

Windows 推荐优先使用 WSL2 + Docker Desktop，或者只在 Windows 上做 Go 代码级测试。原生 Windows 一键完整部署仍不是当前主路径，原因见本文的“跨平台说明”。

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
REDIS_ADDR=localhost:6379
NATS_URL=nats://localhost:4222
PLUGINS_DIR=examples/plugins
PLUGIN_DATA_DIR=.campusos/plugin-data
```

如果宿主机已经有 PostgreSQL 占用 `5432`，可以在 `.env` 中设置：

```env
POSTGRES_PORT=5433
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5433/campusos?sslmode=disable
```

### 2. 启动基础设施

```bash
make docker-up
```

该命令会调用 `scripts/docker-up.sh`，按端口占用情况启动 PostgreSQL、Redis、NATS 和 pgAdmin。只需要单独启动 pgAdmin 时执行：

```bash
make docker-tools-up
```

停止容器：

```bash
make docker-down
```

### 3. 执行数据库迁移

```bash
make migrate-up
make migrate-status
```

`make migrate-up` 不需要手写每个 SQL 文件。它会调用 `scripts/migrate.sh up`，自动扫描 `migrations/*.up.sql`，按文件名顺序执行尚未记录在 `schema_migrations` 表中的迁移。

当前迁移文件包含：

```text
000001_init_schema
000002_add_roles
000003_add_plugins
000004_seed_admin
000005_plugin_schema_alignment
000006_add_ai_call_logs
000007_add_user_spaces
000008_add_user_space_contents
000009_add_user_space_styles
000010_fix_admin_seed_password
```

迁移原理教程见 [`docs/help/make-migrate-up教程.md`](./docs/help/make-migrate-up教程.md)。

### 4. 启动后端

```bash
make run
```

或直接运行：

```bash
go run ./cmd/server/main.go
```

开发热重载：

```bash
make dev
```

`make dev` 需要本机已安装 `air`。

### 5. 启动用户前台

```bash
cd web
pnpm install
pnpm dev
```

默认访问地址：

```text
http://localhost:3000
```

### 6. 启动管理后台

```bash
cd admin
pnpm install
pnpm dev
```

默认访问地址：

```text
http://localhost:3001
```

## 服务地址

| 服务 | 地址 | 说明 |
| --- | --- | --- |
| API Server | `http://localhost:8080/api/v1` | 后端 REST API |
| Host API | `http://127.0.0.1:18080/api/host/{Method}` | 插件调用宿主能力，默认仅绑定本机回环地址 |
| 用户前台 | `http://localhost:3000` | `web/` |
| 管理后台 | `http://localhost:3001` | `admin/` |
| PostgreSQL | `localhost:${POSTGRES_PORT:-5432}` | 用户 `campusos`，数据库 `campusos`，本地密码 `campusos_dev` |
| Redis | `localhost:6379` | 本地缓存服务 |
| NATS | `localhost:4222` | NATS client 端口 |
| NATS Monitor | `http://localhost:8222` | NATS 监控页 |
| pgAdmin | `http://localhost:5050` | 通过 `make docker-up` 或 `make docker-tools-up` 启动 |

默认账号：

| 服务 | 账号 | 密码 |
| --- | --- | --- |
| pgAdmin | `admin@campusos.dev` | `pgadmin123` |
| CampusOS 管理员 | `admin@campusos.local` | `Admin@123456` |
| PostgreSQL | `campusos` | `campusos_dev` |

数据库说明：

| 项目 | 值 |
| --- | --- |
| 数据库引擎 | PostgreSQL 16+ |
| Go 驱动 | pgx 5 |
| 数据库名 | `campusos` |
| 用户名 | `campusos` |
| 端口 | 容器内 `5432`，宿主机默认 `${POSTGRES_PORT:-5432}` |
| 数据卷 | `campusos_postgres-data` |

## 常用命令

| 命令 | 说明 |
| --- | --- |
| `make build` | 编译后端到 `bin/campusos-server` |
| `make run` | 编译并启动后端 |
| `make dev` | 使用 `air` 启动热重载开发模式 |
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

创建帖子：

```bash
curl -X POST http://localhost:8080/api/v1/threads \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"title":"Hello CampusOS","content":"第一篇测试帖子","category_id":"1"}'
```

查看帖子列表：

```bash
curl http://localhost:8080/api/v1/threads
```

## 数据库迁移机制

迁移脚本位于 `scripts/migrate.sh` 和 `scripts/migrate.ps1`。Linux/macOS 本地通常通过 Makefile 使用：

```bash
make migrate-up
make migrate-down
make migrate-reset
make migrate-status
```

实现要点：

| 机制 | 说明 |
| --- | --- |
| 配置来源 | 优先读取 `.env`，否则使用脚本内默认值 |
| 文件发现 | `up` 扫描 `migrations/*.up.sql`，`down` 扫描 `migrations/*.down.sql` 并逆序执行 |
| 执行工具 | 使用 `psql -v ON_ERROR_STOP=1`，任一 SQL 出错即停止 |
| 状态记录 | 通过 `schema_migrations(version, name, applied_at)` 记录已执行版本 |
| 幂等跳过 | `up` 遇到已记录版本会输出 `skip`，不会重复执行 |
| reset | 先执行所有 down，再执行所有 up |

因此，不建议把所有 migration 手工写死到 Makefile。随着 `000006_*`、`000007_*` 增加，脚本扫描机制可以自动纳入新迁移，同时避免重复执行已应用版本。

## 插件系统

### 插件目录

当前示例插件位于：

```text
examples/plugins/hello-plugin
examples/plugins/hello-wasm
```

`hello-wasm` 是 v0.3-dev 的最小 Wasm 插件示例，manifest 使用 `runtime: wasm`，订阅 `thread.created` 和 `post.created`，模块入口为 `handle_event`。

### Manifest 示例

```yaml
name: hello-wasm
display_name: "Hello Wasm"
version: "0.1.0"
runtime: wasm

events:
  subscribe:
    - "thread.created"

permissions:
  api:
    - resource: "log"
      actions: ["write"]

storage:
  type: none

config:
  module: "plugin.wasm"
  entrypoint: "handle_event"
  event_timeout_ms: 1000
```

### Wasm Runtime

Wasm Runtime 基于 wazero，当前支持：

| 能力 | 状态 |
| --- | --- |
| 生命周期 | `Start` / `Stop` / `Health` |
| 事件分发 | Manager 将订阅事件发送给插件 |
| 无参数 ABI | `handle_event()` |
| Payload ABI | `handle_event(i32 ptr, i32 len)`，Runtime 写入 JSON `EventMessage` |
| 超时控制 | 读取 `config.event_timeout_ms` |
| trap 隔离 | 插件 panic/trap 不应拖垮主进程 |
| 示例验证 | `examples/plugins/hello-wasm` 和相关 Go 测试 |

### Host API

Host API 默认监听：

```text
http://127.0.0.1:18080/api/host/{Method}
```

插件调用时需要带上插件身份 header：

```text
X-CampusOS-Plugin: <plugin-name>
```

示例：

```bash
curl -X POST http://127.0.0.1:18080/api/host/GetConfig \
  -H "Content-Type: application/json" \
  -H "X-CampusOS-Plugin: hello-wasm" \
  -d '{"key":"entrypoint"}'
```

当前 Host API 主要能力：

| 方法 | 说明 |
| --- | --- |
| `GetUser` | 读取用户信息 |
| `GetThread` | 读取主题信息 |
| `GetReply` | 读取回复信息 |
| `QueryThreads` | 查询主题列表 |
| `PublishEvent` | 发布事件 |
| `SendNotification` | 发送通知 |
| `GetConfig` | 读取插件配置 |
| `SetConfig` | 更新运行期插件配置，并在 PG 模式下持久化到插件仓储 |
| `CheckPermission` | 调用宿主 RBAC 检查 |
| `Log` | 写入 `plugin_logs` |
| `StorageGet` / `StorageSet` / `StorageDelete` | 插件 KV 存储接口，默认使用 SQLite-backed 持久化目录 |

Host API 会结合插件 manifest 中声明的权限进行校验。没有声明对应资源和动作的插件调用会被拒绝。

### 插件日志

插件运行、事件处理和 Host API `Log` 可写入 `plugin_logs`。管理后台已提供插件日志查看入口，便于排查插件生命周期、事件处理结果和运行错误。

## Go SDK

Go SDK 位于 [`sdk/go`](./sdk/go/)。

当前能力：

| 能力 | 状态 |
| --- | --- |
| Event / Manifest 类型 | 已提供 |
| Host API Client | 已提供 |
| `GetUser` / `GetThread` / `GetReply` / `QueryThreads` | 已封装 |
| `PublishEvent` / `SendNotification` | 已封装 |
| `GetConfig` / `SetConfig` | 已封装 |
| `CheckPermission` / `Log` | 已封装 |
| `StorageGet` / `StorageSet` / `StorageDelete` | 已封装 |
| 本地插件测试 Harness | 已提供 |
| Wasm 编译模板 | 后续任务 |

示例：

```go
client := campusos.NewHostClient("hello-wasm")

value, found, err := client.GetConfig(ctx, "entrypoint")
if err != nil {
    return err
}
if found {
    fmt.Println(value)
}
```

默认 Host API 地址为：

```text
http://127.0.0.1:18080
```

本地插件测试可以使用 SDK Harness：

```go
harness := campusos.NewHarness("hello-wasm")
defer harness.Close()

client := harness.Client()
```

## campusosctl CLI

CLI 入口位于 [`cmd/campusosctl`](./cmd/campusosctl/)。

创建插件脚手架：

```bash
go run ./cmd/campusosctl plugin init hello-demo --runtime wasm --dir /tmp/hello-demo
```

检查插件 manifest：

```bash
go run ./cmd/campusosctl plugin inspect examples/plugins/hello-wasm
```

打包插件：

```bash
go run ./cmd/campusosctl plugin pack examples/plugins/hello-wasm \
  --out /tmp/hello-wasm.campusos-plugin.tar.gz
```

安装插件包：

```bash
go run ./cmd/campusosctl plugin install /tmp/hello-wasm.campusos-plugin.tar.gz \
  --dir examples/plugins
```

当前 CLI 已形成 `init/inspect/pack/install` 最小闭环，后续可继续扩展 `plugin test`、版本回滚和插件签名。

## 项目结构

```text
CampusOS/
├── admin/                         # 管理后台 Vue 应用
├── cmd/
│   ├── campusosctl/               # 插件开发 CLI
│   └── server/                    # 后端服务入口
├── docs/                          # 计划、进度、帮助文档
├── examples/plugins/              # 示例插件
├── internal/
│   ├── community/                 # 版块、主题、回复等社区领域
│   ├── core/identity/             # 用户、角色、权限
│   ├── plugin/                    # 插件 Manager、Runtime、Host API、仓储
│   └── server/                    # 路由、依赖装配、服务启动
├── migrations/                    # SQL migration
├── pkg/
│   ├── auth/                      # JWT、密码处理
│   ├── cache/                     # 缓存抽象和实现
│   ├── config/                    # 配置加载
│   ├── database/                  # PostgreSQL 连接
│   ├── eventbus/                  # 事件总线
│   ├── idgen/                     # ID 生成
│   ├── middleware/                # HTTP middleware
│   └── response/                  # API 响应封装
├── scripts/                       # dev/docker/migration 脚本
├── sdk/go/                        # Go 插件 SDK 雏形
├── web/                           # 用户前台 Vue 应用
├── docker-compose.yml             # 本地基础设施
├── Makefile                       # 常用命令入口
└── README.md
```

## 测试与构建

后端测试：

```bash
go test ./...
```

后端构建：

```bash
make build
```

用户前台构建：

```bash
cd web
pnpm install
pnpm build
```

管理后台构建：

```bash
cd admin
pnpm install
pnpm build
```

CI 当前覆盖：

| Job | 内容 |
| --- | --- |
| Backend Test | Ubuntu runner，启动 PostgreSQL service，执行 migration、`go test ./...` 和后端构建 |
| Frontend Build | 分别构建 `web` 和 `admin` |
| Windows Go Test | Windows runner，执行 `go mod download` 和 `go test ./...` |
| Deploy | tag `v*` 或手动触发，构建发布包并通过 SSH 部署 |

CI/CD 说明见 [`docs/help/GitHub Actions CI-CD使用说明.md`](./docs/help/GitHub%20Actions%20CI-CD使用说明.md)。

## PR 模板与自测

仓库已提供精简 PR 模板，位置：

```text
.github/PULL_REQUEST_TEMPLATE/pull_request_template.md
```

建议提交 PR 前至少执行：

```bash
git diff --check
go test ./...
```

如果修改了前端，还需要执行对应前端构建：

```bash
cd web && pnpm build
cd admin && pnpm build
```

说明文档见 [`docs/help/PR模板与CI自测说明.md`](./docs/help/PR模板与CI自测说明.md)。

## 跨平台说明

当前项目不是“完全不支持 Windows”，但也不建议现在把原生 Windows 完整部署作为主要开发路径。

更准确的结论：

| 场景 | 建议 |
| --- | --- |
| Ubuntu 22.04+ 本地开发 | 推荐。当前项目脚本、Docker Compose、迁移和测试主要按 Linux 工作流验证 |
| Windows + WSL2 + Docker Desktop | 推荐作为 Windows 用户的主要使用方式。开发体验更接近 Ubuntu |
| 原生 Windows + Docker Desktop + PowerShell | 可继续推进，但需要真实 Windows 环境验证 `.ps1` 脚本、Docker 网络、端口、psql、路径和权限 |
| GitHub Windows CI | 已有 `windows-latest` 上的 Go 测试，用于提前发现代码级跨平台问题 |

本项目本地部署中，Go 后端、CLI 和前端 Node/pnpm 运行在本机；PostgreSQL、Redis、NATS、pgAdmin 主要通过 Docker 提供。因此，Windows 的主要难点不在 Go 或 npm 本身，而在这些方面：

| 难点 | 说明 |
| --- | --- |
| Docker Desktop 行为差异 | Windows Docker Desktop、WSL2 网络、端口映射和文件共享与 Linux Docker Engine 不完全一致 |
| PowerShell 脚本实机验证 | 已提供 `.ps1`，但当前仓库工作环境无法真实运行 Windows + Docker Desktop 验证 |
| PostgreSQL client | migration 依赖 `psql`，原生 Windows 需要额外安装并配置 PATH |
| 路径和权限 | 脚本、插件目录、数据目录和 Docker volume 在 Windows 上需要单独验证 |
| 前端依赖 | Node/pnpm 可跨平台，但 lockfile、换行和 shell 命令仍需要 CI 或实机确认 |

因此当前更稳妥的路线是：

1. 先在 Ubuntu 22.04+ 或 WSL2 中完成 v0.3-dev 开发和部署流程。
2. 将后端、前端构建产物、迁移脚本和配置封装成更完整的 Docker 或发布包。
3. 再迁移到 Windows + Docker Desktop 或更高版本 Ubuntu 环境验证。
4. 如果需要强 Windows 原生支持，再补充 self-hosted Windows runner 或专门的 Windows 实机测试清单。

Windows 脚本验证限制记录见 [`docs/进度/v0.3-dev/v0.3.18-dev.md`](./docs/进度/v0.3-dev/v0.3.18-dev.md)。

## 当前限制与后续重点

| 项目 | 当前限制 | 后续方向 |
| --- | --- | --- |
| `SetConfig` | PG 模式已持久化；仍缺少后台配置 schema 和更完整审计字段 | 增加配置 schema、后台表单和更细审计 |
| 插件 KV 存储 | 已有 SQLite-backed KV；尚未提供备份/恢复命令 | 增加插件数据备份恢复说明和工具 |
| 插件打包 | CLI 已支持 pack/install；尚未支持签名、版本回滚和后台上传 | 增加签名、回滚和后台安装向导 |
| Go SDK | 已封装 Host API 常用方法并提供 Harness | 增加 Wasm/TinyGo 模板和 `plugin test` |
| Windows 实机验证 | 已有 `.ps1` 和 Windows Go CI，但未在真实 Windows + Docker Desktop 跑通 | 增加人工验证记录或 self-hosted runner |
| 生产部署 | CD 能构建发布包并 SSH 部署 | 后续补充 systemd、反向代理、TLS、备份和回滚策略 |

## 重要文档索引

| 文档 | 说明 |
| --- | --- |
| [`docs/项目计划v1/00-项目介绍.md`](./docs/项目计划v1/00-项目介绍.md) | v1 项目介绍 |
| [`docs/项目计划v2/00-项目概览与v2修订说明.md`](./docs/项目计划v2/00-项目概览与v2修订说明.md) | v2 概览 |
| [`docs/项目计划v3/02-v0.3-dev计划书.md`](./docs/项目计划v3/02-v0.3-dev计划书.md) | v0.3-dev 计划书 |
| [`docs/help/CampusOS-v0.3-dev流程Skill使用说明.md`](./docs/help/CampusOS-v0.3-dev流程Skill使用说明.md) | v0.3-dev 工作流 Skill 用法 |
| [`docs/help/make-migrate-up教程.md`](./docs/help/make-migrate-up教程.md) | migration 教程 |
| [`docs/help/GitHub Actions CI-CD使用说明.md`](./docs/help/GitHub%20Actions%20CI-CD使用说明.md) | CI/CD 帮助 |
| [`docs/help/PR模板与CI自测说明.md`](./docs/help/PR模板与CI自测说明.md) | PR 模板和本地自测说明 |
| [`docs/进度/v0.4-dev/v4前置收尾功能说明.md`](./docs/进度/v0.4-dev/v4前置收尾功能说明.md) | v4 前置收尾功能说明 |
| [`docs/help/ID策略说明.md`](./docs/help/ID策略说明.md) | ID 策略 |
| [`sdk/go/README.md`](./sdk/go/README.md) | Go SDK 说明 |
| [`examples/plugins/hello-wasm/README.md`](./examples/plugins/hello-wasm/README.md) | Wasm 插件示例 |

## License

MIT
