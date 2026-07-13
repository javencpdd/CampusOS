# 配置与端口

## 配置加载顺序

后端按以下顺序读取配置：

1. 当前进程环境变量。
2. 项目根目录 `.env`。
3. 代码中的开发默认值。

`.env` 不应提交到 Git。新增非敏感示例项时同步更新 `.env.example`。

## 数据库

默认连接字符串：

```dotenv
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5432/campusos?sslmode=disable
```

如果宿主机 `5432` 已被占用，可以同时修改：

```dotenv
POSTGRES_PORT=5433
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5433/campusos?sslmode=disable
```

`POSTGRES_PORT` 只控制 Docker 暴露端口，不会自动重写 `DATABASE_DSN`。

## 服务端口

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SERVER_PORT` | `8080` | API HTTP 端口。 |
| `WEB_PORT` | `3000` | 用户前台开发端口。 |
| `ADMIN_PORT` | `3001` | 管理后台开发端口。 |
| `DOCS_PORT` | `3002` | 官方文档开发端口。 |
| `HOST_API_PORT` | `18080` | 插件 Host API 端口。 |
| `PGADMIN_PORT` | `5050` | pgAdmin 页面端口。 |

Admin 中的官方文档链接默认指向 `http://localhost:3002`。独立部署文档站后，在构建 Admin 前设置：

```dotenv
VITE_DOCS_URL=https://docs.example.edu
VITE_GITHUB_URL=https://github.com/javencpdd/CampusOS
```

## 插件目录

```dotenv
PLUGINS_DIR=data/plugins
PLUGIN_DATA_DIR=data/plugin_data
```

- `PLUGINS_DIR`：Manager 扫描插件实现的根目录。
- `PLUGIN_DATA_DIR`：插件 KV 和运行数据根目录。

不要把两个变量指向同一目录。

## 实例模式

```dotenv
CAMPUSOS_INSTANCE_MODE=single
```

v0.9 的 `User Storage Local Provider` 和 Host API v1 的 SQLite KV 均为本地单写实现。设置 `CAMPUSOS_INSTANCE_MODE=multi` 会让服务在启动前明确拒绝运行，避免多个实例悄悄写同一份本地文件或 SQLite 数据。

当前生产多实例方案需要共享 User Storage Provider、外部插件 Runtime 协调和 SQLite 迁移，尚不属于 v0.9 交付范围。不要通过共享宿主目录绕过此限制。

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
