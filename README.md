# CampusOS

CampusOS 是开源的校园社区与扩展平台，提供社区交流、个人内容、知识检索、有据问答、后台治理和可独立发布的外部插件。

> 更新时间：2026-10-30
> 运行版本标识：`v1.1.0-dev`；当前开发主线：`v1.2-dev`。应用、镜像和合同保留统一运行标识，整版 Final 验收仍在进行，尚未宣布正式发布。

## 当前状态

- 社区：注册登录、版块/标签、帖子与回复、富文本、审核和通知；
- 个人空间：头像、50 MB 默认配额、个人文档、图文资源和文章附件；
- 管理端：用户/权限、内容治理、学期、存储配额、运行信息与插件治理；
- 知识与 AI：公开集合检索、本人逐项选择的私人知识、受管模型与引用式问答；私人索引同意不替代模型逐次发送同意；
- 插件 v5：显式签名导入与分域安装、版本审批和配置；隔离 UI 通过受限 Bridge 调用宿主，当前 v1.2 阶段及验收见[主计划](docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md)；
- 当前示例：`plugins/campusos.pdf-viewer`。它只能预览已获业务授权的 PDF，不能取得 JWT、数据库或任意个人空间路径。

Wasm/Container、检查点升级回退、知识与 AI 已有分阶段实现和验收，范围以主计划为准；最新身份/MFA 增量、当前候选联合回归及远端 LAN 验收仍待，不作为整版 Final。自动信任的公共市场、任意插件安装脚本、Office/音视频预览、多节点高可用及自动 TLS 不在当前交付承诺内。

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

首次 `setup` 会创建本地配置。默认 `EMAIL_PROVIDER=fake` 不发送验证码；需要邮件验证时在 `.env.dev.local` 填写 SMTP 配置后再运行 `up`。 插件 Secret 后台换钥默认关闭；启用时须在该本地配置中保留旧读密钥，操作说明见[Docker 开发文档](docs-site/deployment/docker-development.md)。

空管理域首次启动会创建超级管理员 `admin / admin`，可在3001登录后点击“修改密码”；重启保留修改后的密码。需要额外管理员时运行 `python3 scripts/bootstrap-admin.py`，已有账号和系统配置保持原样。管理员 MFA 默认开启；操作见[管理员入门说明](docs/help/系统设计相关/v1.2独立管理员初始化与账号隔离.md)。

| 服务 | 默认地址 |
| --- | --- |
| 用户端 | <http://localhost:3000> |
| 管理端 | <http://localhost:3001> |
| 项目官网与文档 | <http://localhost:3002> |
| API 健康检查 | <http://127.0.0.1:8080/api/v1/health> |
| Plugin UI Gateway | `http://<隔离租约>.localhost:3003`（由宿主签发，通常不直接打开） |

局域网设备使用隔离插件时，还需开放3003网关并配置指向开发机的通配域名；`*.localhost`只适用于开发机本身。运行`docker-dev.* lan-check`可检查端口、隔离域名及父页面来源，配置见[可信局域网访问](docs-site/deployment/docker-development.md#可信局域网访问)。

管理端 **系统与运维 → 运行配置** 可通过复选框和文本框修改 MFA、SMTP、插件来源及 AI 常用参数，也可验证身份后打开完整 env，编辑并同步表单草稿，再保存重启 API；表单隐藏密钥，完整文件包含凭据。非表单项需部署工具应用。详见[后台运行配置](docs-site/deployment/runtime-config.md)。

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
docs-site/                   3002 项目官网与 VitePress 文档站
plugins/                     v5 外部插件源码；受管包存于 PLUGIN_DATA_DIR/v5
data/personal-space/         用户受控文件
data/resources/              无 Runtime 的主题/风格等资源包
docs/                        设计、计划、进度与内部帮助文档
skills/                      仓库内可发现的 Codex Skills 与使用指南
```

`data/plugins/`、`data/plugin_data/` 仅保留历史 v1-v3 兼容资料，不能作为新插件源码或自动发现入口。

## 文档

- [项目官网](docs-site/index.md)与[项目介绍](docs-site/guide/introduction.md)
- [完整入门路径](docs-site/guide/getting-started.md)
- [Docker 跨平台开发环境](docs-site/deployment/docker-development.md)
- [插件 v5 包、签名与界面操作](docs/help/插件v5包与签名操作说明.md)
- [三层授权与资源访问](docs-site/plugins/authorization-v3.md)
- [当前进展与验收范围](docs-site/project/current-roadmap.md)
- [v1.2 主计划](docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md)与[开发进度](docs/进度/v1.2-dev/)
- [仓库文档导航](docs/README.md)

## 开发与贡献

开始前先阅读[贡献与 CI 工作流](docs-site/contributing/workflow.md)。按改动范围运行测试、格式检查和文档链接检查；提交或创建 PR 的 Windows/Git Bash 用法见 `docs/help/github使用相关/`。

许可信息见 [LICENSE](LICENSE)。
