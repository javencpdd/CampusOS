<p align="center">
  <h1 align="center">CampusOS</h1>
  <p align="center">
    <strong>🚀 下一代校园社区引擎 — 事件驱动、AI Native 的社区操作系统</strong>
  </p>
  <p align="center">
    <a href="./docs/00-项目介绍.md">项目介绍</a> •
    <a href="./docs/01-总体架构.md">架构设计</a> •
    <a href="./docs/03-插件系统.md">插件系统</a> •
    <a href="./docs/06-API设计.md">API 文档</a> •
    <a href="./docs/10-开发路线图.md">路线图</a>
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


## 项目进度

可以参考/docs/releases目录下的文档


## 🛠️ 技术栈

| 层级 | 技术 |
|:---|:---|
| **前端** | Vue 3.4+ / TypeScript 5.3+ / Vite 5 / Element Plus 2 / Pinia 2 |
| **后端** | Go 1.22+ / Gin 1.9+ / GORM 1.25+ |
| **数据库** | PostgreSQL 16+ / Redis 7+ |
| **消息队列** | NATS 2.x (JetStream) |
| **插件运行时** | Wazero (Wasm) / gRPC-Go |
| **容器化** | Docker / Docker Compose / Kubernetes + Helm |
| **可观测性** | OpenTelemetry / Prometheus / Grafana / Jaeger |

## 📁 项目结构

```
CampusOS/
├── cmd/                  # 入口文件（server / migrate）
├── internal/             # 内部包
│   ├── core/             #   核心服务（Identity / Permission / Storage / Audit / Notification）
│   ├── platform/         #   平台层（EventBus / TaskQueue / Cache）
│   ├── plugin/           #   插件运行时（Manager / Wasm / gRPC）
│   ├── community/        #   社区应用（官方论坛）
│   ├── integration/      #   第三方集成（NapCat / AstrBot）
│   └── ai/               #   AI 能力（LLM 网关 / MCP Server）
├── pkg/                  # 公共包（SDK / Middleware / CloudEvents）
├── api/                  # API 定义（OpenAPI / Protobuf）
├── web/                  # 前端代码（Vue 3）
├── migrations/           # 数据库迁移
├── deploy/               # 部署配置（Docker / Helm）
├── docs/                 # 项目文档
└── scripts/              # 脚本工具
```

## 🚀 快速开始

### 环境要求

- Go 1.22+ / Node.js 20 LTS+ / Docker & Docker Compose

### 一键启动

```bash
# 克隆项目
git clone https://github.com/your-org/CampusOS.git
cd CampusOS

# 启动基础设施
docker compose up -d

# 后端
go mod download
go run cmd/migrate/main.go up
go run cmd/server/main.go

# 前端
cd web
pnpm install
pnpm dev
```

### 访问

| 服务 | 地址 | 说明 |
|:---|:---|:---|
| **API Server** | http://localhost:8080/api/v1 | Go 后端 API 服务，提供 19 个 RESTful 端点（用户/帖子/版块/回复/事件），基于 Gin 框架，支持 JWT 认证和事件总线 |
| **Web UI** | http://localhost:3000 | Vue 3 前端界面，包含首页、登录/注册、帖子列表/详情/发帖等 6 个页面，通过 Vite 代理请求后端 API |
| **Grafana** | http://localhost:3001 | 可视化监控面板，展示 Prometheus 采集的系统指标（请求量/延迟/错误率），默认账号 admin/admin |
| **Jaeger** | http://localhost:16686 | 分布式链路追踪系统，用于追踪跨服务请求的完整调用链路，排查性能瓶颈和错误 |
| **MinIO Console** | http://localhost:9001 | 对象存储管理控制台，用于文件上传/下载管理（头像/附件/图片），兼容 S3 协议 |
| **NATS** | http://localhost:8222 | NATS 消息服务器监控页面，查看事件总线的连接状态、消息吞吐量 |
| **PostgreSQL** | localhost:5432 | 关系型数据库，存储用户/帖子/版块/回复等核心数据 |
| **Redis** | localhost:6379 | 内存缓存，用于会话缓存、热点数据缓存、限流计数 |

## 📖 文档

| 文档 | 说明 |
|:---|:---|
| [00-项目介绍](./docs/00-项目介绍.md) | 项目背景、定位、核心理念 |
| [01-总体架构](./docs/01-总体架构.md) | 六层架构设计、技术栈版本明细 |
| [02-核心设计](./docs/02-核心设计.md) | Identity / Permission / Storage / Audit / Notification |
| [03-插件系统](./docs/03-插件系统.md) | Wasm + gRPC 双运行时、Manifest 规范、Host API |
| [04-权限系统](./docs/04-权限系统.md) | RBAC + ABAC 混合权限模型 |
| [05-事件系统](./docs/05-事件系统.md) | CloudEvents + NATS Event Bus |
| [06-API设计](./docs/06-API设计.md) | RESTful API 规范、统一响应体、错误码 |
| [07-数据库设计](./docs/07-数据库设计.md) | PostgreSQL 表结构、索引、迁移规范 |
| [08-开发规范](./docs/08-开发规范.md) | 编码规范、Git 工作流、Commit 规范 |
| [09-插件开发指南](./docs/09-插件开发指南.md) | 从零开发插件的完整教程 |
| [10-开发路线图](./docs/10-开发路线图.md) | v0.1 → v1.0 演进计划 |
| [11-工具安装指南](./docs/11-工具安装指南.md) | Ubuntu 22.04 开发环境搭建 |

## 🤝 参与贡献

我们欢迎所有形式的贡献！

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交改动 (`git commit -m 'feat: add amazing feature'`)
4. 推送分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

请阅读 [开发规范](./docs/08-开发规范.md) 了解代码风格和协作流程。

## 📄 许可证

本项目采用 [MIT License](./LICENSE) 开源协议。