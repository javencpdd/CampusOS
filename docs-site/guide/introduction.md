# 项目介绍

## CampusOS 是什么

CampusOS 面向校园社区场景，把论坛、个人空间和插件平台放在同一套系统中。用户可以按版块发布普通文本帖子或受控富文本文章、回复和管理自己的内容；管理员可以管理用户、版块、插件、集成和平台运行信息。

项目当前发布版本为 `v0.13.0`。v0.7-v0.9 建立模块化单体和扩展边界，v0.10-v0.11 完成内容治理、稳定权限、可靠命令与持久事件；v0.12 交付可信邮箱账号、验证式注册、可撤销 Session、账号恢复、管理员独立准入和结构化图文流程；v0.13 再增加可运营指标、可靠任务告警、管理员准入、TOTP MFA、容量回归门禁和风格包双端交付。旧 API、旧权限表、旧资源来源和 Manifest 仍通过兼容层读取。

## 已实现能力

| 领域 | 当前能力 |
| --- | --- |
| 社区 | 注册登录、版块与标签、普通/富文本帖子、回复楼层、私密内容，以及发布、审核、下架、整改和回收站状态机。 |
| 用户空间 | 公开个人主页、头像、每用户本地空间、风格包，以及通过 Community `ContentQuery` 得到的一致内容视图。 |
| 个人课表与文档 | 管理员治理的春/秋学期、私有课表 Object 版本、第一周、周课表、日历和 Excel/CSV/JSON 导入；私有文档、不可变版本、安全编辑和认证下载。 |
| 管理后台 | 用户与权限、内容治理、版块和版主、扩展与集成、日志和数据库架构说明。 |
| 可靠运行 | 高风险命令事务、持久事件、lease Worker、dead-letter、受控重放、可恢复操作和 Webhook SSRF 防护。 |
| 扩展平台 | Built-in Feature、Wasm 和兼容 `grpc` 名称的受管进程 Runtime，支持 Host API、配置、日志、KV、Catalog 和用户 Grant。 |
| 插件包 | CLI 初始化、检查、打包和安装；Admin 预检、导入、导出和覆盖更新。 |
| 低风险集成 | AI Gateway、Webhook、内部 MCP-like 只读工具和 Message local adapter。 |

## 当前没有承诺的能力

以下项目仍属于后续工作，不能把现有实现描述成已经完成：

- 标准 MCP Server。
- 标准 protobuf gRPC 插件协议；当前 `runtime: grpc` 使用受限 loopback HTTP 合同。
- Discord、OneBot v0.11 等真实外部消息适配器。
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
| `data/resources/` | 无业务 Runtime 的主题、主页包、空间风格、Skill、Prompt 等 Resource Package。 |

## 推荐阅读顺序

1. [完整入门路径](/guide/getting-started)：启动系统并认识 Web、Admin、Docs、API 和四类模块。
2. [系统架构](/guide/architecture)：了解模块和安全边界。
3. [接口约定](/api/overview)：理解认证、响应和错误。
4. [插件体系](/plugins/overview)：区分 Runtime、生命周期和数据目录。
5. [课表插件完整教程](/plugins/schedule-plugin-tutorial)：完成 Manifest v2、受管数据、导入、发布和授权闭环。

## 仓库与许可

源码位于 [GitHub](https://github.com/javencpdd/CampusOS)。具体许可条款以仓库根目录的 `LICENSE` 为准。
