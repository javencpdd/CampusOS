# CampusOS 官方文档

CampusOS 是一个基于 Go、Gin、Vue 3 和 PostgreSQL 的校园社区系统，同时提供用户社区、管理后台、个人空间、受控富文本、个人课表和可扩展插件运行时。

<div class="status-line">
  <code>v0.11.0</code>
  <code>Go + Gin</code>
  <code>Vue 3</code>
  <code>PostgreSQL</code>
  <code>Core / Feature / External / Resource</code>
</div>

<div class="doc-link-grid">
  <a href="/guide/getting-started"><strong>完整入门路径</strong><span>从环境准备、启动验证、模块分类到第一次开发闭环。</span></a>
  <a href="/guide/permission-configuration"><strong>配置角色与权限</strong><span>从权限概念、自定义角色和用户分配，逐步完成板块版主与授权审计。</span></a>
  <a href="/deployment/development"><strong>启动开发环境</strong><span>安装依赖、准备配置、运行 migration 并启动四个开发服务。</span></a>
  <a href="/api/overview"><strong>调用 HTTP API</strong><span>认证、响应包络、错误处理、接口分组和当前契约边界。</span></a>
  <a href="/plugins/schedule-plugin-tutorial"><strong>编写课表插件</strong><span>区分内置课表与外部插件，并完成 Manifest v2、受管数据和发布授权闭环。</span></a>
  <a href="/operations/reliable-tasks"><strong>可靠任务与 Webhook</strong><span>查看持久事件、失败队列、重放边界和安全投递配置。</span></a>
</div>

## 当前界面

下图来自当前 CampusOS 管理端。官方文档站与管理端分开部署，不需要管理员登录，后续可以直接迁移到独立仓库或文档域名。

<img class="product-shot" src="/campusos-admin.png" alt="CampusOS 管理后台登录界面" />

## 最短启动路径

```bash
cp .env.example .env
STOP_EXISTING=true make dev-all
```

默认开发地址：

| 服务 | 地址 |
| --- | --- |
| 用户前台 | `http://localhost:3000` |
| 管理后台 | `http://localhost:3001` |
| 官方文档 | `http://localhost:3002` |
| 后端 API | `http://localhost:8080/api/v1` |

::: warning 当前开发边界
CampusOS 仍处于开发阶段。当前 Docker Compose 主要提供 PostgreSQL、Redis、NATS 和 pgAdmin 等开发依赖，不应把本文中的本地配置直接用于公网生产环境。
当前兼容名称 `runtime: grpc` 表示受管外部进程，Extension/Event 使用受限 loopback HTTP；它不是标准 protobuf gRPC 协议。
:::

## 参与开发

- [GitHub 仓库](https://github.com/javencpdd/CampusOS)
- [完整入门路径](/guide/getting-started)
- [权限配置入门](/guide/permission-configuration)
- [以课表为例编写外部插件](/plugins/schedule-plugin-tutorial)
- [编写第一个插件](/plugins/create-first-plugin)
- [构建与发布](/deployment/release)
