# 开发者学习路线

> 适用版本：`v0.13.0`  
> 目标：从第一次启动到完成一份可提交的改动

这是一条按阶段组织的路线，不要求一次读完全部文档。每阶段都有完成标志；达到后再进入下一步。

## 1. 15 分钟：认识系统形态

先读 [项目介绍](/guide/introduction) 和 [系统架构](/guide/architecture)。

你需要知道：

- CampusOS 是模块化单体，通常部署为一个 API 进程，但内部不是单模块。
- Core Module 不可停用；Built-in Feature 可启停但不可普通卸载。
- External Plugin 可独立安装；Resource Package 只包含主题、风格、Skill、Prompt 等资源。
- Web、Admin 和 Docs 是三个独立前端。

完成标志：能够解释为什么课表属于 Built-in Feature、风格包属于 Resource Package，而它们不会出现在
外部插件安装列表中。

## 2. 30 分钟：启动完整环境

### Docker 路线

只安装 Git 和 Docker 时使用 [Docker 跨平台开发](/deployment/docker-development)：

```bash
git clone https://github.com/javencpdd/CampusOS.git
cd CampusOS
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

默认 `EMAIL_PROVIDER=fake` 不会发送验证码；需要测试注册、密码找回和邮箱绑定时，在本地配置中填写 SMTP。
源码始终在宿主机工作区编辑；Compose 只是把这份工作区绑定挂载进容器，不会自行从 GitHub 同步。

### 本机工具链路线

已经安装 Go、Node.js 和 pnpm 时：

```bash
STOP_EXISTING=true make dev-all
```

如果已经运行过 `docker-dev.* setup`，该命令会保留并复用 Docker 开发栈的 PostgreSQL、Redis、NATS 和
数据卷，只把 API 与三个前端切换为宿主机进程；执行 `./scripts/docker-dev.sh up` 可切回完整 Docker 模式。

详细步骤见 [完整入门路径](/guide/getting-started) 和 [开发环境](/deployment/development)。

| 服务 | 地址 |
| --- | --- |
| Web | `http://localhost:3000` |
| Admin | `http://localhost:3001` |
| Docs | `http://localhost:3002` |
| API | `http://localhost:8080/api/v1` |

完成标志：四个应用可访问，`GET /api/v1/health` 成功。

## 3. 30 分钟：理解改动归属

依次阅读 [模块与插件边界](/guide/module-plugin-resource-boundaries) 和 [数据目录](/reference/data-layout)。

| 改动类型 | 主要位置 |
| --- | --- |
| 平台完整性和安全边界 | `internal/modules/core/` |
| 编译期可启停功能 | `internal/modules/features/` |
| 可独立安装的扩展 | `data/plugins/<id>/` |
| 插件运行数据 | `data/plugin_data/<id>/` |
| 主题、风格和 Agent 资源 | `data/resources/<type>/<id>/` |
| 用户文件和课表 | `data/personal-space/<user-id>/` |

完成标志：能够为一个新需求选择正确目录，并说明它的生命周期和数据所有者。

## 4. 30 分钟：跟踪一个请求

选择 `GET /api/v1/categories`：

1. 在 `internal/transport/httpapi/` 找到路由。
2. 进入 Community Handler 和 Service。
3. 找到公开 Port 与 Repository。
4. 在 Web 中找到 API 调用。
5. 对照 [接口约定](/api/overview) 和 [当前契约](/api/contracts)。

按钮是否可见不是授权依据。服务端仍会检查身份、Permission Code、所有权或板块作用域。

完成标志：能够从页面请求追踪到路由、业务服务、数据所有者和响应合同。

## 5. 完成第一次改动

开始前：

```bash
git status --short
```

基础验证：

```bash
GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
make docs-links
make architecture-check
git diff --check
```

Web/Admin/Docs 改动还要运行各自的组件测试、lint 或 build。具体矩阵见
[贡献与 CI 工作流](/contributing/workflow)。

完成标志：改动有对应测试、兼容说明和回滚方式，没有通过删除权限、审计或数据约束换取成功。

## 6. 选择专项路线

| 方向 | 阅读顺序 |
| --- | --- |
| External Plugin | [插件体系](/plugins/overview) → [课表插件教程](/plugins/schedule-plugin-tutorial) → [Manifest](/plugins/manifest) → [Host API](/plugins/host-api) |
| 权限和版主 | [权限配置](/guide/permission-configuration) → [版主管理 API](/api/moderation) |
| API 客户端 | [接口约定](/api/overview) → [认证与社区](/api/community) → [当前契约](/api/contracts) |
| 风格包 | [风格包与沙箱 SDK](/plugins/style-packs) → [版本兼容](/plugins/compatibility) |
| 可靠任务 | [可靠任务与 Webhook](/operations/reliable-tasks) → [备份与恢复](/operations/recovery) |
| Docker 交付 | [Docker 部署](/deployment/docker) → [构建与发布](/deployment/release) |

## 7. 提交贡献

```bash
./sh/git_commit.sh "feat: describe the change"
./sh/git_pr.sh -t "feat: describe the change"
```

辅助脚本不是强制入口，也不替代代码审查和 CI。提交前阅读
[贡献与 CI 工作流](/contributing/workflow)。

仓库内更详细的文档状态、历史替代和逐文件审计位于：

```text
docs/help/README.md
docs/help/文档审计与整理说明.md
```
