# CampusOS 官方文档

CampusOS 是一个基于 Go、Gin、Vue 3 和 PostgreSQL 的校园社区系统，同时提供用户社区、管理后台、个人空间、受控富文本、个人课表和可扩展插件运行时。

<div class="status-line">
  <code>v0.6.8-dev</code>
  <code>Go + Gin</code>
  <code>Vue 3</code>
  <code>PostgreSQL</code>
  <code>Built-in / Wasm / gRPC</code>
</div>

<div class="doc-link-grid">
  <a href="/guide/introduction"><strong>了解 CampusOS</strong><span>项目能力、技术边界、仓库结构和建议阅读顺序。</span></a>
  <a href="/deployment/development"><strong>启动开发环境</strong><span>安装依赖、准备配置、运行 migration 并启动四个开发服务。</span></a>
  <a href="/api/overview"><strong>调用 HTTP API</strong><span>认证、响应包络、错误处理、接口分组和当前契约边界。</span></a>
  <a href="/plugins/create-first-plugin"><strong>编写插件</strong><span>从脚手架到 manifest、测试、打包、预检和管理端导入。</span></a>
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
:::

## 参与开发

- [GitHub 仓库](https://github.com/javencpdd/CampusOS)
- [编写第一个插件](/plugins/create-first-plugin)
- [构建与发布](/deployment/release)
