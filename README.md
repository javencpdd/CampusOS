<p align="center">
  <h1 align="center">CampusOS</h1>
  <p align="center">
    <strong>🚀 下一代校园社区引擎 — 事件驱动、AI Native 的社区操作系统</strong>
  </p>
  <p align="center">
    <a href="./docs/项目计划v1/00-项目介绍.md">项目介绍</a> •
    <a href="./docs/项目计划v1/01-总体架构.md">架构设计</a> •
    <a href="./docs/项目计划v2/02-插件系统v2.md">插件系统</a> •
    <a href="./docs/项目计划v1/06-API设计.md">API 文档</a> •
    <a href="./docs/项目计划v2/04-开发路线图v2.md">路线图</a>
  </p>
</p>

---

## ✨ 项目简介

CampusOS 是一个基于 **Go + Vue 3** 构建的面向高校场景的开放式社区平台。它不是传统论坛，而是一个**社区操作系统 (Community OS)**——官方论坛仅仅是运行在这个操作系统上的一个基础 App。

**核心理念：**
- 🔮 **万物皆事件** — 所有核心行为通过 Event Bus 广播，插件通过监听事件改变系统行为
- 🏗️ **双运行时插件架构** — Wasm 负责轻量扩展，gRPC 负责重型集成
- 🤖 **AI 一等公民** — 原生支持 MCP 协议与统一大模型接口
- 🔒 **默认安全** — 严苛的插件权限清单与沙箱隔离

---

## 📊 项目进度

| 版本 | 状态 | 核心交付 |
|:---|:---|:---|
| v0.1.0 ~ v0.1.8 | ✅ 已完成 | 基础功能：Go 后端骨架 + PostgreSQL 11张表 + Vue 3 前端 + JWT 认证 + 版块管理 + 帖子回复 + Event Bus (NATS+Memory) + 插件系统框架 |
| v0.1.9 | ✅ 已完成 | 管理后台初始页面 |
| v0.2.0 ~ v0.2.1 | ✅ 已完成 | Host API 完整实现 + 角色表迁移 + 插件系统增强（依赖注入重构） |
| v0.2.2 | ✅ 已完成 | plugins/api_keys 数据库表 + 插件仓储 + API Key 认证中间件 + 缓存层 |
| v0.2.3 | ✅ 已完成 | **前端双项目架构拆分** + 管理后台完整 UI（仪表盘/用户管理/帖子管理/版块管理/插件管理/事件日志） |
| v0.2.4+ | 📋 计划中 | RBAC 权限前端集成 + 帖子审核队列 + Redis 缓存接入 |

详细进度请参考 [docs/进度](./docs/进度/) 目录和 [docs/releases](./docs/releases/) 目录。

---

## 🏗️ 架构总览

```
┌─────────────────┐     ┌─────────────────┐
│   用户前台       │     │   管理后台       │
│   web/          │     │   admin/        │
│   :3000         │     │   :3001         │
│                 │     │                 │
│  · 首页         │     │  · 仪表盘       │
│  · 登录/注册    │     │  · 用户管理     │
│  · 帖子列表/详情│     │  · 帖子管理     │
│  · 发帖         │     │  · 版块管理     │
└────────┬────────┘     └────────┬────────┘
         │                       │
         └───────────┬───────────┘
                     │
         ┌───────────▼───────────┐
         │     后端 API           │
         │     Go + Gin           │
         │     :8080              │
         │                       │
         │  · 36 个 RESTful 端点 │
         │  · JWT 认证           │
         │  · RBAC 权限          │
         │  · 事件总线            │
         │  · 插件系统            │
         └───────────────────────┘
```

---

## 🛠️ 技术栈

| 层级 | 技术 |
|:---|:---|
| **用户前台** | Vue 3.4+ / TypeScript 5.3+ / Vite 5 / Element Plus 2（按需引入） / Pinia 2 |
| **管理后台** | Vue 3.4+ / TypeScript 5.3+ / Vite 5 / Element Plus 2（全量引入） / Pinia 2 |
| **后端** | Go 1.22+ / Gin 1.9+ / pgx 5 |
| **数据库** | PostgreSQL 16+ / Redis 7+ |
| **消息队列** | NATS 2.x (JetStream) + 内存回退模式 |
| **插件运行时** | gRPC-Go（已实现）/ Wazero Wasm（计划中） |
| **认证** | JWT (Access Token + Refresh Token) / API Key |
| **容器化** | Docker / Docker Compose |
| **可观测性** | Prometheus（已配置） / Grafana / Jaeger（计划中） |

