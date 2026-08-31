# CampusOS Windows 系统 Docker 部署实操报告

> 实操日期：2026-07-30
> 适用基线：CampusOS `v0.13.0`
> 实操类型：Windows 开发环境、Docker Desktop WSL 2 后端、Linux Containers
> 配套入口：[`compose.dev.yml`](../../compose.dev.yml)、[`docker-dev.ps1`](../../scripts/docker-dev.ps1)
> 权威通用指南：[CampusOS v0.13 Docker 跨平台部署、迁移与开发指南](系统设计相关/v0.13%20Docker跨平台部署、迁移与开发指南.md)

## 1. 报告目的和结论

本文记录一台 Windows 开发机从安装 Docker Desktop、启用 WSL 2、生成 CampusOS 本地配置，到解决
Docker Hub 代理故障、启动七个服务、验证 HTTP 入口及安全关闭的完整过程。

本次最终结果：

| 项目 | 实际结果 |
| --- | --- |
| Docker Desktop | `4.84.0` |
| Docker CLI | `29.6.2` |
| Docker Compose | `v5.3.1` |
| Docker 后端 | WSL 2、Linux Containers |
| CampusOS Compose 项目 | `campusos-dev` |
| 服务数量 | 7 个 |
| 健康状态 | PostgreSQL、Redis、NATS、API、Web、Admin、Docs 全部 `healthy` |
| HTTP 验证 | `3000`、`3001`、`3002`、`8080/api/v1/health` 全部返回 `200` |
| 网络处理 | Windows 系统代理 `127.0.0.1:7897`，重启 Docker Desktop 后生效 |
| 邮件方式 | `EMAIL_PROVIDER=fake`，不发送真实验证码 |

这是开发环境实操，不是生产部署验收。开发栈使用固定开发凭据、绑定源码和热更新，只允许绑定
`127.0.0.1`，不能直接暴露到公网或校园局域网。

## 2. 使用的项目文件

Windows 启动过程由仓库文件定义，不需要手工拼接一套新 Compose：

| 文件 | 作用 |
| --- | --- |
| `compose.dev.yml` | 编排开发环境的七个服务、端口、健康检查、绑定挂载和命名卷 |
| `deploy/docker/Dockerfile.dev` | 构建 API、Web、Admin 和 Docs 开发镜像 |
| `deploy/docker/.env.dev.example` | 开发配置模板 |
| `deploy/docker/.env.dev.local` | 本机实际配置；由向导生成并被 Git 忽略 |
| `scripts/docker-dev.ps1` | Windows 的 setup、up、ps、logs、down 等统一入口 |
| `.gitignore` | 排除本机环境变量、日志、运行状态和构建产物 |

`docker-compose.yml` 是旧的独立基础设施兼容入口；当前完整 Docker 开发流程应使用
`compose.dev.yml` 和 `docker-dev.ps1`，不要混用两套 PostgreSQL 数据源。

## 3. 前置条件

### 3.1 Windows 和硬件

建议使用 64 位 Windows 10/11，并确认：

1. BIOS/UEFI 已启用 Intel VT-x 或 AMD-V。
2. Windows 可以启用 WSL 2 和 Virtual Machine Platform。
3. 系统盘有足够空间保存 Docker Desktop、基础镜像、构建缓存和数据库卷。
4. 当前账号可以在安装阶段批准 UAC 管理员授权。

首次构建会下载 Go、Node、PostgreSQL、Redis、NATS 及前端依赖。建议至少给 Docker Desktop 分配
4 核 CPU 和 6 GiB 内存；本次实操引擎可用 16 CPU、约 8 GiB 内存。

### 3.2 项目和网络

确认已安装 Git，并取得宿主机源码：

```powershell
cd C:\Users\19046\Desktop\Code\CampusOS
git status --short --branch
```

如果所在网络不能直接访问 Docker Hub，应先准备可用 HTTP/HTTPS 代理。本次使用 Clash Verge，
监听地址为：

```text
127.0.0.1:7897
```

代理必须在 Docker Desktop 启动和拉取镜像期间持续运行。

## 4. 安装 WSL 2 和 Docker Desktop

### 4.1 推荐安装顺序

以管理员身份打开 PowerShell，先安装 WSL 2 组件：

```powershell
wsl --install --no-distribution
```

如果命令提示需要重启，应先保存其他工作并重启 Windows。重启后检查：

```powershell
wsl --status
wsl --version
```

然后使用 Windows Package Manager 安装 Docker Desktop：

```powershell
winget install `
  --id Docker.DockerDesktop `
  --exact `
  --source winget `
  --accept-package-agreements `
  --accept-source-agreements
