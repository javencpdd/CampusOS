# Docker 单主机部署与迁移

CampusOS v0.13 提供经过仓库门禁检查的单主机 Docker Compose 交付。宿主机不需要安装 Go、Node.js、
PostgreSQL、Redis 或 NATS，只需要 Git、Docker 和 Compose v2。

## 支持边界

| 宿主平台 | 支持方式 |
| --- | --- |
| Windows 10/11 x64 | Docker Desktop，启用 WSL2 和 Linux Containers |
| Linux x64/arm64 | Docker Engine 与 `docker compose` v2 |
| 原生 Windows Containers | 不支持；CampusOS 镜像是 Linux 容器 |
| 多节点、高可用集群 | 不属于 v0.13；当前合同是单主机、单 API 实例 |

“跨平台”表示同一套 Linux 容器可在受支持的 Windows 或 Linux Docker 宿主上运行，不表示兼容所有历史
Windows 版本、32 位主机或没有 Compose v2 的环境。仓库本轮真实整栈证据来自 Linux `amd64`；
Windows Docker Desktop 和 Linux `arm64` 发布前仍应在目标 Runner 执行本页验收清单。

## 交付组成

`compose.deploy.yml` 启动：

- PostgreSQL、Redis、NATS；
- CampusOS API；
- 用户前台、管理后台和官方文档三个 Nginx 容器；
- 可选 pgAdmin；
- 仅恢复时显式运行的 maintenance 容器。

同一组服务还提供独立 Compose 文件：

| 组件 | 独立配置 | 镜像 |
| --- | --- | --- |
| PostgreSQL/Redis/NATS | `deploy/docker/components/compose.infra.yml` | 上游固定主版本镜像 |
| API | `deploy/docker/components/compose.api.yml` | `campusos-api:<tag>` |
| 用户前台 | `deploy/docker/components/compose.web.yml` | `campusos-web:<tag>` |
| 管理后台 | `deploy/docker/components/compose.admin.yml` | `campusos-admin:<tag>` |
| 官方文档 | `deploy/docker/components/compose.docs.yml` | `campusos-docs:<tag>` |

根 `compose.deploy.yml` 通过 `extends` 复用这些服务定义，因此一键配置与独立配置不会维护两份实现。

PostgreSQL、Redis、NATS 和 CampusOS `data/` 使用由脚本显式创建的外部命名卷。API 以 UID `10001`
的非 root 用户运行，不挂载 Docker socket、不使用 privileged 模式；`campusos_backend` 创建为
Docker internal 网络，仅 edge 网络承载 Web/Admin 到 API 的代理通信。Web/Admin 的 `/api` 由各自
Nginx 反向代理到 API。

## 首次启动

### Linux

```bash
./scripts/docker-deploy.sh init
./scripts/docker-deploy.sh config
./scripts/docker-deploy.sh up
```

### Windows PowerShell

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\docker-deploy.ps1 init
.\scripts\docker-deploy.ps1 config
.\scripts\docker-deploy.ps1 up
```

`init` 从 `deploy/docker/.env.example` 生成 `.env.docker`，并为数据库、JWT、Challenge、Session 和 MFA
创建独立随机密钥。该文件被 Git 忽略；不要提交、发送到聊天或放入镜像。

默认地址：

| 服务 | 地址 |
| --- | --- |
| 用户前台 | `http://localhost:3000` |
| 管理后台 | `http://localhost:3001` |
| 官方文档 | `http://localhost:3002` |
| API 健康检查 | `http://127.0.0.1:8080/api/v1/health` |

首次管理员邮箱是 `admin@campusos.local`，密码是 `.env.docker` 中一次性的
`AUTH_BOOTSTRAP_ADMIN_SECRET`。首次登录后立即修改密码并配置 MFA，不要把该值当作长期共享密码。

## 联网前配置

至少检查 `.env.docker`：