---

## 🚀 快速开始

### 环境要求

- Go 1.22+
- Node.js 20 LTS+（前端开发需要）
- pnpm 8.x+（前端包管理器）
- Docker & Docker Compose（基础设施需要）

### 1. 启动基础设施

使用 Docker Compose 一键启动 PostgreSQL、Redis、NATS：

```bash
cd CampusOS
docker compose up -d
```
关闭所有docker容器
sudo ./sh/stop_docker.sh

启动后的服务：

| 容器 | 端口 | 说明 |
|:---|:---|:---|
| `campusos-postgres` | `5432` | PostgreSQL 数据库 |
| `campusos-redis` | `6379` | Redis 缓存 |
| `campusos-nats` | `4222` / `8222` | NATS 消息服务器 / 监控页面 |

### 2. 数据库迁移

```bash
# 方式一：使用 Makefile
make migrate-up

# 方式二：手动执行
PGPASSWORD=campusos_dev psql -h localhost -U campusos -d campusos -f migrations/000001_init_schema.up.sql
PGPASSWORD=campusos_dev psql -h localhost -U campusos -d campusos -f migrations/000002_add_roles.up.sql
PGPASSWORD=campusos_dev psql -h localhost -U campusos -d campusos -f migrations/000003_add_plugins.up.sql
```

### 3. 启动后端

```bash
# 方式一：使用 Makefile
make run

# 方式二：直接运行
go run cmd/server/main.go

# 方式三：开发模式（热重载，需要安装 air）
make dev
```

启动后输出：
```
✅ PostgreSQL 连接成功
✅ 已注册 6 个默认事件订阅（含插件分发）
🚀 CampusOS API 监听 0.0.0.0:8080
📋 API 端点总数: 36
🔌 已加载 1 个插件
```

### 4. 启动用户前台（端口 3000）

```bash
cd web
pnpm install    # 首次需要安装依赖
pnpm dev
```

### 5. 启动管理后台（端口 3001）

```bash
cd admin
pnpm install    # 首次需要安装依赖
pnpm dev
```

---

## 🌐 服务访问地址

启动所有服务后，可通过以下地址访问：

| 服务 | 地址 | 启动方式 | 说明 |
|:---|:---|:---|:---|
| **API Server** | http://localhost:8080/api/v1 | `go run cmd/server/main.go` | Go 后端 API，36 个 RESTful 端点 |
| **用户前台** | http://localhost:3000 | `cd web && pnpm dev` | Vue 3 用户界面（首页/登录/注册/帖子列表/详情/发帖） |
| **管理后台** | http://localhost:3001 | `cd admin && pnpm dev` | Vue 3 管理界面（仪表盘/用户/帖子/版块/插件/事件管理） |
| **NATS 监控** | http://localhost:8222 | `docker compose up -d` | NATS 消息服务器监控页面 |
| **PostgreSQL** | `localhost:5432` | `docker compose up -d` | 数据库连接：`psql -h localhost -U campusos -d campusos`（密码：campusos_dev） |
| **Redis** | `localhost:6379` | `docker compose up -d` | 缓存服务，可通过 `redis-cli -h localhost` 连接 |

---

## 📡 API 快速测试

```bash
# 健康检查
curl http://localhost:8080/api/v1/health | jq .

# 注册用户
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"demo","nickname":"Demo","email":"demo@example.com","password":"123456"}' | jq .

# 登录获取 Token
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"demo@example.com","password":"123456"}' | jq -r '.data.access_token')

# 获取当前用户
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/auth/me | jq .

# 创建帖子
curl -X POST http://localhost:8080/api/v1/threads \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Hello CampusOS","content":"这是第一篇帖子","category_id":"1"}' | jq .

# 查看帖子列表
curl http://localhost:8080/api/v1/threads | jq .

# 查看事件历史
curl http://localhost:8080/api/v1/events | jq .

# 查看插件列表（需要 admin 权限）
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/plugins | jq .
```

---

## 🔧 Makefile 命令

| 命令 | 说明 |
|:---|:---|
| `make build` | 编译后端二进制到 `bin/campusos-server` |
| `make run` | 编译并启动后端 |
| `make dev` | 开发模式（热重载，需要 air） |
| `make test` | 运行所有测试 |
| `make test-coverage` | 生成测试覆盖率报告 |
| `make lint` | 运行代码检查（需要 golangci-lint） |
| `make clean` | 清理构建产物 |
| `make migrate-up` | 执行数据库迁移 |
| `make migrate-down` | 回滚数据库迁移 |
| `make docker-up` | 启动 PostgreSQL + Redis + NATS 容器 |
| `make docker-down` | 停止所有容器 |

