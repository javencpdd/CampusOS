# CampusOS 官方文档

> 更新时间：2026-09-25（Asia/Shanghai）

CampusOS 是一个基于 Go、Gin、Vue 3 和 PostgreSQL 的校园社区系统，提供用户社区、管理后台、个人空间、
受控图文文章、课表，以及可治理的外部插件平台。

<div class="status-line">
  <code>开发运行版本 v1.1.0-dev</code>
  <code>当前开发主线 v1.1-dev</code>
  <code>Go + Gin</code>
  <code>Vue 3</code>
  <code>PostgreSQL</code>
  <code>Core / Feature / External / Resource</code>
</div>

<div class="doc-link-grid">
  <a href="/guide/developer-learning-path"><strong>开发者学习路线</strong><span>按阶段完成认识架构、启动环境、跟踪请求、首次改动和提交贡献。</span></a>
  <a href="/guide/permission-configuration"><strong>配置角色与权限</strong><span>从权限概念、自定义角色和用户分配，逐步完成板块版主与授权审计。</span></a>
  <a href="/deployment/development"><strong>启动开发环境</strong><span>安装依赖、准备配置、运行 migration 并启动四个开发服务。</span></a>
  <a href="/api/overview"><strong>调用 HTTP API</strong><span>认证、响应包络、错误处理、接口分组和当前契约边界。</span></a>
  <a href="/plugins/pdf-viewer-tutorial"><strong>编写第一个 v4 插件</strong><span>以自包含 PDF Viewer 学习 Manifest、隔离 UI、Bridge、三层授权和用户配置目录。</span></a>
  <a href="/operations/reliable-tasks"><strong>可靠任务与 Webhook</strong><span>查看持久事件、失败队列、重放边界和安全投递配置。</span></a>
  <a href="/project/current-roadmap"><strong>查看项目规划</strong><span>了解 v1.1-dev 的附件、PDF Viewer 与自包含插件重构状态，以及仍需完成的验收边界。</span></a>
</div>

## 当前界面

下图来自当前 CampusOS 管理端。官方文档站与管理端分开部署，不需要管理员登录，后续可以直接迁移到独立仓库或文档域名。

<img class="product-shot" src="/campusos-admin.png" alt="CampusOS 管理后台登录界面" />

## 最短启动路径

```bash
git clone https://github.com/javencpdd/CampusOS.git
cd CampusOS
./scripts/docker-dev.sh setup
# 编辑 deploy/docker/.env.dev.local 后：
./scripts/docker-dev.sh setup --start
```

源码保存在宿主工作区并绑定挂载进开发容器。安装 Go、Node.js 与 pnpm 后，也可用
`STOP_EXISTING=true make dev-all` 在共享同一开发数据卷的前提下切换到原生应用进程。

默认开发地址：

| 服务 | 地址 |
| --- | --- |
| 用户前台 | `http://localhost:3000` |
| 管理后台 | `http://localhost:3001` |
| 官方文档 | `http://localhost:3002` |
| 后端 API | `http://localhost:8080/api/v1` |

::: warning 当前开发边界
当前统一版本为 `v1.1.0-dev`，对应 `v1.1-dev` 图文附件和插件自包含开发主线；应用与部署镜像默认标签保持一致，但这不表示已宣布 `v1.1 Final`。
CampusOS 提供 Docker 开发栈和单主机 Compose 交付，但不包含生产级高可用、自动 TLS 或多节点故障转移。Windows 使用 Docker Desktop
Linux Containers，不支持原生 Windows Containers。`campusos.plugin/v4` 当前提供纯 UI 的 `runtime: none` 实践样例；通用 Wasm/Container Runner 的完整生产验收仍未完成。
:::

## 参与开发

- [GitHub 仓库](https://github.com/javencpdd/CampusOS)
- [完整入门路径](/guide/getting-started)
- [开发者学习路线](/guide/developer-learning-path)
- [贡献、Pull Request 与 CI/CD](/contributing/workflow)
- [权限配置入门](/guide/permission-configuration)
- [以 PDF Viewer 为例编写 v4 外部插件](/plugins/pdf-viewer-tutorial)
- [v4 自包含插件体系](/plugins/overview)
- [构建与发布](/deployment/release)
- [v0.1-v0.14 版本演进](/project/version-evolution)
- [文档状态与历史替代](/project/document-lifecycle)
- [当前规划与后续路线](/project/current-roadmap)
