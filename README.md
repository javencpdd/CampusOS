# CampusOS

CampusOS 是一个基于 Go 与 Vue 3 的校园社区系统，提供社区内容、管理后台、个人空间、课表、可治理插件平台和受控集成能力。

## 当前状态

当前发布版本为 `v0.13.0`。v13 在 v12 的可信账号、结构化社区和可靠命令基础上补齐统一错误合同、低基数指标与可靠任务运行闭环、管理员准入、TOTP MFA、容量回归门禁，以及系统主题和个人主页风格包的强制双端交付。

- 内容：普通文本与富文本共用发布、审核、下架、整改、回收站和清除状态合同；公开列表、个人主页和只读集成使用 Community 内容事实源。
- 权限：使用稳定 Permission Code、路由 Operation、全局/板块作用域、自定义角色、越权防护、最后管理员并发保护和授权审计。
- 可靠运行：注册、角色/权限和内容治理高风险写操作可与 required audit、Outbox 同事务提交；持久 Worker 提供 lease、重试、dead-letter、受控重放和可恢复操作记录。
- 运营与安全：管理端提供可靠任务、管理员准入和 MFA 策略；Prometheus 导出默认关闭且只绑定 loopback，容量与迁移/恢复演练进入发布门禁。
- 扩展：区分 External Plugin、Built-in Feature、Resource Package 与 Integration；支持 Wasm 和进程 Runtime、Host API v1/v2、受管数据/文件、用户授权与本地插件目录。
- 前端：Web 与 Admin 按布局能力适配多类视口；分组聚合帖子、二手小数价格、回复/下架通知、50 MB 用户空间与头像历史均由服务端合同约束，核心页面会预加载以避免首次进入模块时的假刷新感。

v13 同时提供 Windows/Linux 通用的 Docker 开发栈和经过门禁检查的单主机 Compose 交付，但仍不提供
生产级高可用、多节点自动故障转移或自动 TLS。标准 MCP Server、标准 protobuf gRPC 扩展协议、真实
Discord/OneBot 生产适配器和远程公共插件市场也仍属于后续范围。历史名为 `runtime: grpc` 的进程 Runtime
当前通过受限 loopback HTTP Extension 合同通信。

## 快速开始

以下是第一次开发的完整路径。推荐使用 Docker 路线，只需准备 Git、Docker Desktop/Engine 和 Compose v2。Windows 必须使用
Docker Desktop 的 WSL2 backend 与 Linux Containers。

### 1. 克隆并进入仓库

```bash
git clone https://github.com/javencpdd/CampusOS.git
cd CampusOS
git status --short
docker compose version
```

Docker 开发仍然需要先在宿主机克隆源码。`compose.dev.yml` 会把仓库、`web/`、`admin/` 和
`docs-site/` 绑定挂载进容器；日常应在宿主机 IDE 中编辑，容器不会自行从 GitHub 同步，也不应作为源码
唯一保存位置。进入容器只用于诊断或执行容器内命令。

### 2. 生成基础配置

Linux、WSL2 或 Git Bash：

```bash
./scripts/docker-dev.sh setup
```

Windows PowerShell：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\docker-dev.ps1 setup
```

脚本会创建已被 Git 忽略的 `deploy/docker/.env.dev.local`；它是 Docker 开发环境的实际配置文件和后续
修改入口。根 `.env.example`、`deploy/docker/.env.example` 和 `.env.dev.example` 都只是其他模式或初始化模板。

- `EMAIL_PROVIDER=fake`：可以开发 API 和使用现有开发账号，但不会发送或显示注册验证码。
- `EMAIL_PROVIDER=smtp`：用于完整测试注册、密码找回和邮箱绑定；填写对应 `EMAIL_SMTP_*`，不要提交密码。

修改后重新执行 `docker-dev.* up`；`docker restart` 不会重读环境文件，只有修改 Desktop 代理/引擎时才重启 Docker Desktop。Docker 模式的 API/Web/Admin/Docs 输出会同时写入 `.campusos/logs/`，可在管理端“平台日志”实时查看。
可信局域网开放 3000–3002 后运行 `docker-dev.* lan-check`；Windows 和 Linux 都会自动识别访问 IP、检查三个服务并给出防火墙/远端验证提示。API/数据服务仍只监听本机，详见 [Docker 开发指南](docs-site/deployment/docker-development.md)。

### 3. 校验配置并启动

Linux、WSL2 或 Git Bash：

```bash
./scripts/docker-dev.sh setup --start
```

Windows PowerShell：

```powershell
.\scripts\docker-dev.ps1 setup -Start
```

向导会检查端口、loopback、SMTP 和 Compose；后续运行 `docker-dev.* up`。Windows 出现 Docker Hub
OAuth/代理错误时按 [Windows 实操报告](docs/help/Windows系统Docker部署实操报告.md) 重启代理链路后再构建。

### 4. 验证服务并阅读官方文档

| 服务 | 地址 |
| --- | --- |
| 用户前台 | `http://localhost:3000` |
| 管理后台 | `http://localhost:3001` |
| 官方文档 | `http://localhost:3002` |
| API | `http://localhost:8080/api/v1` |

```bash
curl -fsS http://localhost:8080/api/v1/health
```

`http://localhost:3002` 是本地运行的官方文档前端。建议按以下顺序学习：

