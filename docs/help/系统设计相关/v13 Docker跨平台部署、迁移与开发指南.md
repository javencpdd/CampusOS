# CampusOS v13 Docker 跨平台部署、迁移与开发指南

> 适用版本：`v0.13.0`  
> 适用宿主：Windows 10/11 + Docker Desktop Linux Containers，或支持 Docker Engine 与 Compose v2 的现代 Linux  
> 权威入口：`compose.deploy.yml`、`compose.dev.yml`、`deploy/docker/`、`scripts/docker-*.sh|ps1`

## 1. 目标与边界

本交付解决两个问题：

1. 用不可变镜像在另一台 Windows/Linux 主机启动单机 CampusOS。
2. 开发者不安装宿主 Go/Node/PostgreSQL，也能绑定源码、热更新和运行测试。

容器仍是 Linux 容器。Windows 通过 Docker Desktop 的 WSL2 backend 运行，不支持原生 Windows
Containers、32 位系统或没有 Compose v2 的旧 Docker。v13 不把单主机 Compose 描述为高可用、自动 TLS、
跨区域备份或多节点生产集群。本轮真实整栈与恢复证据来自 Linux `amd64`；Windows Docker Desktop 与
Linux `arm64` 具备同一静态合同，但正式发行仍需在目标平台 Runner 重复验收。

## 2. 两套 Compose 的职责

| 文件 | 用途 | 源码 | 前端 | 数据 |
| --- | --- | --- | --- | --- |
| `compose.deploy.yml` | 运行、升级、备份和主机迁移 | 构建进镜像 | Nginx 静态产物 | PostgreSQL/Redis/NATS/CampusOS 命名卷 |
| `compose.dev.yml` | Windows/Linux 继续开发 | 绑定挂载 | Vite/VitePress HMR | 开发数据库命名卷；仓库 `data/` 直接可见 |

两套栈使用不同项目名和命名卷，不应混用。部署配置由 `.env.docker` 提供随机密钥；开发配置以
`deploy/docker/.env.dev.example` 为模板，并由首次配置向导生成受 Git 忽略的
`deploy/docker/.env.dev.local`。开发栈继续使用明确标注的 loopback 固定值。

### 2.1 开发栈首次配置向导

向导不会替开发者克隆代码。必须先在宿主机取得 Git 工作区；开发 Compose 把仓库根目录和三个前端目录
绑定挂载进容器，Git 分支、提交和推送仍由宿主完成。容器 shell 只用于诊断，不能作为源码唯一保存位置。

Linux：

```bash
./scripts/docker-dev.sh setup
# 编辑 deploy/docker/.env.dev.local
./scripts/docker-dev.sh setup --start
```

Windows PowerShell：

```powershell
.\scripts\docker-dev.ps1 setup
# 编辑 deploy/docker/.env.dev.local
.\scripts\docker-dev.ps1 setup -Start
```

第一次执行只复制模板并提示需要填写的字段，不启动容器。第二次执行校验端口、loopback 绑定、邮件
Provider、SMTP 必填项和 Compose 语法；带 `--start`/`-Start` 时才构建并启动。脚本只显示非敏感摘要，
不会输出 `EMAIL_SMTP_PASSWORD`。

默认目标首次创建时，如果根 `.env` 已存在，向导会按固定白名单迁移 PostgreSQL 账号、Redis 密码、
JWT/Challenge/Session/MFA 和 `EMAIL_*`，但不复制旧 `DATABASE_DSN` 或端口。迁移值不输出；创建完成后
应以 `.env.dev.local` 为共享开发模式的配置事实源，避免继续双写。

本地配置至少需要确认：

| 配置 | 默认值 | 什么时候修改 |
| --- | --- | --- |
| `CAMPUSOS_DEV_*_PORT` | Web `3000`、Admin `3001`、Docs `3002`、API `8080` 等 | 宿主端口冲突时 |
| `POSTGRES_*` | 仅 loopback 开发使用的固定值 | 需要隔离多个开发栈时 |
| `EMAIL_PROVIDER` | `fake` | 需要收到注册、找回密码或邮箱绑定验证码时改为 `smtp` |
| `EMAIL_SMTP_*` | 空 | 使用 SMTP 时填写 host、port、from；需要认证时 username/password 成对填写 |

`fake` 只确认可靠消费者被调用，不发送、不显示验证码。因此它适合 API 和现有账号开发，但无法完成依赖
真实验证码的新用户注册。配置 SMTP 后，脚本只能确认结构有效；网络、授权码和收件箱仍需在启动后验证。

