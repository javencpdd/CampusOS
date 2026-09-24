# CampusOS

CampusOS 是一个面向校园社区的模块化单体，提供社区内容、个人空间、图文文章与附件、后台治理，以及可独立发布的外部插件。

> 更新时间：2026-09-25
> 当前统一版本：`v1.1.0-dev`（`v1.1-dev` 开发主线）；未宣布正式发布前，应用、部署镜像与当前文档使用同一版本号，不代表 `v1.1 Final`。

## 当前状态

- 社区：注册登录、版块/标签、帖子与回复、富文本、审核和通知；
- 个人空间：头像、50 MB 默认配额、个人文档、图文资源和文章附件；
- 管理端：用户/权限、内容治理、学期、存储配额、运行信息与插件治理；
- v4 插件：源码位于 `plugins/<key>/`，仅从校验后的 `plugins/.installed/` 发布包发现；隔离 UI 通过 Bridge 调用宿主；
- 当前示例：`plugins/campusos.pdf-viewer`。它只能预览已获业务授权的 PDF，不能取得 JWT、数据库或任意个人空间路径。

尚未作为正式交付承诺：通用 Wasm/container 插件执行器、自动信任的公共市场、完整插件升级回滚、Office/音视频预览、多节点高可用和自动 TLS。

## 快速开始

需要 Git、Docker Desktop（Windows）或 Docker Engine（Linux）以及 Compose v2。开发配置事实源是 Git 忽略的 `deploy/docker/.env.dev.local`。

Windows PowerShell：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\docker-dev.ps1 setup -Start
```

Linux、WSL2 或 Git Bash：

```bash
./scripts/docker-dev.sh setup --start
```

首次 `setup` 会创建本地配置。默认 `EMAIL_PROVIDER=fake` 不发送验证码；需要邮件验证时在 `.env.dev.local` 填写 SMTP 配置后再运行 `up`。

| 服务 | 默认地址 |
| --- | --- |
| 用户端 | <http://localhost:3000> |
| 管理端 | <http://localhost:3001> |
| 官方文档 | <http://localhost:3002> |
| API 健康检查 | <http://127.0.0.1:8080/api/v1/health> |
| Plugin UI Gateway | <http://localhost:3003>（供隔离插件加载，通常不直接打开） |

普通 Go/Vue/文档源码由开发容器热加载。修改 `.env.dev.local`、端口或挂载后运行 `docker-dev.* up`；修改依赖锁文件、Dockerfile、Compose 构建项、插件前端/Vite/Manifest 或容器入口后运行 `docker-dev.* rebuild`。详细说明见[Docker 跨平台开发环境](docs-site/deployment/docker-development.md)。

停止容器但保留开发数据：

```powershell
.\scripts\docker-dev.ps1 down
```

```bash
./scripts/docker-dev.sh down
```

## 仓库结构

```text
cmd/                         API 入口
internal/                    核心领域、功能模块、HTTP 服务与插件宿主
web/                         用户端 Vue 应用
admin/                       管理端 Vue 应用
docs-site/                   3002 官方 VitePress 文档站
plugins/                     v4 外部插件源码与不可变发布包
data/personal-space/         用户受控文件与用户级插件配置
data/resources/              无 Runtime 的主题/风格等资源包
docs/                        设计、计划、进度与内部帮助文档
skills/                      仓库内可发现的 Codex Skills 与使用指南
```

`data/plugins/`、`data/plugin_data/` 仅保留历史 v1-v3 兼容资料，不能作为新插件源码或自动发现入口。

## 文档

- [官方文档站首页](docs-site/index.md)
- [完整入门路径](docs-site/guide/getting-started.md)
- [Docker 跨平台开发环境](docs-site/deployment/docker-development.md)
- [以 PDF Viewer 学习第一个 v4 外部插件](docs-site/plugins/pdf-viewer-tutorial.md)
- [三层授权与资源访问](docs-site/plugins/authorization-v3.md)
- [v1.1 插件自包含与动态加载重构计划书](docs/项目计划书v1/项目计划v1.1/02-v1.1插件自包含与动态加载重构计划书.md)
- [v1.1 开发进度](docs/进度/v1.1-dev/)
- [仓库文档导航](docs/README.md)

## 开发与贡献

开始前先阅读[贡献与 CI 工作流](docs-site/contributing/workflow.md)。按改动范围运行测试、格式检查和文档链接检查；提交或创建 PR 的 Windows/Git Bash 用法见 `docs/help/github使用相关/`。

许可信息见 [LICENSE](LICENSE)。
