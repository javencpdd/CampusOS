# 环境配置与 Host API 端口说明

> 适用范围：CampusOS 本地开发环境
> 相关文件：`.env`、`.env.example`、`pkg/config/config.go`、`docker-compose.yml`、`internal/plugin/hostapi/grpc_server.go`

## 1. `.env.example` 在开发环境中会生效吗

不会直接生效。

`.env.example` 是示例配置文件，作用是告诉开发者“可以有哪些配置项、默认应该怎么写”。当前 CampusOS 后端不会主动读取 `.env.example`。

当前后端配置加载逻辑在 `pkg/config/config.go`：

```go
fileEnv := loadDotEnv(".env")
```

这表示后端启动时只尝试读取项目根目录下的 `.env` 文件。没有 `.env` 时，后端会继续使用系统环境变量或代码里的默认值。

## 2. 配置读取优先级

后端读取配置的优先级是：

```text
系统环境变量 > .env 文件 > 代码默认值
```

举例：

| 配置来源 | 示例 | 是否会被后端读取 |
| --- | --- | --- |
| shell 环境变量 | `export SERVER_PORT=9000` | 会，优先级最高 |
| `.env` | `SERVER_PORT=8080` | 会，在没有同名 shell 环境变量时生效 |
| `.env.example` | `SERVER_PORT=8080` | 不会直接生效 |
| 代码默认值 | `SERVER_PORT` 默认 `8080` | 当前两者都没有时生效 |

Docker Compose 也类似：它默认会读取项目目录下的 `.env` 作为变量替换来源，例如：

```yaml
ports:
  - "${POSTGRES_PORT:-5432}:5432"
```

这里的 `${POSTGRES_PORT:-5432}` 会优先使用 shell 环境变量或 `.env` 里的 `POSTGRES_PORT`。`.env.example` 不会被 Docker Compose 自动当作运行配置。

## 3. 怎么判断启动时用的是 `.env.example` 还是 `.env`

准确说，当前系统不会“启动 `.env.example`”。需要判断的是：当前进程用的是 shell 环境变量、`.env`，还是代码默认值。

### 3.1 先看文件是否存在

在项目根目录执行：

```bash
ls -la .env .env.example
```

如果 `.env` 存在，后端会尝试读取它。

如果只有 `.env.example`，后端不会读取它。此时后端使用 shell 环境变量或代码默认值。

### 3.2 检查 shell 环境变量是否覆盖了 `.env`

例如检查端口：

```bash
printenv SERVER_PORT
printenv HOST_API_ADDR
printenv DATABASE_DSN
```

如果这些命令有输出，那么对应值会覆盖 `.env`。

如果没有输出，后端才会继续看 `.env`。

### 3.3 对比 `.env` 中的值

查看 `.env` 中对应配置：

```bash
grep -E '^(SERVER_PORT|HOST_API_ADDR|DATABASE_DSN|POSTGRES_PORT)=' .env
```

如果 shell 里没有同名环境变量，这些值就是后端优先使用的值。

### 3.4 看实际监听端口

启动后端后执行：

```bash
ss -ltnp | grep -E ':(8080|18080)\b'
```

常见结果：

| 监听端口 | 说明 |
| --- | --- |
| `0.0.0.0:8080` | 主业务 API 服务，供 web/admin 前端调用 |
| `127.0.0.1:18080` | Host API 服务，供插件/SDK 调用 |

如果你把 `.env` 里的 `SERVER_PORT` 改成 `9000`，重启后端后主业务端口应变成 `9000`。如果没变，通常说明有 shell 环境变量覆盖，或者后端没有在项目根目录启动。

### 3.5 最直接的测试方法

可以临时设置 shell 环境变量测试优先级：

```bash
SERVER_PORT=19090 go run ./cmd/server
```

如果日志显示服务启动在 `0.0.0.0:19090`，说明 shell 环境变量生效，并覆盖了 `.env`。

## 4. `.env` 和 `.env.example` 应该怎么用

推荐用法：

```bash
cp .env.example .env
```

然后只修改 `.env`。

