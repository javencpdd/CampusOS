# 开发环境

## 环境要求

| 工具 | 用途 |
| --- | --- |
| Go | 原生模式编译和运行 API、CLI 与测试；完整 Docker 模式不要求宿主安装。 |
| Node.js + pnpm | 原生模式运行 Web、Admin 和官方文档；完整 Docker 模式不要求宿主安装。 |
| Docker Compose | 运行共享 PostgreSQL、Redis、NATS，或运行完整容器开发栈。 |
| Git | 获取源码和管理变更。 |

## 获取项目

```bash
git clone https://github.com/javencpdd/CampusOS.git
cd CampusOS
./scripts/docker-dev.sh setup
# 编辑 deploy/docker/.env.dev.local
```

Docker 开发仍以这份宿主源码为准。容器通过 bind mount 读取它，不会替你 clone、commit 或 push。

## 准备原生工具链

只使用完整 Docker 模式时跳过本节。需要运行 `make dev-all` 时，安装 Go、Node.js、pnpm，并为三个独立前端
安装依赖：

```bash
cd web && pnpm install
cd ../admin && pnpm install
cd ../docs-site && pnpm install
cd ..
```

## 一键启动

完整 Docker 模式：

```bash
./scripts/docker-dev.sh setup --start
```

宿主机 API/前端模式：

```bash
STOP_EXISTING=true make dev-all
```

只要已经生成 `deploy/docker/.env.dev.local`，原生启动脚本会：

1. 停止完整 Docker 模式的 API、Web、Admin、Docs 容器。
2. 保留并启动同一个 `campusos-dev` PostgreSQL、Redis、NATS 项目和命名卷。
3. 读取同一份数据库、邮件和认证安全配置并执行 migration。
4. 从宿主工作区启动四个应用并登记受控 PID。

在另一终端执行 `./scripts/docker-dev.sh up` 可切回完整 Docker 模式。两种模式共享数据库卷和宿主
`data/`，但不会同时运行两套 API。

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
./scripts/docker-dev.sh infra-up
./scripts/docker-dev.sh migrate up
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
- `STOP_EXISTING=true`：受控停止 Docker 应用容器和已登记的旧原生进程，再交接默认端口。
- `WEB_PORT`、`ADMIN_PORT`、`DOCS_PORT`、`SERVER_PORT`：覆盖默认端口。
- `CAMPUSOS_DEV_INFRA_MODE=legacy`：使用旧 `docker-compose.yml` 独立数据源；不与 Docker 开发卷共享。

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
