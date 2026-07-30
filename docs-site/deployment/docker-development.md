# Docker 跨平台开发环境

这套开发环境让 Windows 和 Linux 开发者只安装 Git、Docker 与 Compose v2，就能修改并运行 CampusOS。
Go、Node.js、pnpm、PostgreSQL、Redis 和 NATS 都在 Linux 容器中，不依赖 Ubuntu 22.04 宿主。

## 与部署栈的区别

| 项目 | `compose.dev.yml` | `compose.deploy.yml` |
| --- | --- | --- |
| 用途 | 修改源码、热更新、测试 | 单主机运行和迁移 |
| 源码 | 绑定挂载 | 构建进不可变镜像 |
| Web/Admin/Docs | Vite/VitePress 开发服务器 | Nginx 静态产物 |
| API | 源码变化后轮询重编译 | 已编译二进制 |
| 数据 | 开发数据库命名卷，`data/` 来自工作区 | 全部运行数据使用命名卷 |
| 密钥 | 固定的 loopback 开发值 | `.env.docker` 随机生成 |

不要把开发 Compose 用于公网或共享生产环境。

## 准备宿主机

两种 Docker 方式都不会替你获取或长期保存源码。先在宿主机执行 `git clone`，再用 VS Code、JetBrains
IDE 或其他编辑器修改这份工作区。开发 Compose 的挂载关系是：

| 宿主目录 | 容器目录 | 用途 |
| --- | --- | --- |
| 仓库根目录 | `/workspace` | API、migration、module、插件、资源和 `data/` |
| `web/` | `/workspace/web` | 用户前台 HMR |
| `admin/` | `/workspace/admin` | 管理端 HMR |
| `docs-site/` | `/workspace/docs-site` | 官方文档 HMR |

容器不会把改动同步回 GitHub；Git 分支、提交和推送仍在宿主工作区完成。`docker-dev.* shell` 主要用于诊断，
不要只在容器可写层保存源码，因为重建容器后该层会消失。

### Windows

1. 安装 Git；管理员 PowerShell 可用 `wsl --install --no-distribution` 启用 WSL 2，按提示重启。
2. 安装并启动 Docker Desktop，在设置中启用 WSL2 backend 和 Linux Containers。
3. 给仓库所在磁盘开启文件共享。
4. 用 `docker context show` 确认当前为 `desktop-linux`，再运行 `docker info`。
5. 推荐把仓库克隆到 WSL2 文件系统以获得更快的绑定挂载。

### Linux

安装 Docker Engine 和 Compose v2，并让当前用户有权访问 Docker daemon。发行版可以是 Ubuntu、Debian、
Fedora、RHEL、Arch 等；项目不依赖 Ubuntu 22.04 的宿主库。

检查：

```bash
docker version
docker compose version
git version
```

## 首次配置和启动

Linux：

```bash
./scripts/docker-dev.sh setup
# 编辑 deploy/docker/.env.dev.local
./scripts/docker-dev.sh setup --start
```

Windows PowerShell：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\docker-dev.ps1 setup
# 编辑 deploy/docker/.env.dev.local
.\scripts\docker-dev.ps1 setup -Start
```

第一次 `setup` 生成受 Git 忽略的 `deploy/docker/.env.dev.local`，不会启动容器。填写后再次执行：

- 如果根 `.env` 已存在，只按白名单导入数据库账号、JWT/认证和 `EMAIL_*`，不导入旧 DSN/端口且不打印值；
- 检查所有宿主端口合法且不冲突；
- 强制开发栈使用 `127.0.0.1`，避免固定开发凭据暴露到网络；
- 校验 `fake`/`smtp` Provider 和 SMTP 必填字段；
- 执行 Compose 静态配置检查；
- 不显示 SMTP 密码。

默认 `EMAIL_PROVIDER=fake` 可以运行系统，但不会发送或打印验证码。需要从页面完成注册、密码找回或邮箱
绑定时，应配置 SMTP；只开发 API 或使用预置开发管理员时可以保留 `fake`。
自动导入只是一次兼容迁移；创建后以 `.env.dev.local` 为两种共享开发模式的配置事实源。

后续启动直接使用：

```bash
./scripts/docker-dev.sh up
```

PowerShell：

```powershell
.\scripts\docker-dev.ps1 up
```

修改 `.env.dev.local` 后必须重新执行上述 `up`。启动脚本使用 `docker compose up -d --build`，会按新配置
重建受影响容器；仅执行 `docker restart` 不会重新读取环境文件。普通端口、SMTP、JWT 等项目配置不需要
重启 Docker Desktop，只有修改 Docker Desktop 自身代理、DNS 或引擎设置时才需要重启 Desktop。

`POSTGRES_DB`、`POSTGRES_USER`、`POSTGRES_PASSWORD` 是 PostgreSQL 首次初始化变量；已有数据卷不会因为
重建容器自动修改数据库内部账号。不要为了让配置“生效”直接删除数据卷，应先备份并按数据库迁移流程处理。

也可以在任意平台使用已经生成的配置直接调用标准 Compose：

```bash
docker compose \
  --env-file deploy/docker/.env.dev.local \
  -f compose.dev.yml \
  up -d --build --wait --wait-timeout 600