| 阶段 | 本地官方文档 | 文档站未启动时 |
| --- | --- | --- |
| 完整学习路线 | [开发者学习路线](http://localhost:3002/guide/developer-learning-path) | [仓库递进入门路线](docs/help/开发者递进入门路线.md) |
| 认识架构边界 | [系统架构](http://localhost:3002/guide/architecture) | [当前架构概览](docs/architecture/当前架构概览.md) |
| 掌握 Docker 开发 | [Docker 跨平台开发](http://localhost:3002/deployment/docker-development) | [开发运行指南](docs/help/系统设计相关/开发运行与验证指南.md) |
| 编写插件 | [课表插件教程](http://localhost:3002/plugins/schedule-plugin-tutorial) | [插件文档入口](docs/help/插件相关/插件分级与生命周期说明.md) |
| 理解权限 | [权限配置入门](http://localhost:3002/guide/permission-configuration) | [权限与可靠审计](docs/help/系统设计相关/v11权限管理与可靠审计设计入门.md) |
| 准备贡献 | [贡献与 CI 工作流](http://localhost:3002/contributing/workflow) | [开发、验证与贡献指南](docs/help/系统设计相关/开发运行与验证指南.md) |

官方文档后续部署到公网时，开发者只需替换站点域名；仓库内 `docs/` 继续作为离线文档、机器合同和历史
验收证据入口。

### 5. 完成改动并切换开发模式

```bash
git switch -c feat/my-change
./scripts/docker-dev.sh test
git diff --check && python scripts/check-line-endings.py --include-untracked
```

专项检查和 Docker/宿主热更新切换见[贡献与 CI 工作流](docs-site/contributing/workflow.md)和
[Docker 跨平台部署、迁移与开发指南](docs/help/系统设计相关/v13%20Docker跨平台部署、迁移与开发指南.md)。
开发结束后使用 `docker-dev.* down` 停止容器并保留数据。

## 提交改动与创建 Pull Request

`sh/git_commit.sh` 负责状态、暂存、commit 和当前分支 push；`sh/git_pr.sh` 负责检查并创建 PR。切换分支
不需要修改脚本：它们动态读取当前分支，PR base 默认从 `origin/HEAD` 推断。Windows 应使用 Git Bash/
WSL2；PowerShell 可显式调用 Git for Windows：

```powershell
& 'C:\Program Files\Git\bin\bash.exe' ./sh/git_commit.sh -s
& 'C:\Program Files\Git\bin\bash.exe' ./sh/git_pr.sh --help
```

```bash
git branch --show-current
git status --short
./sh/git_commit.sh "docs: document Windows Docker workflow"
gh auth login
gh auth status
./sh/git_pr.sh -t "docs: document Windows Docker workflow" --base main --dry-run
./sh/git_pr.sh -t "docs: document Windows Docker workflow" --base main
```
提交脚本执行 `git add -A`、为新分支设置 upstream，并拒绝直接 push 主干；PR 脚本要求工作区干净并拒绝
head/base 相同。使用其他 remote 时传 `--remote <name>`。完整参数、PR 模板和常见故障见
[Git 提交与 PR 脚本使用说明](docs/help/github使用相关/PR提交脚本使用说明.md)。

## 仓库结构

| 路径 | 作用 |
| --- | --- |
| `cmd/`、`internal/` | 服务入口、模块化单体、领域服务与扩展平台 |
| `modules/`、`internal/modules/` | 编译期 Core/Built-in Feature 描述符与实现；不进入插件安装流程 |
| `web/`、`admin/` | 用户前台与管理后台 |
| `docs-site/`、`docs/` | 对外文档站与仓库内计划、帮助、API、架构和进度证据 |
| `data/plugins/` | External Plugin 实现代码、Manifest 与运行入口 |
| `data/plugin_data/` | External Plugin 私有运行数据与版本快照 |
| `data/module_data/` | Built-in Feature 本地可变数据 |
| `data/resources/` | 主题、主页包、空间风格、Skills、Prompt 等资源包 |
| `data/personal-space/<user_id>/` | 用户文件、图片、课表与插件用户附件 |
| `sdk/`、`examples/plugins/` | Go/TypeScript SDK 与可验证插件示例 |

## 验证

跨平台换行先执行 `python scripts/check-line-endings.py --include-untracked`；完整门禁执行 `make release-check`。验证矩阵见
[开发、验证与贡献指南](docs/help/系统设计相关/开发运行与验证指南.md)。

## 文档

所有文档从 [CampusOS 文档门户](docs/README.md) 进入。高频入口：

| 主题 | 文档 |
| --- | --- |
| 新开发者入门 | [开发者递进入门路线](docs/help/开发者递进入门路线.md)、[官方学习路线](docs-site/guide/developer-learning-path.md) |
| Docker 开发与部署 | [Windows 实操报告](docs/help/Windows系统Docker部署实操报告.md)、[跨平台开发教程](docs-site/deployment/docker-development.md)、[单主机部署与迁移](docs-site/deployment/docker.md) |
| 架构与数据边界 | [当前架构概览](docs/architecture/当前架构概览.md)、[模块与插件边界](docs-site/guide/module-plugin-resource-boundaries.md) |
| HTTP API | [API 索引](docs/api/API索引.md) |
| 权限与可靠审计 | [权限配置入门](docs-site/guide/permission-configuration.md)、[可靠任务与 Webhook](docs-site/operations/reliable-tasks.md) |
| 插件与资源包 | [课表插件完整教程](docs-site/plugins/schedule-plugin-tutorial.md)、[插件体系](docs-site/plugins/overview.md) |
| Help 文档状态 | [Help 索引、历史文档与替代关系](docs/help/README.md) |
| 当前版本与后续 | [v13 最终专业审计](docs/项目计划v13/02-v13最终专业审计与后续路线.md)、[v1-v13 计划总结](docs/计划书总结/README.md)、[公开规划页](docs-site/project/current-roadmap.md) |

## License

见 [LICENSE](LICENSE)。
