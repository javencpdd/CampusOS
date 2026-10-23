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

## V12-05 Wasm 执行器

更新时间：2026-10-19（Asia/Shanghai）。Linux 原生 API 可设置 `CAMPUSOS_PLUGIN_WASM_IMAGE=sha256:<完整64位小写hex>`，指向宿主从当前源码构建、仅含固定 `/runner` 的本地 scratch 镜像。默认空值关闭后端执行；标签、缺失镜像、非 Linux、无实际 cgroup v2 控制或 PG 独占 guard 均拒绝发布。构建命令和完整 ABI 见仓库 `docs/api/v1.2插件Wasm执行沙箱合同.md`。

需宿主 `/usr/bin/docker`、PostgreSQL 与单写部署。不要给 API 开发容器或 guest 挂载 Docker socket；Compose 模板没有自动启用本原生宿主模式。guest 无 DB/Secret/宿主目录/网络/凭据，只能使用有限 typed Host import。每 worker 最多256MiB/1000 millicores/并发1/5000ms，总预留最多1024MiB/4000 millicores/16 workers；批准更小值仍生效。管理端 enable 只是意图，实际 ready 才发布贡献。

撤销/到期/进程退出会关闭准入并回收，已知 runtime_lost 需显式 enable/retry。重启先取得 PG 独占 guard，清理同一数据库部署命名空间的旧 worker，再准备新代次。Container/Windows、作者 SDK、内建 AI/RAG 消费接线仍各属06/08a/14c；本页不表示 v1.2 Final。


## V12-06 Container 执行器

更新时间：2026-10-19（Asia/Shanghai）。原生 API 显式设置 `CAMPUSOS_PLUGIN_CONTAINER_ENABLED=true`，默认false。签名清单必须引用本地缓存的明确 registry+完整 RepoDigest；宿主不会自动拉取。镜像 EntryPoint 需要实现 `campusos.container-pipe/v1`，HTTP/1.1 或 unary gRPC/HTTP2 经独占标准输入/输出通信，不开放插件端口。

执行需要本地 Docker Linux containers、cgroup v2、默认 seccomp 和独占 PG guard；网络none、只读根/非root、CPU/RSS/swap0/PID硬限、独立限额临时目录，无host mount/socket/数据库或Token。普通配置及当前 typed Host Broker 通过有限协议传递，Secret/Egress未发布能力拒绝。Wasm/Container共用总预算与leader；取消、OOM、退出、撤权/到期和重启按当前代次回收。

Linux真实进程/数据库/实际服务器证据及完整合同见仓库 `docs/api/v1.2插件Container执行沙箱合同.md`、`.125`。Windows固定本地Docker Desktop实现分支仍待真实目标验收，交叉编译不能使06完成。作者SDK和内建AI/RAG接线仍属08a/14c。

V12-06本地启动目录合同更新：Windows从系统API获取Program Files与共享Windows目录，不假定C盘，不继承同名环境变量；仅支持标准系统级Docker Desktop路径，UNC/自定义/user级安装无搜索回退。目录查询失败保持关闭。当前合同与Linux回归见仓库`.126`及`v12-06-local-command-linux.json`，Windows实机仍未验收。

2026-10-19：仓库提供`scripts/v12-06-windows-drill.ps1`原生验收入口，包含独占PG与HTTP/gRPC、当前授权、实际服务器恢复/正常关闭。需要真实Windows控制台及支持race的原生Go工具链；详细命令和前提见Container执行合同第6节及`.127`。Linux PowerShell只用于语法/拒绝路径验证，当前仍缺Windows实机结果，06保持进行中。

2026-10-19 12:06：用户同意Windows相关内容延期，当前以已验收Linux范围推进后续开发，记录见仓库`.128`。Windows原生实机仍未验收，完整跨平台出口检查器保留；延期不会改变执行器默认关闭、本地daemon或隔离配置。