```

首次构建会下载 Go module 与三个前端的锁定依赖，后续使用 Docker 构建缓存和命名卷。
Linux/POSIX 启动脚本会把当前宿主 UID/GID 传入 API、Web、Admin 和 Docs；Windows 脚本使用
Docker Desktop/WSL2 常见的 `1000:1000`。应用进程因此不会用 root 所有权写入绑定源码。
Corepack 缓存固定在镜像内并锁定 pnpm `8.15.9`，运行期不会因切换用户而下载“最新 pnpm”。

### 可信局域网访问

默认所有端口只监听 `127.0.0.1`。需要让同一可信局域网内的设备访问 Web、Admin 和 Docs 时，在
`deploy/docker/.env.dev.local` 设置：

```dotenv
CAMPUSOS_DEV_BIND=127.0.0.1
CAMPUSOS_DEV_ALLOW_LAN=true
CAMPUSOS_DEV_WEB_BIND=0.0.0.0
CAMPUSOS_DEV_ADMIN_BIND=0.0.0.0
CAMPUSOS_DEV_DOCS_BIND=0.0.0.0
```

不要把 `CAMPUSOS_DEV_BIND` 改为 `0.0.0.0`。它继续控制 API、PostgreSQL、Redis、NATS 和 pgAdmin，
启动器也会拒绝非 loopback 值。浏览器通过 Web/Admin 的同源 Vite 代理访问 API，因此无需暴露 8080。
Admin 中指向 Web/Docs 的本地 companion URL 会自动换成浏览器正在访问的局域网主机名。

修改后运行 `docker-dev.* up`。如果镜像已经存在且 Docker Hub 暂时不可用，可以只重建三个 UI 容器：

```powershell
docker compose `
  --env-file deploy/docker/.env.dev.local `
  -f compose.dev.yml `
  up -d --no-build --no-deps --force-recreate web admin docs
```

Windows 还需要在管理员 PowerShell 中为“专用网络”添加窄范围防火墙规则：

```powershell
New-NetFirewallRule `
  -DisplayName "CampusOS Dev UI (Private LAN)" `
  -Direction Inbound `
  -Action Allow `
  -Protocol TCP `
  -LocalPort 3000-3002 `
  -Profile Private `
  -RemoteAddress LocalSubnet
```

用 `ipconfig` 查询开发机 IPv4 地址，局域网设备分别访问
`http://<开发机IPv4>:3000`、`:3001`、`:3002`。若仍无法连接，检查 Windows 当前网络是否为“专用”、
路由器/AP 是否开启客户端隔离，以及两台设备是否处于可互访网段。不要配置公网端口转发；Admin 3001
只应在可信测试网络短期开放。关闭局域网访问时把三个 UI bind 和 opt-in 恢复为模板默认值，并删除规则：

```powershell
Remove-NetFirewallRule -DisplayName "CampusOS Dev UI (Private LAN)"
```

| 服务 | 地址 |
| --- | --- |
| 用户前台 | `http://localhost:3000` |
| 管理后台 | `http://localhost:3001` |
| 官方文档 | `http://localhost:3002` |
| API | `http://localhost:8080/api/v1` |
| PostgreSQL | `127.0.0.1:55432` |
| Redis | `127.0.0.1:56379` |
| NATS | `127.0.0.1:54222` |

开发管理员：

```text
邮箱：admin@campusos.local
密码：Admin@123456
```

这些固定值只允许在 loopback 开发栈使用。

### Windows Docker Hub 代理

如果构建出现：

```text
failed to fetch oauth token
Post "https://auth.docker.io/token"
```

这是 Docker Hub 网络、DNS 或代理故障，不是 CampusOS Dockerfile 错误。先确认 Windows 系统代理或
Docker Desktop “Settings → Resources → Proxies”中的代理可用。例如代理监听
`127.0.0.1:7897` 时，保持代理程序运行，并让 Docker Desktop/Containers proxy 使用 System proxy
或对应的手工 HTTP/HTTPS proxy。