```dotenv
CAMPUSOS_ENV=production
HTTP_BIND=127.0.0.1
API_BIND=127.0.0.1
EMAIL_PROVIDER=smtp
EMAIL_SMTP_HOST=smtp.example.edu
EMAIL_SMTP_PORT=587
EMAIL_SMTP_USERNAME=...
EMAIL_SMTP_PASSWORD=...
EMAIL_SMTP_FROM=...
PUBLIC_WEB_URL=https://community.example.edu
PUBLIC_DOCS_URL=https://docs.example.edu
```

Compose 不自动签发 TLS 证书。正式环境应在外层使用 Caddy、Nginx、Traefik 或云负载均衡器终止 TLS，
只将必要的 Web/Admin/Docs 入口暴露到可信网络。API、PostgreSQL、Redis、NATS 和 pgAdmin 默认不应直接
暴露到公网。

## 常用操作

Linux：

```bash
./scripts/docker-deploy.sh ps
./scripts/docker-deploy.sh logs api
./scripts/docker-deploy.sh backup
./scripts/docker-deploy.sh down
```

Windows：

```powershell
.\scripts\docker-deploy.ps1 ps
.\scripts\docker-deploy.ps1 logs -Service api
.\scripts\docker-deploy.ps1 backup
.\scripts\docker-deploy.ps1 down
```

`down` 只停止并删除容器；供一键栈和分组件栈共用的外部网络及外部命名卷都会保留。不要把
`docker compose down -v` 当作日常停止命令。

## 分开构建和启动

先初始化共享密钥和五份组件配置：

```bash
./scripts/docker-component.sh init
```

Windows：

```powershell
.\scripts\docker-component.ps1 init
```

配置分两层：

- `.env.docker`：数据库密码、JWT、Challenge、Session、MFA、SMTP 等跨组件共享值；
- `.env.components/infra.env`、`api.env`、`web.env`、`admin.env`、`docs.env`：组件自己的端口、网络、
  数据卷和公开 URL 覆盖。

不要在五个文件中复制不同的数据库密码。API 与 PostgreSQL 必须读取同一个 `.env.docker`。

按顺序独立启动：

```bash
./scripts/docker-component.sh up infra
./scripts/docker-component.sh up api
./scripts/docker-component.sh up web
./scripts/docker-component.sh up admin
./scripts/docker-component.sh up docs
```

PowerShell：

```powershell
.\scripts\docker-component.ps1 up -Component infra
.\scripts\docker-component.ps1 up -Component api
.\scripts\docker-component.ps1 up -Component web
.\scripts\docker-component.ps1 up -Component admin
.\scripts\docker-component.ps1 up -Component docs
```

只构建一个镜像：

```bash
./scripts/docker-component.sh build api
./scripts/docker-component.sh build web
./scripts/docker-component.sh build admin
./scripts/docker-component.sh build docs
```

前端可以先启动，但 API 尚未健康时 `/api` 会返回代理错误。建议使用
`infra -> api -> web/admin/docs` 的顺序。停止时可以反向执行 `down`；独立停止 Docs 不影响 API，
停止某个前端也不会停止其他前端。

组件通过固定的 Compose 外部网络名和数据卷名协作。脚本会幂等创建 internal 的
`campusos_backend`、普通 edge 网络 `campusos_edge` 以及所需外部命名卷；如果同名 backend 已存在但
不是 internal，启动会拒绝继续，避免数据库隔离在不知情时降级。一键栈与分组件栈默认复用
`campusos_*` 命名卷，所以切换启动方式不会创建一套空数据库。修改网络或卷名时，必须让相关组件使用一致值。

同一时间只能有一个 PostgreSQL 容器挂载生产数据库卷。需要整实例备份或恢复时，先按
`docs -> admin -> web -> api -> infra` 停止分组件栈，再使用同一网络/卷配置启动总编排并执行
`docker-deploy.* backup|restore`；完成后可以再切回分组件模式。不要同时启动总编排 infra 和独立 infra。

