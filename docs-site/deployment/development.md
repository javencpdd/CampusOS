# 开发环境

## 环境要求

| 工具 | 用途 |
| --- | --- |
| Go | 编译和运行 API、CLI 与测试。 |
| Node.js + pnpm | 运行 Web、Admin 和官方文档前端。 |
| Docker Compose | 启动 PostgreSQL、Redis、NATS 和 pgAdmin。 |
| Git | 获取源码和管理变更。 |

## 获取项目

```bash
git clone https://github.com/javencpdd/CampusOS.git
cd CampusOS
cp .env.example .env
```

## 安装前端依赖

三个前端是相互独立的 Node 项目：

```bash
cd web && pnpm install
cd ../admin && pnpm install
cd ../docs-site && pnpm install
cd ..
```

## 一键启动

```bash
STOP_EXISTING=true make dev-all
```

启动脚本依次执行：

1. 启动 Docker 开发依赖。
2. 执行尚未应用的数据库 migration。
3. 启动 API、Web、Admin 和官方文档站。
4. 等待四个 HTTP 地址可访问。

| 服务 | 默认地址 |
| --- | --- |
| Web | `http://localhost:3000` |
| Admin | `http://localhost:3001` |
| Docs | `http://localhost:3002` |
| API | `http://localhost:8080/api/v1` |
| pgAdmin | `http://localhost:5050` |

开发日志写入：

```text
.campusos/logs/api.log
.campusos/logs/web.log
.campusos/logs/admin.log
.campusos/logs/docs.log
```

## 只启动部分服务

基础设施：

```bash
make docker-up
make migrate-up
```

后端：

```bash
make run
```

前端：

```bash
make web-dev
make admin-dev
make docs-dev
```

## 常用启动选项

```bash
SKIP_INFRA=true SKIP_MIGRATE=true make dev-all
```

- `SKIP_INFRA=true`：不启动 Docker 依赖。
- `SKIP_MIGRATE=true`：不执行 migration。
- `STOP_EXISTING=true`：停止占用目标端口的旧开发进程。
- `WEB_PORT`、`ADMIN_PORT`、`DOCS_PORT`、`SERVER_PORT`：覆盖默认端口。

## 开发账号

默认管理员由 migration 和后端启动兜底逻辑创建：

| 项目 | 值 |
| --- | --- |
| 邮箱 | `admin@campusos.local` |
| 默认密码 | `Admin@123456` |

该账号只用于本地开发。任何面向共享环境的部署都必须修改默认密码和敏感配置。

## 验证改动

```bash
GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
make docs-links
cd web && pnpm lint && pnpm format:check && pnpm build
cd ../admin && pnpm build
cd ../docs-site && pnpm build
cd ../sdk/typescript && pnpm build
```

修改 migration 后额外执行：

```bash
make migrate-up
make migrate-status
```
