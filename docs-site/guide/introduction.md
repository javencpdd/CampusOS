# 项目介绍

## CampusOS 是什么

CampusOS 面向校园社区场景，把论坛、个人空间和插件平台放在同一套系统中。用户可以按版块发布普通文本帖子或受控富文本文章、回复和管理自己的内容；管理员可以管理用户、版块、插件、集成和平台运行信息。

项目当前开发基线为 `v0.7.2-dev`。v6 计划内工作已完成并封版，v7 已冻结兼容基线并建立使用可选生命周期接口的 Module Registry：启动前检查依赖，按确定拓扑注册和启动，失败时逆序回滚，并接管 EventBus 与 Plugin Platform 基础设施生命周期。现有 API、数据库、插件和前端合同保持兼容。

## 已实现能力

| 领域 | 当前能力 |
| --- | --- |
| 社区 | 注册登录、版块、默认标签、普通帖子、富文本图文文章、回复楼层、私密帖子。 |
| 用户空间 | 公开个人主页、头像、每用户本地空间、风格包、内容同步。 |
| 个人课表 | 独立学期 JSON、第一周设置、周课表、日历和 Excel/CSV/JSON 导入。 |
| 管理后台 | 用户、帖子、版块、版主、插件、集成、日志和数据库架构说明。 |
| 插件运行时 | Built-in、Wasm、gRPC，支持事件、Host API、配置、日志和 KV。 |
| 插件包 | CLI 初始化、检查、打包和安装；Admin 预检、导入、导出和覆盖更新。 |
| 低风险集成 | AI Gateway、Webhook、内部 MCP-like 只读工具和 Message local adapter。 |

## 当前没有承诺的能力

以下项目仍属于后续工作，不能把现有实现描述成已经完成：

- 标准 MCP Server。
- Discord、OneBot v11 等真实外部消息适配器。
- 公网插件市场和自动信任第三方插件。
- 在主页面同源执行任意 JavaScript 的风格包；动态特效只允许使用受筛查的 `sandbox-worker.v1`。
- 完整的容器化生产交付、跨区域高可用或零停机升级。

## 技术组成

```text
web (Vue 3, :3000) ---------\
                              -> API (Go + Gin, :8080) -> PostgreSQL
admin (Vue 3, :3001) -------/                         -> Redis / NATS
docs-site (VitePress, :3002)                          -> data/
```

| 组件 | 作用 |
| --- | --- |
| `cmd/server` | API 服务入口。 |
| `internal/` | Identity、Community、Plugin、Space、Richtext、Schedule 等领域模块。 |
| `web/` | 普通用户前台。 |
| `admin/` | 管理员前台。 |
| `docs-site/` | 可独立迁移的官方文档前端。 |
| `data/plugins/` | 插件实现、manifest 和运行入口。 |
| `data/plugin_data/` | 插件运行数据和可编辑风格包源码。 |
| `data/personal-space/` | 每个用户的本地文件和图片。 |

## 推荐阅读顺序

1. [系统架构](/guide/architecture)：了解模块和安全边界。
2. [开发环境](/deployment/development)：在本机启动完整系统。
3. [接口约定](/api/overview)：理解认证、响应和错误。
4. [插件体系](/plugins/overview)：区分 Runtime、生命周期和数据目录。
5. [编写第一个插件](/plugins/create-first-plugin)：完成一次插件闭环。

## 仓库与许可

源码位于 [GitHub](https://github.com/javencpdd/CampusOS)。具体许可条款以仓库根目录的 `LICENSE` 为准。