---

## 📁 项目结构

```
CampusOS/
├── cmd/server/                 # 后端入口
├── internal/
│   ├── core/identity/          # 身份服务（用户/角色/权限）
│   ├── community/              # 社区应用（帖子/版块/回复）
│   ├── plugin/                 # 插件系统（Manager/gRPC Runtime/Host API）
│   └── server/                 # 路由注册 + 依赖注入
├── pkg/
│   ├── auth/                   # JWT + bcrypt
│   ├── cache/                  # 缓存层（Memory + Noop + Redis 占位）
│   ├── config/                 # 配置管理
│   ├── database/               # PostgreSQL 连接池（pgx）
│   ├── eventbus/               # 事件总线（NATS + 内存回退）
│   ├── middleware/              # HTTP 中间件（CORS/TraceID/JWT/权限/API Key）
│   └── response/               # 统一响应体
├── api/proto/                  # Protobuf 定义
├── migrations/                 # 数据库迁移脚本（3 个迁移）
├── examples/plugins/           # 示例插件
├── web/                        # 用户前台（Vue 3 + TypeScript，端口 3000）
│   └── src/
│       ├── api/                #   用户 API 封装
│       ├── router/             #   路由配置（6 个用户路由）
│       ├── stores/             #   Pinia 状态管理
│       └── views/              #   页面（首页/登录/注册/帖子列表/详情/发帖）
├── admin/                      # 管理后台（Vue 3 + TypeScript，端口 3001）
│   └── src/
│       ├── api/                #   管理 API 封装
│       ├── router/             #   路由配置（6 个管理路由 + 权限守卫）
│       ├── stores/             #   Pinia 状态管理（独立认证存储）
│       ├── components/         #   布局组件（AdminLayout）
│       └── views/              #   页面（仪表盘/用户/帖子/版块/插件/事件管理）
├── docs/                       # 项目文档
│   ├── 项目计划v1/             #   v1 设计文档（14 个文档）
│   ├── 项目计划v2/             #   v2 修订文档（8 个文档）
│   ├── 进度/                   #   版本发布说明
│   └── releases/               #   版本发布记录
├── deploy/prometheus/          # Prometheus 配置
├── docker-compose.yml          # 基础设施容器编排
├── Makefile                    # 构建/运行/测试命令
├── go.mod                      # Go 依赖管理
└── README.md
```

---

## 📖 文档

| 文档 | 说明 |
|:---|:---|
| [项目介绍](./docs/项目计划v1/00-项目介绍.md) | 项目背景、定位、核心理念 |
| [总体架构](./docs/项目计划v1/01-总体架构.md) | 六层架构设计、技术栈版本明细 |
| [核心设计](./docs/项目计划v1/02-核心设计.md) | Identity / Permission / Storage / Audit / Notification |
| [插件系统 v2](./docs/项目计划v2/02-插件系统v2.md) | 双运行时、Manifest、Host API、三层权限 |
| [权限系统 v2](./docs/项目计划v2/03-权限系统v2.md) | RBAC 角色系统、权限矩阵 |
| [API 设计](./docs/项目计划v1/06-API设计.md) | RESTful API 规范、统一响应体、错误码 |
| [数据库设计](./docs/项目计划v1/07-数据库设计.md) | PostgreSQL 表结构、索引、迁移规范 |
| [开发规范](./docs/项目计划v1/08-开发规范.md) | 编码规范、Git 工作流、Commit 规范 |
| [前端双项目架构](./docs/项目计划v2/06-前端双项目架构.md) | 用户前台 + 管理后台独立部署方案 |
| [API 端点清单](./docs/项目计划v2/07-API端点清单.md) | 全部 API 端点详细列表 |
| [开发路线图 v2](./docs/项目计划v2/04-开发路线图v2.md) | v0.1 → v1.0 演进计划 |
| [v1→v2 变更说明](./docs/项目计划v2/05-v1到v2变更说明.md) | 项目计划修订原因和变更对照 |

---

## 🤝 参与贡献

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交改动 (`git commit -m 'feat: add amazing feature'`)
4. 推送分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

## 📄 许可证

本项目采用 [MIT License](./LICENSE) 开源协议。