```

也可以从
[Docker Desktop Windows 官方安装说明](https://docs.docker.com/desktop/setup/install/windows-install/)
下载安装器。不要使用来源不明的镜像站安装包。

### 4.2 首次启动 Docker Desktop

启动 Docker Desktop 后完成以下设置：

1. 接受 Docker Desktop 许可条款。
2. 选择 WSL 2 backend。
3. 保持 Linux Containers；CampusOS 开发镜像不是 Windows Containers。
4. 等待 Dashboard 显示 Docker Engine 正常运行。

在新的 PowerShell 中验证：

```powershell
docker --version
docker compose version
docker info
docker context show
```

正常情况下，`docker context show` 应显示 `desktop-linux`，`docker info` 的 `OSType` 应为
`linux`。

## 5. 第一次配置 CampusOS

### 5.1 允许当前 PowerShell 运行仓库脚本

仓库中的 PowerShell 脚本受 Git 管理。只对当前终端临时放宽执行策略：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
```

关闭该 PowerShell 后，Process 级设置自动失效，不需要永久修改系统执行策略。

### 5.2 生成本地开发配置

在仓库根目录运行：

```powershell
.\scripts\docker-dev.ps1 setup
```

第一次执行会从模板生成：

```text
deploy/docker/.env.dev.local
```

它包含开发端口、PostgreSQL/Redis 凭据、JWT/Challenge/Session/MFA 开发值和邮件配置。该文件符合
`.env.*.local` 忽略规则，不能提交到 Git。

第一次生成配置后，脚本只显示下一步提示，不会启动容器，这是正常行为。

### 5.3 检查邮件方式

默认配置：

```text
EMAIL_PROVIDER=fake
```

它允许 API 和预置开发账号正常运行，但不会发送、打印或提供注册验证码。需要完整验证注册、密码找回或
邮箱绑定时，应在 `.env.dev.local` 中改为 `smtp` 并填写有效的 `EMAIL_SMTP_*`。

不要把 SMTP 密码写入仓库文档、命令历史或受 Git 跟踪的 `.env.example`。

### 5.4 校验但不启动

再次执行不带 `-Start` 的命令：

```powershell
.\scripts\docker-dev.ps1 setup
```

如果输出包含：

```text
Docker development configuration is valid
Run '.\scripts\docker-dev.ps1 setup -Start' to start now
```

说明配置已通过，但项目仍未启动。

在 PowerShell 中不要额外输入 `sh`。`sh` 表示尝试进入 POSIX Shell，不是 CampusOS 的启动命令；
如果误进入或终端等待，可以输入 `exit` 或按 `Ctrl+C` 返回 PowerShell。

## 6. 构建并启动项目

首次启动：

```powershell
.\scripts\docker-dev.ps1 setup -Start
```

配置已经生成后，日常启动可以直接执行：

```powershell
.\scripts\docker-dev.ps1 up
```

日常 `up` 使用已有开发镜像，Compose 语义相当于：

```powershell
docker compose `
  --env-file deploy/docker/.env.dev.local `
  -f compose.dev.yml `
  up -d --no-build --wait --wait-timeout 600
```

只有首次 `setup -Start`，或依赖、开发 Dockerfile、Compose 构建项、容器入口脚本变化后执行
`.\scripts\docker-dev.ps1 rebuild`，才会附加 `--build` 并访问 Docker Hub/依赖源。

首次构建需要：

1. 拉取 PostgreSQL、Redis、NATS、Go 和 Node 基础镜像。
2. 构建 API 开发镜像。
3. 按各自 lockfile 安装 Web、Admin 和 Docs 依赖种子。
4. 创建命名卷和开发网络。
5. 启动基础设施。
6. 执行数据库迁移并启动 API。
7. 启动三个前端开发服务器。
8. 等待七个服务通过健康检查。

不要在构建尚未结束时反复执行 `setup -Start` 或 `rebuild`，否则会产生相互竞争的 Compose 客户端。

## 7. Docker Hub 代理故障和处理

### 7.1 本次故障

第一次构建在解析 Dockerfile syntax image 时失败：

```text
failed to fetch oauth token
Post "https://auth.docker.io/token"
connectex: A connection attempt failed
```

宿主机 DNS 把 `auth.docker.io` 解析到不可用地址，Docker Desktop 无法从 Docker Hub 获取 OAuth Token。
这是宿主网络、DNS 或代理问题，不是 CampusOS Dockerfile、Go 模块或前端依赖错误。

### 7.2 确认宿主代理可用

本次 Windows 系统代理为：

```text
HTTP/HTTPS proxy: 127.0.0.1:7897
```

可在 Windows“设置 → 网络和 Internet → 代理”中确认，也可以检查当前用户设置：

```powershell
Get-ItemProperty `
  'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' `
  | Select-Object ProxyEnable, ProxyServer
