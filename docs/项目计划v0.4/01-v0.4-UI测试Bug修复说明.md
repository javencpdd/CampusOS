# CampusOS v4 UI 测试 Bug 修复说明

> 日期：2026-07-02
> 范围：`docs/Todo/bug` 中记录的用户主页、数据库/pgAdmin、admin 页面问题
> 状态：已处理并完成本地验证

## 1. 问题来源

本次修复基于以下记录：

| 文件 | 问题 |
| --- | --- |
| `docs/Todo/bug/关于用户主页` | 访问个人主页内容接口时报 `user_space_contents.tags` 违反 not-null 约束 |
| `docs/Todo/bug/数据库操作` | pgAdmin 无法连接 PostgreSQL，手动 `docker network connect` 时遇到 5432 端口占用 |
| `docs/Todo/bug/admin页面` | 仪表盘快捷入口进入 `/admin/users` 等地址后空白；插件日志弹窗报 `rows is not iterable` |

## 2. 修复思路

### 2.1 用户主页内容同步

现象：

```text
错误: null value in column "tags" of relation "user_space_contents" violates not-null constraint
GET /api/v1/u/user_1/contents?page=1&page_size=20 500
```

根因：

`thread.Tags` 为空时，Go 中的 `nil` slice 传给 PostgreSQL `TEXT[] NOT NULL` 字段会被编码为 SQL `NULL`。数据库字段虽然有 `DEFAULT '{}'`，但显式传入 `NULL` 时默认值不会生效。

处理：

| 文件 | 处理 |
| --- | --- |
| `internal/space/sync.go` | 同步帖子到个人主页内容时，将 `nil` tags 归一化为空数组 |
| `internal/space/pg_repository.go` | PostgreSQL 写入前再次做防御性默认值处理 |
| `internal/space/sync_test.go` | 增加 nil tags 归一化测试，避免回归 |

同时保留个人主页内容回填逻辑：访问个人主页内容列表时，如果历史帖子尚未通过事件同步进入 `user_space_contents`，服务会按作者回填已发布帖子。

### 2.2 pgAdmin 与 PostgreSQL 连接

现象：

```text
connection to server at "127.0.0.1", port 5432 failed: Connection refused
failed to bind host port 0.0.0.0:5432/tcp: address already in use
```

根因分为两层：

| 问题 | 说明 |
| --- | --- |
| 连接地址误用 | pgAdmin 运行在 Docker 容器内，`127.0.0.1` 指 pgAdmin 容器自身，不是 PostgreSQL 容器 |
| 宿主机端口冲突 | 本机已有 PostgreSQL 占用 `127.0.0.1:5432`，Compose 默认 `5432:5432` 无法稳定重建 |

处理：

| 文件/环境 | 处理 |
| --- | --- |
| `docker-compose.yml` | pgAdmin 增加 `PGADMIN_LISTEN_ADDRESS=0.0.0.0`，保留 `host.docker.internal` 兜底 |
| `scripts/docker-up.sh` / `scripts/docker-up.ps1` | 将 pgAdmin 纳入 `make docker-up` 启动范围，并检查端口占用 |
| `.env`（本地忽略文件） | 设置 `POSTGRES_PORT=5433` 和对应 `DATABASE_DSN`，避开宿主机 5432 冲突 |
| `docs/help/数据库管理指南.md` | 明确 pgAdmin 中应使用 Host=`postgres`、Port=`5432` |
| `README.md` | 同步默认账号、可配置端口和当前迁移文件列表 |

本地修复后的 pgAdmin 连接规则：

| 场景 | Host | Port |
| --- | --- | --- |
| pgAdmin 页面里连接 CampusOS PostgreSQL | `postgres` | `5432` |
| 宿主机命令行连接 CampusOS PostgreSQL | `127.0.0.1` | `5433` |

不建议用 `docker network connect` 解决该问题。端口被占用时，应该先调整 `.env` 中的 `POSTGRES_PORT` 并重建容器。

### 2.3 admin 快捷入口空白

现象：

仪表盘快捷按钮进入：

```text
/admin/users
/admin/threads
/admin/categories
/admin/plugins
/admin/events
```

页面空白；侧边栏进入 `/users`、`/threads` 等地址正常。

根因：

admin 路由实际挂载在根路径子路由下，合法地址是 `/users`、`/threads`、`/categories`、`/plugins`、`/events`。仪表盘写死跳转 `/admin/*`，这些路径没有匹配组件。

处理：

| 文件 | 处理 |
| --- | --- |
| `admin/src/views/DashboardView.vue` | 快捷入口改为命名路由跳转 |
| `admin/src/router/index.ts` | 增加 `/admin/*` 到真实路由的兼容重定向 |

这样既修复仪表盘按钮，也兼容用户手动打开旧路径。

### 2.4 插件日志弹窗表格报错

现象：

```text
TypeError: rows is not iterable
TypeError: Cannot set properties of null (setting '__vnode')
```

根因：

Element Plus 的 `el-table` 要求 `data` 必须是数组。插件日志接口正常返回结构是：

```json
{
  "data": {
    "items": [],
    "total": 0
  }
}
```

旧代码使用 `r?.data?.items || r?.data || []`，当 `items` 不存在、为 `null` 或响应结构变化时，可能把对象传给 `el-table`，触发 `rows is not iterable`。

处理：

| 文件 | 处理 |
| --- | --- |
| `admin/src/views/PluginManageView.vue` | 增加 `responseItems`，统一从接口响应中提取数组；无法提取时返回空数组 |

## 3. 验证结果

| 命令 | 结果 |
| --- | --- |
| `GOCACHE=/tmp/campusos-go-cache go test ./internal/space -count=1` | 通过 |
| `GOCACHE=/tmp/campusos-go-cache go test ./... -count=1` | 通过 |
| `cd admin && npm run build` | 通过，仅有 Vite/Rollup 既有 chunk 和注释警告 |
| `docker compose config -q` | 通过 |
| `docker compose up -d --force-recreate postgres pgadmin` | 通过，未删除 volume |
| `docker exec campusos-pgadmin getent hosts postgres` | 通过，pgAdmin 可解析 `postgres` |
| `docker exec campusos-pgadmin python3 -c '...'` | 通过，pgAdmin 容器可连接 `postgres:5432` |
| `curl -I http://127.0.0.1:5050/` | 通过，返回 302 到 pgAdmin 登录页 |
| `PGPASSWORD=campusos_dev psql -h 127.0.0.1 -p 5433 -U campusos -d campusos -c 'SELECT 1;'` | 通过 |
| `make migrate-up && make migrate-status` | 通过，`schema_migrations` 记录到 `000010_fix_admin_seed_password` |

## 4. 本地环境说明

当前宿主机已有 PostgreSQL 占用 `127.0.0.1:5432`。为避免和 CampusOS Docker PostgreSQL 冲突，本地 `.env` 使用：

```env
POSTGRES_PORT=5433
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5433/campusos?sslmode=disable
```

`.env` 已被 `.gitignore` 忽略，不进入仓库提交。其他开发者如果没有本机 PostgreSQL 占用 `5432`，可以继续使用默认端口。

## 5. 后续建议

| 建议 | 说明 |
| --- | --- |
| UI 回归 | 手动验证 `/u/:username`、`/space/settings`、admin 仪表盘快捷入口、插件日志弹窗 |
| 连接说明 | 后续文档中区分“容器内端口”和“宿主机映射端口” |
| admin 路由 | 新增后台页面时优先使用命名路由，避免路径前缀再次写错 |
| 数据结构 | 前端表格数据进入 `el-table` 前统一归一化为数组 |