部署栈同时按组件拆分为：

```text
deploy/docker/components/
├── compose.infra.yml
├── compose.api.yml
├── compose.web.yml
├── compose.admin.yml
├── compose.docs.yml
└── .env.<component>.example
```

根 `compose.deploy.yml` 使用 Compose `extends` 复用上述服务定义并补充健康依赖，承担一键启动。独立文件
可以分别构建、启动和停止；不是从整体文件复制出来的第二套实现。

## 3. 镜像和服务结构

`deploy/docker/Dockerfile` 提供四个生产 target：

- `api`：Go 二进制、migration、内置 module/plugin/resource 种子和维护脚本；
- `web`：用户前台静态构建；
- `admin`：管理后台静态构建；
- `docs`：VitePress 官方文档静态构建。

`deploy/docker/Dockerfile.dev` 提供：

- `api-dev`：Go 工具链、PostgreSQL 客户端和轮询重编译；
- `web-dev`、`admin-dev`、`docs-dev`：按各自 lockfile 构建依赖种子。

开发前端源码来自绑定挂载，`node_modules` 来自独立命名卷。三个服务不并行写同一个 pnpm store，
避免 Windows 文件共享和冷启动竞争。Go 开发构建关闭 VCS stamping，解决宿主 Git UID 与容器 UID 不同
导致的 safe-directory 错误；版本权威仍由仓库版本合同和 Git 提交决定。Bash 启动器自动传入宿主
UID/GID，PowerShell 使用 Docker Desktop/WSL2 的 `1000:1000`；API、Vite 和 VitePress 应用进程
降权运行。Corepack 使用镜像内共享缓存并锁定 pnpm `8.15.9`，避免非 root 运行期再次下载包管理器。

## 4. 数据和密钥边界

部署栈的权威持久数据：

| 数据 | 位置 |
| --- | --- |
| PostgreSQL | Compose `postgres-data` |
| CampusOS 文件和资源 | Compose `campusos-data`，容器内 `/app/data` |
| Redis/NATS 运行状态 | 各自命名卷；不作为跨主机权威备份 |
| 部署密钥 | 宿主 `.env.docker`，不进入镜像和普通备份 |

`.dockerignore` 排除个人空间、插件数据、模块数据、图片、配置、日志和构建产物。版本化
`data/plugins` 实现和 `data/resources` 内置资源可以作为首次启动种子；入口只复制缺失文件，不覆盖卷内
管理员安装的插件、用户资源或模块状态。

`.env.docker` 包含 JWT、Challenge、Session、MFA envelope、数据库和第三方凭据。迁移现有实例时必须
单独加密传输并限制读取。CampusOS Session、Access Token 和 Refresh Token 不作为可移植导出物；迁移后
应撤销旧会话并重新登录。

## 5. Linux 操作

部署：

```bash
./scripts/docker-deploy.sh init
./scripts/docker-deploy.sh config
./scripts/docker-deploy.sh up
```

开发：

```bash
./scripts/docker-dev.sh setup
./scripts/docker-dev.sh setup --start
./scripts/docker-dev.sh logs api
./scripts/docker-dev.sh test
```

## 6. Windows PowerShell 操作

部署：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\docker-deploy.ps1 init
.\scripts\docker-deploy.ps1 config
.\scripts\docker-deploy.ps1 up
```

开发：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\docker-dev.ps1 setup
.\scripts\docker-dev.ps1 setup -Start
.\scripts\docker-dev.ps1 logs -Service api
.\scripts\docker-dev.ps1 test
```

Windows 必须让 Docker Desktop 使用 Linux Containers，并授权仓库所在磁盘。建议将仓库放在 WSL2
文件系统；直接放在 NTFS 也能工作，但大型前端依赖和轮询监听通常更慢。

## 7. 分组件打包、配置和启动

初始化：

```bash
./scripts/docker-component.sh init
```

```powershell
.\scripts\docker-component.ps1 init
```

生成的配置在 `.env.components/`，被 Git 忽略并按组件划分：

| 文件 | 独立控制 |
| --- | --- |
| `infra.env` | backend 网络、PostgreSQL/Redis/NATS 数据卷和非敏感数据库名 |
| `api.env` | API 端口、backend/edge 网络和 CampusOS 数据卷 |
| `web.env` | 用户前台绑定地址和端口 |
| `admin.env` | 管理后台绑定地址、端口及公开伴随链接覆盖 |
| `docs.env` | 文档站绑定地址和端口 |