`.env.example` 应保留为模板，不放真实密钥，不写个人机器上的临时端口。`.env` 是本机实际运行配置，可以根据当前开发机情况调整，例如 Docker PostgreSQL 映射到 `5433` 时，需要让 `DATABASE_DSN` 指向对应端口。

## 5. 127.0.0.1:18080 是做什么的

`127.0.0.1:18080` 是 CampusOS 的 Host API 端口。

通俗解释：

```text
8080  是普通用户和后台管理页面调用的主业务 API。
18080 是插件调用系统能力的内部 API。
```

也就是说：

| 地址 | 用途 | 面向对象 |
| --- | --- | --- |
| `http://127.0.0.1:8080/api/v1/...` | 注册、登录、发帖、版块、后台管理等主业务接口 | web 前端、admin 前端、普通 API 调用 |
| `http://127.0.0.1:18080/api/host/...` | 插件读取用户/帖子、发布事件、读写插件配置、检查权限、写插件日志等 | 插件、Go SDK、后续插件运行时 |

18080 不负责显示网页，也不是管理后台端口。它是给插件用的“系统能力入口”。

## 6. 为什么 Host API 要单独开 18080

主业务 API 和插件 API 分开，有几个好处：

| 好处 | 说明 |
| --- | --- |
| 边界清晰 | 用户前端走 8080，插件能力走 18080 |
| 权限可控 | 插件调用 Host API 时必须声明插件身份和权限 |
| 便于隔离 | 后续可以限制 Host API 只监听 `127.0.0.1`，不暴露到外网 |
| 便于 SDK 固定入口 | `sdk/go` 默认 Host API 地址就是 `http://127.0.0.1:18080` |

当前默认配置来自 `pkg/config/config.go`：

```go
HostAPI: HostAPIConfig{
    Enabled: get("HOST_API_ENABLED", "true") == "true",
    Addr:    get("HOST_API_ADDR", "127.0.0.1:18080"),
}
```

如果不想启动 Host API，可以在 `.env` 中设置：

```env
HOST_API_ENABLED=false
```

如果要改端口，可以设置：

```env
HOST_API_ADDR=127.0.0.1:18081
```

改完后需要重启后端。

## 7. 18080 能直接访问吗

可以访问，但它不是普通浏览器页面。

Host API 只接受类似下面的插件调用：

```text
POST /api/host/GetUser
POST /api/host/GetThread
POST /api/host/QueryThreads
POST /api/host/PublishEvent
POST /api/host/GetConfig
POST /api/host/SetConfig
POST /api/host/CheckPermission
POST /api/host/Log
POST /api/host/StorageGet
POST /api/host/StorageSet
POST /api/host/StorageDelete
```

而且请求需要带插件身份，例如：

```http
X-CampusOS-Plugin: hello-wasm
```

如果没有插件身份，或者插件没有注册，接口会拒绝请求。

## 8. 常见误区

| 误区 | 正确理解 |
| --- | --- |
| `.env.example` 会自动生效 | 不会，它只是模板 |
| 修改 `.env.example` 后服务配置会改变 | 不会，应修改 `.env` |
| 18080 是后台管理页面 | 不是，后台页面通常是 `3001`，后端主 API 是 `8080` |
| 18080 和 8080 是同一个服务 | 不是，8080 是主业务 API，18080 是插件 Host API |
| 18080 应该暴露给外网 | 不建议，默认绑定 `127.0.0.1`，只给本机插件/SDK 调用更安全 |

## 9. 推荐排查命令

```bash
# 1. 看配置文件
ls -la .env .env.example

# 2. 看 shell 环境变量是否覆盖
printenv SERVER_PORT
printenv HOST_API_ADDR
printenv DATABASE_DSN

# 3. 看 .env 中实际配置
grep -E '^(SERVER_PORT|HOST_API_ENABLED|HOST_API_ADDR|DATABASE_DSN|POSTGRES_PORT)=' .env

# 4. 看服务实际监听端口
ss -ltnp | grep -E ':(8080|18080)\b'

# 5. 看主业务 API 是否正常
curl http://127.0.0.1:8080/api/v1/health
```

如果这些结果互相矛盾，优先检查是否在项目根目录启动后端，以及 shell 环境变量是否覆盖了 `.env`。