## 更新版本

1. 先执行 `backup` 并把归档复制到另一块磁盘。
2. 保存当前 `.env.docker` 的受控副本。
3. 获取经过审查的新源码或发行包。
4. 执行 `build`，再执行 `up`。
5. 检查健康、登录、MFA、邮件、插件和关键内容。

API 启动时会等待 PostgreSQL 并执行尚未应用的向前 migration。不要修改已经执行过的历史 migration，
也不要在没有恢复演练时直接降级数据库。

## 备份

```bash
./scripts/docker-deploy.sh backup
```

```powershell
.\scripts\docker-deploy.ps1 backup
```

归档保存在仓库根目录 `backups/`，包含：

- PostgreSQL 逻辑备份；
- 归一化到固定 `modules/` 与 `data/*` 布局的插件数据、模块数据、资源包和个人空间文件；
- 校验和及元数据。

API 镜像中的 `pg_dump` 主版本与 PostgreSQL 16 锁定一致。Linux/Unix 宿主上的归档会设置为 `0600`；
Windows 依赖当前用户目录 ACL，仍应把 `backups/` 视为敏感目录。归档不包含 `.env.docker`。Redis 与
NATS 是可重建运行状态，不作为跨主机权威备份。

## 恢复与迁移到新主机

迁移时需要两类材料：

1. `backups/campusos-backup-*.tar.gz`；
2. 原主机的 `.env.docker`，通过受控加密渠道单独传输。

保留原密钥才能继续解密既有 MFA 数据和受保护配置。CampusOS Session、Access Token、Refresh Token
不是导出数据；恢复后仍应主动撤销旧会话并重新登录。

在新主机检出目标版本，把归档放入 `backups/`，把密钥文件放到仓库根目录并限制读取权限，然后执行：

```bash
./scripts/docker-deploy.sh restore backups/campusos-backup-20260729T000000Z.tar.gz --confirm
```

Windows：

```powershell
.\scripts\docker-deploy.ps1 restore `
  -Archive .\backups\campusos-backup-20260729T000000Z.tar.gz `
  -Confirm
```

恢复入口会先校验归档。如果旧 API 正在运行，还会自动生成一份恢复前安全备份；随后停止 API/Web/Admin，
恢复 PostgreSQL 与 `data/`，执行向前 migration，并等待整栈健康。恢复不是跨数据库和文件卷的分布式事务，
所以必须保留恢复前备份，验收完成前不要删除源主机。

## 验收清单

```bash
curl -fsS http://127.0.0.1:8080/api/v1/health
```

随后人工确认：

1. 用户前台、管理后台和文档站可以打开。
2. 管理员能够重新登录并完成 MFA。
3. 历史帖子、图片、课表、插件和风格包可以读取。
4. 注册邮件能通过真实 SMTP 投递。
5. 管理端可靠任务没有持续增长的 pending/dead 异常。
6. 备份归档在另一环境完成过恢复演练。

## 常见问题

**Windows 上脚本无法执行**

使用 PowerShell 脚本。它会创建一键栈和分组件栈共用的外部网络及外部命名卷，其中
`campusos_backend` 使用 internal 隔离；直接执行原始 Compose 前也必须按同一属性创建
`campusos_backend` 和 `campusos_edge`。

**端口被占用**

修改 `.env.docker` 中 `WEB_PORT`、`ADMIN_PORT`、`DOCS_PORT` 和 `API_PORT`。

**生产启动拒绝配置**

确认 `CAMPUSOS_ENV=production`，并使用 `init` 生成的非默认 JWT、Bootstrap、Challenge、Session 和 MFA
密钥。不要重新使用仓库开发示例值。

**需要继续修改源码**

部署栈不会绑定源码。请使用 [Docker 跨平台开发环境](/deployment/docker-development)。