跨组件必须相同的数据库密码、JWT、MFA、Challenge、SMTP 和第三方 Secret 只保存在 `.env.docker`，避免复制
后发生漂移。

独立镜像构建：

```bash
./scripts/docker-component.sh build api
./scripts/docker-component.sh build web
./scripts/docker-component.sh build admin
./scripts/docker-component.sh build docs
```

独立启动：

```bash
./scripts/docker-component.sh up infra
./scripts/docker-component.sh up api
./scripts/docker-component.sh up web
./scripts/docker-component.sh up admin
./scripts/docker-component.sh up docs
```

Windows 将命令中的脚本改为 `docker-component.ps1` 并使用 `-Component api` 等参数。

组件通过 Compose 外部网络 `campusos_backend` 和 `campusos_edge` 发现 `postgres`、`redis`、`nats`
和 `api`。脚本将 backend 创建为 Docker internal 网络，把数据库和消息服务限制在内部通信范围；edge
网络负责前端到 API 的代理。若同名 backend 已存在但不是 internal，脚本会拒绝启动并要求先停止相关容器后
人工移除该网络。一键/分组件模式使用相同的 `campusos_*` 外部命名卷，因此切换启动方式不迁移数据。
自定义网络或卷名时必须在相关组件配置中保持一致。

同一个生产数据库卷不能同时挂载到一键栈和独立 infra 的两个 PostgreSQL 容器。备份与恢复是整实例操作：
先按 `docs -> admin -> web -> api -> infra` 停止分组件项目，再用相同网络/卷名启动总编排，执行
`docker-deploy.* backup|restore`；操作完成后再切回分组件模式。

## 8. 开发热更新合同

后端每隔 `CAMPUSOS_DEV_POLL_INTERVAL` 秒检查 Go、migration 和 module descriptor。变更时先构建候选，
再执行 migration，均成功后才替换正在运行的 API。构建失败不会主动杀死上一版成功进程。

Web/Admin/Docs 使用轮询监听，解决 Docker Desktop 对文件事件转发不一致的问题。修改普通源码会 HMR；
修改 `package.json` 或 lockfile 后重新执行 `docker-dev.* rebuild`，由 `--build` 重建对应依赖种子。日常
`docker-dev.* up` 使用 `--no-build` 从已有镜像启动，不会因普通源码变化访问 Docker Hub。

### 8.1 完整 Docker 与原生应用模式交接

完成 `docker-dev.* setup` 后，已安装 Go、Node.js 和 pnpm 的开发者可以执行：

```bash
STOP_EXISTING=true make dev-all
```

启动器停止 `campusos-dev` 的 API/Web/Admin/Docs 容器，保留 PostgreSQL、Redis、NATS 及其命名卷，再让
宿主进程读取同一个 Docker 开发配置、执行 migration 并使用宿主 `data/`。数据库事实、可靠任务、会话、
个人文件、插件/模块数据和资源不会因模式切换而换到另一套数据源。

反向切换使用 `./scripts/docker-dev.sh up` 或 `.\scripts\docker-dev.ps1 up`。脚本通过
`.campusos/run/native-dev.pid` 核验并停止原生 CampusOS 进程，禁止两套 API 同时写入。WSL2 中启动的
原生模式建议继续在同一 WSL2 shell 使用 Bash 启动器。直接调用 `docker compose up` 会绕过进程交接。

| 数据层 | 共享方式 |
| --- | --- |
| PostgreSQL/Redis/NATS | 同一 `campusos-dev` Compose 项目和命名卷 |
| CampusOS `data/` | 同一宿主 Git 工作区，Docker API 通过 `/workspace` bind mount 访问 |
| 源码 | 同一宿主工作区 |
| Go/pnpm cache、`node_modules` | 各模式独立；不属于需迁移的业务数据 |

`CAMPUSOS_DEV_INFRA_MODE=legacy` 仅用于兼容旧 `docker-compose.yml` 基础设施；它与 `campusos-dev`
命名卷不共享，不能用于“无缝切换”。升级前的 Legacy PostgreSQL 数据不会被启动器自动合并；需要保留时
先备份、再恢复并核对数量，禁止两个 PostgreSQL 容器并发挂载同一物理卷。

默认端口：

| 服务 | 应用端口 | 开发基础设施宿主端口 |
| --- | --- | --- |
| Web/Admin/Docs/API | `3000/3001/3002/8080` | 同左 |
| PostgreSQL | 容器 `5432` | `55432` |
| Redis | 容器 `6379` | `56379` |
| NATS | 容器 `4222` | `54222` |