```

预期 `ProxyEnable` 为 `1`，`ProxyServer` 包含 `127.0.0.1:7897`。

### 7.2.1 使用仓库的 Windows 代理脚本

`sh/proxy.sh` 是 Bash 脚本，适用于 Linux、WSL2 和 Git Bash，不应在原生 PowerShell 中直接运行。
Windows 使用 `sh/proxy.ps1`：

```powershell
# 当前 PowerShell 环境变量 + Git 全局代理
. .\sh\proxy.ps1 on

# 同时设置 Windows 当前用户系统代理，供 Docker Desktop 的 System proxy 读取
. .\sh\proxy.ps1 on -SystemProxy

.\sh\proxy.ps1 status
.\sh\proxy.ps1 test

# 清除当前 Shell 设置，并恢复开启前保存的 Git/Windows 系统代理
. .\sh\proxy.ps1 off
```

第一字符 `.` 后面还有一个空格，这是 PowerShell dot-source。若写成 `\.\sh\proxy.ps1 on`，脚本子进程结束后
环境变量不会留在当前终端；Git 和带 `-SystemProxy` 的 Windows 设置仍是持久配置。默认代理是
`http://127.0.0.1:7897`，也可传 `-ProxyHost`、`-Port`。恢复状态保存在被 `.gitignore` 覆盖的
`.campusos/run/proxy-windows-state.json`，不包含代理凭据。Windows 脚本不修改 `~/.ssh/config`。

### 7.3 让 Docker Desktop 重新读取代理

