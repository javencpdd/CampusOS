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

### Windows

1. 安装 Git 与 Docker Desktop。
2. 在 Docker Desktop 启用 WSL2 backend 和 Linux Containers。
3. 给仓库所在磁盘开启文件共享。
4. 推荐把仓库克隆到 WSL2 文件系统以获得更快的绑定挂载。

### Linux

安装 Docker Engine 和 Compose v2，并让当前用户有权访问 Docker daemon。发行版可以是 Ubuntu、Debian、
Fedora、RHEL、Arch 等；项目不依赖 Ubuntu 22.04 的宿主库。

检查：

```bash
docker version
docker compose version
git version
```

## 启动

Linux：

```bash
./scripts/docker-dev.sh up
```

Windows PowerShell：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\docker-dev.ps1 up
```

也可以在任意平台直接使用标准 Compose：

```bash
docker compose \
  --env-file deploy/docker/.env.dev.example \
  -f compose.dev.yml \
  up -d --build --wait --wait-timeout 600
```

首次构建会下载 Go module 与三个前端的锁定依赖，后续使用 Docker 构建缓存和命名卷。
Linux/POSIX 启动脚本会把当前宿主 UID/GID 传入 API、Web、Admin 和 Docs；Windows 脚本使用
Docker Desktop/WSL2 常见的 `1000:1000`。应用进程因此不会用 root 所有权写入绑定源码。
Corepack 缓存固定在镜像内并锁定 pnpm `8.15.9`，运行期不会因切换用户而下载“最新 pnpm”。

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

删除开发容器和开发命名卷：

```bash
./scripts/docker-dev.sh reset --confirm
```

Windows 对应：

```powershell
.\scripts\docker-dev.ps1 reset -Confirm
```

`reset` 会删除开发 PostgreSQL、Redis、NATS 和依赖缓存卷，不会删除绑定挂载的源码；工作区 `data/`
仍需按项目规则单独备份和管理。

## Windows 注意事项

- 使用 Git 的默认 LF 规则；仓库 `.gitattributes` 会固定 shell、YAML 和 Dockerfile 的换行。
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

复制 `deploy/docker/.env.dev.example` 到自己的未跟踪文件，修改端口，并设置
`CAMPUSOS_DOCKER_DEV_ENV` 指向它。