默认只绑定 `127.0.0.1`。需要局域网联调时显式修改个人开发 env，且不得继续使用固定开发凭据。

## 9. 备份、恢复与主机迁移

旧主机：

```bash
./scripts/docker-deploy.sh backup
```

Windows 使用：

```powershell
.\scripts\docker-deploy.ps1 backup
```

安全保存：

- `backups/campusos-backup-*.tar.gz`；
- `.env.docker`；
- 当前镜像版本或对应不可变 Git 提交。

备份脚本把可配置的容器绝对路径归一化为固定 `modules/` 与 `data/*` 布局，避免恢复结果依赖原主机目录。
API 镜像锁定 PostgreSQL 16 客户端，确保 `pg_dump` 与数据库主版本一致。Unix 归档权限自动设为 `0600`；
Windows 应使用只允许当前管理员读取的目录 ACL。

新主机检出目标版本并把归档放入 `backups/`。先恢复原 `.env.docker`，再执行：

```bash
./scripts/docker-deploy.sh restore backups/<archive>.tar.gz --confirm
```

PowerShell：

```powershell
.\scripts\docker-deploy.ps1 restore -Archive .\backups\<archive>.tar.gz -Confirm
```

流程会校验 archive/checksum、安全路径和格式；旧 API 运行时自动生成恢复前备份；随后停止有写入能力的
API/Web/Admin，恢复 `data/` 和 PostgreSQL，再启动 API 执行向前 migration。维护容器只在显式恢复时运行，
以 root 读取可能为 `0600` 的归档，但仅保留 `DAC_OVERRIDE`、`CHOWN`、`FOWNER` 三项文件恢复能力；
`backups/` 只读，且没有 privileged、Docker socket 或 edge 网络。

数据库与文件卷不能跨介质原子提交，所以源主机和恢复前备份必须保留到验收完成。Redis/NATS 在新主机从
空运行状态开始，可靠业务事实以 PostgreSQL Outbox/Receipt 和核心表为准。

## 10. 更新、停止和回滚

- `docker-deploy.* down`：删除部署容器，保留供一键栈和分组件栈共用的外部网络与外部命名卷。
- `docker-dev.* down`：停止开发栈，保留开发数据库和缓存。
- `docker-dev.* reset --confirm` / `reset -Confirm`：只删除开发项目的命名卷，不删除源码。
- 不提供无确认的部署 `down -v` 包装；删除部署卷必须经过人工备份和独立审批。
- 生产升级先备份、构建新镜像、启动、验收；数据库优先 forward-fix，不盲目执行 down migration。

代码回滚不等于数据回滚。恢复旧归档前应核对目标二进制的 migration 兼容范围、插件数据版本和资源包
checksum。

## 11. 正式环境加固

1. 设置 `CAMPUSOS_ENV=production`。
2. 保留 `API_BIND=127.0.0.1`，数据库和消息服务不暴露公网。
3. 在外层反向代理终止 TLS，并分别限制 Web、Admin、Docs 的网络访问。
4. 配置真实 SMTP，不在日志或镜像保存授权码。
5. 首次用 Bootstrap Secret 登录后改密、启用 MFA，再轮换一次性 Bootstrap Secret。
6. 备份至少保留一份异机副本，并实际执行恢复演练。
7. 连接外部 Prometheus/告警系统，检查可靠任务 backlog、dead-letter 和邮件投递。
8. 不启用 Docker socket、privileged、任意宿主目录或数据库直连插件。

## 12. 验收命令

静态合同：

```bash
make docker-deploy-check
```

开发栈启动后：

```bash
curl -fsS http://127.0.0.1:8080/api/v1/health
./scripts/docker-dev.sh test
```

发布候选仍需执行：

```bash
RUN_RESTORE_DRILL=true RUN_BROWSER_SMOKE=true make release-check
```

Docker 构建成功不等于 SMTP、TLS、外部告警、真实生产负载或跨主机恢复已经通过。

## 13. 外部文档

- 文档站：[Docker 跨平台开发](../../../docs-site/deployment/docker-development.md)
- 文档站：[Docker 单主机部署与迁移](../../../docs-site/deployment/docker.md)
- [初始管理员安全](v12部署与初始管理员安全.md)
- [备份恢复说明](备份恢复说明.md)
- [v13 容量基线与回归门禁](v13容量基线与回归门禁.md)
