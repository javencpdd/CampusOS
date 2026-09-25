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
| `CAMPUSOS_DEV_PLUGIN_UI_PORT` | `3003` | v4 Plugin UI Gateway 开发端口。 |
| `PGADMIN_PORT` | `5050` | pgAdmin 页面端口。 |

Admin 中的官方文档链接默认指向 `http://localhost:3002`。独立部署文档站后，在构建 Admin 前设置：

```dotenv
VITE_WEB_URL=https://community.example.edu
VITE_DOCS_URL=https://docs.example.edu
VITE_GITHUB_URL=https://github.com/javencpdd/CampusOS
```

`VITE_WEB_URL` 用于 Admin 的“外观与风格包”页面打开用户端主题切换页；它不是后端 API 地址。

## v4 插件目录

```dotenv
CAMPUSOS_PLUGIN_V4_DIR=plugins
CAMPUSOS_PLUGIN_V4_DEV_SOURCE=true
CAMPUSOS_PLUGIN_UI_ORIGIN=http://localhost:3003
```

- `CAMPUSOS_PLUGIN_V4_DIR`：v4 源码和 `.installed` 发布包的根目录。
- `CAMPUSOS_PLUGIN_V4_DEV_SOURCE=true`：仅开发环境使用，将已验证源码制作为可发现的发布版本；生产模式只扫描 `.installed/`。
- `CAMPUSOS_PLUGIN_UI_ORIGIN`：Web/Admin 中隔离 iframe 的 Gateway 来源；浏览器不可使用 Docker 内部 `api` 主机名。

用户配置由宿主创建在 `data/personal-space/<user-id>/plugins/<key>/`。旧 `data/plugins` 和
`data/plugin_data` 只保留 v1-v3 兼容资料，不能配置为新插件的发现入口。

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
