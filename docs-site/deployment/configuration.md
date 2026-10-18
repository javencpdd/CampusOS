# 配置与端口

## 配置加载顺序

后端按以下顺序读取配置：

1. 当前进程环境变量。
2. 项目根目录 `.env`。
3. 代码中的开发默认值。

`.env` 不应提交到 Git。新增非敏感示例项时同步更新 `.env.example`。

启动器还会注入模式配置：

- `docker-dev.* up` 由 Compose 读取 `deploy/docker/.env.dev.local`。
- `STOP_EXISTING=true make dev-all` 检测到该文件后，会把其中的数据库、Redis、NATS、邮件和认证配置转成
  进程环境变量，因此优先于根 `.env`，并共享 Docker 开发数据卷。
- `CAMPUSOS_DEV_INFRA_MODE=legacy` 才使用根 `.env` 和旧 `docker-compose.yml` 独立数据源。

## 数据库

下面的默认连接字符串只描述没有共享 Docker 开发配置时的 Legacy/直接后端启动：

```dotenv
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5432/campusos?sslmode=disable
```

如果宿主机 `5432` 已被占用，可以同时修改：

```dotenv
POSTGRES_PORT=5433
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5433/campusos?sslmode=disable
```

`POSTGRES_PORT` 只控制旧 Compose 暴露端口，不会自动重写 `DATABASE_DSN`。共享模式改用
`CAMPUSOS_DEV_POSTGRES_PORT`，`start-dev.sh` 会自动构造指向该映射端口的 DSN。

## 服务端口

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SERVER_PORT` | `8080` | API HTTP 端口。 |
| `WEB_PORT` | `3000` | 用户前台开发端口。 |
| `ADMIN_PORT` | `3001` | 管理后台开发端口。 |
| `DOCS_PORT` | `3002` | 官方文档开发端口。 |
| `CAMPUSOS_DEV_PLUGIN_UI_PORT` | `3003` | v5 独立 Plugin UI Gateway 开发端口。 |
| `PGADMIN_PORT` | `5050` | pgAdmin 页面端口。 |

Admin 中的官方文档链接默认指向 `http://localhost:3002`。独立部署文档站后，在构建 Admin 前设置：

```dotenv
VITE_WEB_URL=https://community.example.edu
VITE_DOCS_URL=https://docs.example.edu
VITE_GITHUB_URL=https://github.com/javencpdd/CampusOS
```

`VITE_WEB_URL` 用于 Admin 的“外观与风格包”页面打开用户端主题切换页；它不是后端 API 地址。

## v5 插件与隔离界面

签名包由 Admin 显式导入，受管存储位于 `PLUGIN_DATA_DIR/v5`，普通配置保存在 PostgreSQL；不扫描旧 `.installed` 或源码目录自动运行。

原生服务需同时配置以下三项，未配置则关闭外部 UI：

```dotenv
CAMPUSOS_PLUGIN_UI_LISTEN=127.0.0.1:3004
CAMPUSOS_PLUGIN_UI_ORIGIN=http://{isolation}.localhost:3003
CAMPUSOS_PLUGIN_UI_PARENTS="http://localhost:3000 http://localhost:3001"
```

`{isolation}` 必须是域名前缀，宿主按当前版本、owner、会话和授权证明分配随机租约域。3004 是仅接收受管静态资源的内部监听；浏览器使用 3003 网关，不能使用 Docker 内部 `api` 名称。父页面必须是精确 Origin，不能设置通配符。网关不提供 API、Cookie/Authorization 透传或宿主目录。

Docker 开发配置使用 `CAMPUSOS_PLUGIN_UI_SCHEME=http`、`CAMPUSOS_PLUGIN_UI_DOMAIN=localhost` 和 `CAMPUSOS_DEV_PLUGIN_UI_PORT=3003` 构造带 `{isolation}` 的 Origin，`CAMPUSOS_PLUGIN_UI_PARENTS` 与实际 Web/Admin Origin 一致。更改 Compose、入口或 SDK 挂载后运行 `docker-dev.* rebuild`。

生产为独立 UI 域配置通配 DNS/TLS，例如 `https://{isolation}.plugins.example.edu`；不能与 Web/Admin 同源，也不能把父页面放在该隔离命名空间。局域网浏览器的 localhost 指向设备自身，须使用解析到开发机的专用通配域并更新精确父 Origin；不自动依赖公网 DNS 服务。详见[Docker 开发](docker-development.md)与插件操作（仓库 `docs/help/插件v5包与签名操作说明.md`）。

## 实例模式

```dotenv
CAMPUSOS_INSTANCE_MODE=single
```

当前 `User Storage Local Provider` 和 Host API v1 的 SQLite KV 均为本地单写实现。设置 `CAMPUSOS_INSTANCE_MODE=multi` 会让服务在启动前明确拒绝运行，避免多个实例悄悄写同一份本地文件或 SQLite 数据。

生产多实例方案需要共享 User Storage Provider、外部插件 Runtime 协调、SQLite 迁移以及集群级 Webhook 限流，尚不属于 v0.11 交付范围。不要通过共享宿主目录绕过此限制。

## 本地日志

```dotenv
CAMPUSOS_LOG_DIR=.campusos/logs
```

平台日志接口只允许读取预先声明的日志源，不接受任意文件路径。

## 敏感配置

以下信息不能写入仓库、插件包或前端构建产物：

- 数据库生产密码。
- JWT 密钥和 API Key。
- Webhook 签名密钥。
- AI provider token。
- 第三方平台凭据。

生产环境应由部署平台的 Secret 管理能力注入，而不是复用开发 `.env`。

签名信任文件由操作者提供，设置容器可见的 `CAMPUSOS_PLUGIN_V5_TRUST_STORE` 路径；空值不信任任何发布者。开发可放在被 Git 忽略的 `.campusos/` 并使用 `/workspace/.campusos/...` 路径，不能放私钥。生产 API Compose 传入完整 UI 三变量，默认全空；TLS 反向代理只向独立 UI 监听（如 `api:3004`）转发有限静态请求并保留租约 Host，不能把 UI 域转到平台 API。正式栈不自动开通外部域、证书或端口。