修改代理后重启 Docker Desktop，并用小镜像验证：

```powershell
docker desktop restart
docker pull hello-world:latest
```

只有小镜像成功拉取后才重新执行 `docker-dev.ps1 up`。`docker info` 显示
`http.docker.internal:3128` 属于 Docker Desktop 内部转发地址，不代表宿主代理端口丢失。不要把临时
Docker Hub CDN IP 固定到 Windows hosts。

## 与原生 `make dev-all` 无缝切换

完成 `docker-dev.* setup` 后，`deploy/docker/.env.dev.local` 同时成为完整 Docker 模式和共享基础设施模式的
开发配置。宿主机已经安装 Go、Node.js 与 pnpm 时，可切到原生应用进程：

```bash
STOP_EXISTING=true make dev-all
```

脚本会按以下顺序交接：

1. 停止 `campusos-dev` 项目中的 API、Web、Admin 和 Docs 容器，但保留 PostgreSQL、Redis、NATS。
2. 从同一份 Docker 开发配置读取数据库映射端口、SMTP、JWT、Challenge、Session 和 MFA 设置。
3. 在同一个 PostgreSQL 容器中执行 migration。
4. 从宿主工作区启动 API 和三个前端，并记录 `.campusos/run/native-dev.pid`。

切回完整 Docker 模式时，在另一个终端执行：

```bash
./scripts/docker-dev.sh up
```

PowerShell 使用 `.\scripts\docker-dev.ps1 up`。启动器只会停止 PID 文件中已核验为
`scripts/start-dev.sh` 的 CampusOS 进程，不会按端口盲目终止任意程序。Windows 上从 WSL2 启动原生模式时，
建议也从同一个 WSL2 终端运行 Bash 版 Docker 启动器。

| 数据 | 两种模式是否共享 | 实现 |
| --- | --- | --- |
| PostgreSQL 业务事实 | 是 | 同一 `campusos-dev` PostgreSQL 容器和命名卷 |
| Redis/NATS 状态 | 是 | 同一基础设施容器和命名卷 |
| 用户文件、插件数据、模块数据和资源 | 是 | 两种 API 都读取宿主仓库的 `data/` |
| 源码 | 是 | 宿主工作区；Docker 模式使用 bind mount |
| Go/pnpm 构建缓存和 `node_modules` | 否，也不需要共享 | Docker 使用专用卷，原生模式使用宿主工具链 |

默认应用端口相同，因此这是单写入者的安全交接，不支持同时运行两套 API。直接调用标准
`docker compose up` 会绕过 PID 交接，应优先使用 `docker-dev.* up`。只有需要旧的独立
`docker-compose.yml` 基础设施时才运行：

```bash
CAMPUSOS_DEV_INFRA_MODE=legacy STOP_EXISTING=true make dev-all
```

Legacy 模式的数据不与 `campusos-dev` 命名卷共享。
如果升级前一直使用旧 `docker-compose.yml`，第一次切换不会自动合并两套 PostgreSQL；应先按
[备份与恢复](/operations/recovery) 导出并验证，再导入 `campusos-dev`，不要让两个 PostgreSQL 容器同时
挂载同一卷。

## 修改代码后会发生什么

### Go 后端

`api` 容器每秒检查 Go 源码、migration 和 module descriptor：

1. 先构建新的候选二进制；
2. 再执行向前 migration；
3. 两者都成功后，优雅停止旧进程并启动新进程；
4. 构建或 migration 失败时保留上一次成功进程，并在日志中显示错误。

查看日志：

```bash
./scripts/docker-dev.sh logs api
```

### Web、Admin 与 Docs

三个前端使用独立 `node_modules` 卷和锁定依赖种子。源码通过绑定挂载进入容器，Vite/VitePress 使用轮询
监听，因此 Windows Docker Desktop 和 Linux 都能触发 HMR。

修改 `package.json` 或 `pnpm-lock.yaml` 后重新运行：

```bash
./scripts/docker-dev.sh up
```

`up` 自带 `--build`，会重建发生依赖变化的目标并重新播种依赖卷。

## 在没有宿主 Node.js 时增删依赖

以下示例会修改绑定挂载的 `web/package.json` 和 lockfile：

```bash
docker compose \
  --env-file deploy/docker/.env.dev.example \
  -f compose.dev.yml \
  run --rm --no-deps web \
  /usr/local/bin/campusos-dev-frontend pnpm add <package>
```

完成后再次执行 `docker-dev.sh up` 或 PowerShell 的 `docker-dev.ps1 up`。

## 运行测试

全量 Go 测试：

```bash
./scripts/docker-dev.sh test
```