Docker Desktop 可以使用 Windows 系统代理，也可以在 Docker Desktop
“Settings → Resources → Proxies”中手工配置。官方行为见
[Docker Desktop 代理设置](https://docs.docker.com/desktop/settings-and-maintenance/settings/#proxies)。

本次处理方式是保持 Windows 系统代理有效，然后重启 Docker Desktop：

```powershell
docker desktop restart
```

重启完成后先拉取一个很小的官方镜像：

```powershell
docker pull hello-world:latest
```

成功时应看到镜像层下载完成和 Digest。之后再执行：

```powershell
.\scripts\docker-dev.ps1 rebuild
```

这里是在验证“需要构建镜像”的代理链路，因此使用 `rebuild`；日常从已有镜像启动仍使用 `up`。

`docker info` 可能显示：

```text
HTTP Proxy:  http.docker.internal:3128
HTTPS Proxy: http.docker.internal:3128
```

这是 Docker Desktop 内部代理转发地址，不表示 `7897` 配置丢失。Docker Desktop 后端先连接内部代理，
再由它按照宿主系统代理把请求转发到 `127.0.0.1:7897`。

### 7.4 仍然失败时

按以下顺序排查：

1. Clash Verge/Mihomo 是否仍在运行。
2. `127.0.0.1:7897` 是否确实监听 HTTP/HTTPS 代理，而不是只有 SOCKS。
3. 浏览器或 `curl.exe` 是否能通过系统代理访问 `https://registry-1.docker.io/v2/`。
4. Docker Desktop 的 Docker Desktop proxy 和 Containers proxy 是否都使用 System proxy，或手工指向
   `http://127.0.0.1:7897`。
5. 修改代理后是否执行了 `docker desktop restart`。
6. 防火墙、安全软件或校园网络是否拦截 `auth.docker.io`、`registry-1.docker.io`。

不要把某个临时 Docker Hub IP 写入 Windows hosts。Docker Hub 使用动态 CDN 地址，固定 IP 容易失效，
也会绕过正常的负载均衡和证书路由。

## 8. 启动验收

### 8.1 查看七个服务

```powershell
.\scripts\docker-dev.ps1 ps
```

预期服务：

| 服务 | 默认宿主地址 | 预期状态 |
| --- | --- | --- |
| `postgres` | `127.0.0.1:55432` | `healthy` |
| `redis` | `127.0.0.1:56379` | `healthy` |
| `nats` | `127.0.0.1:54222` | `healthy` |
| `api` | `127.0.0.1:8080` | `healthy` |
| `web` | `127.0.0.1:3000` | `healthy` |
| `admin` | `127.0.0.1:3001` | `healthy` |
| `docs` | `127.0.0.1:3002` | `healthy` |

### 8.2 验证 HTTP

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:3000
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:3001
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:3002
Invoke-RestMethod http://127.0.0.1:8080/api/v1/health
```

本次四个入口全部返回 HTTP `200`，API 健康响应包含：

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "service": "CampusOS",
    "status": "ok",
    "version": "0.13.0"
  }
}
```

### 8.3 浏览器入口和开发账号

| 用途 | 地址 |
| --- | --- |
| 用户前台 | <http://localhost:3000> |
| 管理后台 | <http://localhost:3001> |
| 官方文档 | <http://localhost:3002> |
| API 健康检查 | <http://localhost:8080/api/v1/health> |

开发管理员：

```text
邮箱：admin@campusos.local
密码：Admin@123456
```

这些固定值只能用于 loopback 开发栈，不能复用到生产、共享演示或可被其他设备访问的环境。

### 8.4 查看日志

```powershell
# 持续查看 API 日志
.\scripts\docker-dev.ps1 logs api

# 查看其他服务
.\scripts\docker-dev.ps1 logs web
.\scripts\docker-dev.ps1 logs admin
.\scripts\docker-dev.ps1 logs docs
```

日志跟随模式使用 `Ctrl+C` 退出只会停止日志显示，不会关闭容器。

## 9. 日常开发

开发 Compose 把宿主工作区绑定到 Linux 容器：

- 修改 Go 源码后，API 容器轮询构建候选二进制、执行向前迁移并热切换。
- Web、Admin 和 Docs 使用 Vite/VitePress 轮询监听。
- `node_modules`、Go build cache 和 module cache 位于 Docker 命名卷，不写入宿主源码目录。
- PostgreSQL、Redis 和 NATS 使用命名卷；`data/` 中的用户和插件数据来自宿主工作区。

修改 `package.json` 或 lockfile 后重新执行：

```powershell
.\scripts\docker-dev.ps1 rebuild
```

它会使用已有缓存重建受影响镜像并启动。普通源码由热更新加载，不要执行 `rebuild`；已经停止的容器使用
`.\scripts\docker-dev.ps1 up` 从现有镜像启动，不会进入 Docker Hub 构建流程。

## 10. 停止、再次启动和数据边界

### 10.1 停止整个 CampusOS 开发栈

```powershell
.\scripts\docker-dev.ps1 down
```

该命令删除当前 Compose 容器和网络，但保留命名卷及宿主 `data/`，所以下次启动仍能读取原数据库和用户文件。

再次启动：

```powershell
.\scripts\docker-dev.ps1 up
```

### 10.2 只停止应用服务

希望保留 PostgreSQL、Redis 和 NATS 运行时：

```powershell
.\scripts\docker-dev.ps1 stop-apps
```

这适合切换到宿主机原生 Go/Node 开发模式。

### 10.3 退出 Docker Desktop

先执行项目的 `down`，再在系统托盘右键 Docker 图标并选择 **Quit Docker Desktop**。以后重新打开
Docker Desktop 并等待引擎就绪，再执行项目的 `up`。

### 10.4 不要误用 reset

以下命令会删除 `campusos-dev` 容器和命名卷：

```powershell
.\scripts\docker-dev.ps1 reset -Confirm
```

它可能清除开发 PostgreSQL、Redis、NATS 数据，只能在明确要重置本地环境且已完成必要备份时使用。

## 11. 常见现象速查

| 现象 | 含义 | 处理 |
| --- | --- | --- |
| `Docker development configuration is valid` 后返回提示符 | 只做了 setup 校验，没有启动 | 执行 `setup -Start` 或 `up` |
| 误输入 `sh` 后没有 PowerShell 提示符 | 尝试进入 POSIX Shell | 输入 `exit` 或按 `Ctrl+C` |
| `permission denied ... docker_engine` | Docker Desktop 未运行、当前终端状态异常或账号无访问权限 | 启动 Docker Desktop，重新打开 PowerShell并运行 `docker info` |
| `failed to fetch oauth token` | Docker Hub 网络、DNS 或代理故障 | 检查 7897、重启 Docker Desktop、先测试 `docker pull hello-world` |
| `port is already allocated` | 默认端口被其他进程占用 | 停止占用者，或修改 `.env.dev.local` 中对应端口 |
| API 健康但注册收不到验证码 | 默认使用 Fake Email Provider | 配置隔离测试 SMTP 并重新申请验证码 |
| 首次构建很慢 | 正在下载基础镜像和依赖 | 保持网络和代理稳定，不要重复启动 |
| 修改源码没有刷新 | 文件共享、轮询或容器状态异常 | 查看对应服务日志，必要时执行 `up` 重建 |

## 12. Windows 文件格式注意事项

容器内部是 Linux，仓库文件应遵守 `.gitattributes`：

- 仓库文本统一以 UTF-8、LF 存入 Git，Windows 与 Linux 检出的普通文本也保持 LF。
- `.sh`、Dockerfile、YAML 和资源包文本固定为 LF。
- Windows 专用的 `.ps1`、`.bat`、`.cmd` 在工作区使用 CRLF，提交时仍由 Git 规范化。
- 图片、字体、压缩包、Office 文件、WASM、可执行文件和数据库等显式标记为二进制，不做换行转换。
- `.editorconfig` 约束支持 EditorConfig 的编辑器，新建文件默认使用 UTF-8、LF 和文件末尾换行。
- 文件名大小写必须与 import、COPY 和文档链接完全一致；Windows 通常不敏感，Linux 敏感。
- 密钥和本地 `.env` 不进入 Git。

提交前在 PowerShell、Git Bash 或 Linux 终端执行只读检查：

```powershell
python scripts/check-line-endings.py --include-untracked
```

已有工作区需要修复时可以显式执行：

```powershell
python scripts/check-line-endings.py --include-untracked --fix
```

`--fix` 只重写工作区的换行字节，不会执行 `git add`、commit 或 push。修复后再次运行只读检查并审查
`git diff`。不要在包含未完成改动的工作区盲目执行 `git add --renormalize .`，以免把无关文件一起暂存。

首次引入新的 `.gitattributes` 且它尚未提交时，Windows 的 `core.autocrlf` 与索引中的旧规则可能让
`git status` 暂时列出大量只有工作区换行时间戳/格式变化的文件。此时以换行检查、`git diff` 和
`git add --dry-run --all` 审查实际提交范围；新规则提交后重新检出，状态应恢复稳定。不要因此直接删除或
覆盖源码。

出现 `bad interpreter`、资源包 checksum 不一致或 Docker COPY 找不到文件时，应先运行上述检查并确认文件名
大小写，而不是修改容器启动命令。

## 13. 安全和维护要求

1. API、数据库、Redis、NATS 和 pgAdmin 必须继续绑定 `127.0.0.1`；UI 端口只能按下述显式 opt-in 开放。
2. `.env.dev.local`、SMTP 密码和代理凭据不得提交。
3. 不要把固定开发管理员密码用于生产。
4. `EMAIL_PROVIDER=fake` 不是邮件测试工具。
5. 不要同时启动旧 `docker-compose.yml` 和当前 `compose.dev.yml` 的数据库。
6. 停止项目优先使用仓库脚本，不要盲目删除所有 Docker Volume。
7. 代理故障优先修复系统代理或 Docker Desktop 设置，不要固定 Docker Hub hosts IP。
8. 本报告记录一次 Windows 实操；命令合同变化时，以脚本、Compose 和跨平台权威指南为准。

如需可信局域网访问，不要修改控制敏感服务的 `CAMPUSOS_DEV_BIND`。使用
`CAMPUSOS_DEV_ALLOW_LAN=true` 和独立的 `CAMPUSOS_DEV_WEB_BIND`、`CAMPUSOS_DEV_ADMIN_BIND`、
`CAMPUSOS_DEV_DOCS_BIND` 只开放 3000–3002，并仅为 Windows“专用网络”的 `LocalSubnet` 添加防火墙规则。
完整配置、关闭方法和风险边界见[官方 Docker 跨平台开发环境](../../docs-site/deployment/docker-development.md#可信局域网访问)。
启动后运行 `.\scripts\docker-dev.ps1 lan-check`，可自动输出开发机 LAN IPv4、三个访问 URL、容器和
HTTP 探测结果，以及 Windows 网络类别/项目防火墙规则提示。相同诊断也支持 Linux：
`./scripts/docker-dev.sh lan-check` 会识别默认路由网卡和子网，并提示 UFW、firewalld 或
nftables/iptables 检查方法；两种平台都会输出 Windows 与 Linux 远端验证命令。

## 14. 最短复现清单

已经安装并启动 Docker Desktop、系统代理也可用时：

```powershell
cd C:\Users\19046\Desktop\Code\CampusOS
Set-ExecutionPolicy -Scope Process Bypass

.\scripts\docker-dev.ps1 setup
# 首次生成后按需检查 deploy/docker/.env.dev.local

docker desktop restart
docker pull hello-world:latest

.\scripts\docker-dev.ps1 setup -Start
.\scripts\docker-dev.ps1 ps

Invoke-RestMethod http://127.0.0.1:8080/api/v1/health
```

开发结束：

```powershell
.\scripts\docker-dev.ps1 down
```