Windows：

```powershell
.\scripts\docker-dev.ps1 test
```

前端示例：

```bash
docker compose \
  --env-file deploy/docker/.env.dev.example \
  -f compose.dev.yml \
  run --rm --no-deps web \
  /usr/local/bin/campusos-dev-frontend pnpm test:component
```

完整发布门禁仍建议在仓库根目录执行 `make release-check`；Docker 开发栈不替代数据库恢复和浏览器发布验收。

## 进入容器

```bash
./scripts/docker-dev.sh shell
```

PowerShell：

```powershell
.\scripts\docker-dev.ps1 shell
```

工作区位于 `/workspace`。Go 和 pnpm 缓存保存在命名卷，不写入宿主 `node_modules`。
直接绕过脚本调用 Compose 时，非 `1000:1000` 的 Linux 开发者应显式传入
`CAMPUSOS_DEV_UID=$(id -u)` 和 `CAMPUSOS_DEV_GID=$(id -g)`。

## 停止与重置

只停止容器并保留数据库和依赖缓存：

```bash
./scripts/docker-dev.sh down
```

Windows：

```powershell
.\scripts\docker-dev.ps1 down
```

删除开发容器和开发命名卷：

```bash
./scripts/docker-dev.sh reset --confirm
```

Windows 对应：

```powershell
.\scripts\docker-dev.ps1 reset -Confirm
```

`down` 和 `reset` 都会先停止仍在运行的受管原生开发进程。`reset` 会删除开发 PostgreSQL、Redis、NATS
和依赖缓存卷，不会删除绑定挂载的源码；工作区 `data/` 仍需按项目规则单独备份和管理。

## Windows 注意事项

- `.gitattributes` 将普通文本、Shell、Dockerfile、YAML 和资源包固定为 UTF-8/LF；Windows 专用
  `.ps1`、`.bat`、`.cmd` 使用 CRLF，二进制文件不转换。
- 支持 EditorConfig 的编辑器会读取 `.editorconfig`。提交前运行
  `python scripts/check-line-endings.py --include-untracked`；需要修复时显式追加 `--fix`，脚本不会暂存。
- 不要在 Windows Containers 模式运行。
- Defender 或索引软件可能降低大型仓库绑定挂载速度；优先把仓库放在 WSL2 文件系统。
- PowerShell 脚本只在当前进程临时放宽执行策略，不要求永久修改系统策略。

## Linux 注意事项

- Fedora/RHEL 等启用 SELinux 的主机若拒绝绑定挂载，应按组织策略给仓库目录授予容器文件上下文，
  不要关闭 SELinux。
- rootless Docker 可以运行开发栈，但宿主端口和绑定挂载权限必须由当前用户拥有。
- 如果曾用旧开发镜像产生 root 所有的 `.vitepress/cache` 或 Vite 缓存，先把该缓存归还给当前用户，
  再重建开发镜像；当前脚本不会继续产生 root 所有文件。
- ARM64 主机依赖上游镜像提供对应架构；CampusOS Go 二进制会按 BuildKit 的目标架构编译。

## 常见问题

**API 显示 VCS ownership 错误**

开发构建已使用 `-buildvcs=false`，避免宿主 Git UID 与容器 UID 不同造成失败。若仍出现该错误，确认使用的是
当前 `compose.dev.yml` 和开发镜像。

**页面打开但 `/api` 失败**

Vite 在容器内必须使用 `CAMPUSOS_API_PROXY_TARGET=http://api:8080`。不要把该值改回容器内的
`localhost:8080`。

**修改代码没有刷新**

确认 `CHOKIDAR_USEPOLLING=true` 未被覆盖，并检查源码目录已授权给 Docker Desktop。

**默认端口冲突**

运行 `docker-dev.* setup` 生成本地配置并修改端口。向导会在启动前报告具体冲突项。

**发送验证码显示成功但没有邮件**

检查配置摘要是否显示 `Email provider: fake`。Fake Provider 不发送邮件；在
`deploy/docker/.env.dev.local` 中配置 `EMAIL_PROVIDER=smtp` 和 `EMAIL_SMTP_*` 后重新运行
`docker-dev.* setup --start`（PowerShell 使用 `setup -Start`），再申请新的验证码。旧 Fake 事件不会补发。

**Windows PowerShell 中输入 `sh` 后没有启动项目**

`sh` 只会尝试进入 POSIX Shell。PowerShell 的项目启动命令是
`.\scripts\docker-dev.ps1 setup -Start` 或 `.\scripts\docker-dev.ps1 up`；误进入 Shell 时使用
`exit` 或 `Ctrl+C` 返回。
