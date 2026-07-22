# CampusOS

CampusOS 是一个基于 Go 与 Vue 3 的校园社区系统，提供社区内容、管理后台、个人空间、课表、可治理插件平台和受控集成能力。

## 当前状态

当前发布版本为 `v0.13.0`。v13 在 v12 的可信账号、结构化社区和可靠命令基础上补齐统一错误合同、低基数指标与可靠任务运行闭环、管理员准入、TOTP MFA、容量回归门禁，以及系统主题和个人主页风格包的强制双端交付。

- 内容：普通文本与富文本共用发布、审核、下架、整改、回收站和清除状态合同；公开列表、个人主页和只读集成使用 Community 内容事实源。
- 权限：使用稳定 Permission Code、路由 Operation、全局/板块作用域、自定义角色、越权防护、最后管理员并发保护和授权审计。
- 可靠运行：注册、角色/权限和内容治理高风险写操作可与 required audit、Outbox 同事务提交；持久 Worker 提供 lease、重试、dead-letter、受控重放和可恢复操作记录。
- 运营与安全：管理端提供可靠任务、管理员准入和 MFA 策略；Prometheus 导出默认关闭且只绑定 loopback，容量与迁移/恢复演练进入发布门禁。
- 扩展：区分 External Plugin、Built-in Feature、Resource Package 与 Integration；支持 Wasm 和进程 Runtime、Host API v1/v2、受管数据/文件、用户授权与本地插件目录。
- 前端：Web 与 Admin 按布局能力适配手机、平板、横屏和桌面；课表、内容治理、插件中心和权限页纳入七视口回归。

当前仍不提供标准 MCP Server、标准 protobuf gRPC 扩展协议、真实 Discord/OneBot 生产适配器、远程公共插件市场或生产级高可用部署。历史名为 `runtime: grpc` 的进程 Runtime 当前通过受限 loopback HTTP Extension 合同通信。

## 快速开始

需要 Go、Node.js + pnpm、Docker Compose，以及可用的 PostgreSQL、Redis 和 NATS 开发环境。

```bash
cp .env.example .env
STOP_EXISTING=true make dev-all
```

| 服务 | 地址 |
| --- | --- |
| 用户前台 | `http://localhost:3000` |
| 管理后台 | `http://localhost:3001` |
| 官方文档 | `http://localhost:3002` |
| API | `http://localhost:8080/api/v1` |

环境变量、开发账号、数据库迁移、手动启动和故障排查见 [开发、验证与贡献指南](docs/help/系统设计相关/开发运行与验证指南.md)。

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

```bash
make release-check
```

完整门禁、恢复演练和贡献脚本说明见 [开发、验证与贡献指南](docs/help/系统设计相关/开发运行与验证指南.md)。

## 文档

所有文档从 [CampusOS 文档门户](docs/README.md) 进入。高频入口：

| 主题 | 文档 |
| --- | --- |
| 新开发者入门 | [官方文档完整入门路径](docs-site/guide/getting-started.md) |
| 架构与数据边界 | [当前架构概览](docs/architecture/当前架构概览.md)、[v11 可靠命令与数据所有权](docs/architecture/v11可靠命令事件与数据所有权.md)、[v10 模块/插件/资源物理隔离](docs/architecture/v10模块插件资源物理隔离.md) |
| HTTP API | [API 索引](docs/api/API索引.md) |
| 权限与可靠审计 | [v11 权限管理与可靠审计设计入门](docs/help/系统设计相关/v11权限管理与可靠审计设计入门.md)、[可靠任务与 Webhook 安全运维](docs/help/系统设计相关/v11可靠任务与Webhook安全运维.md) |
| 插件与资源包 | [课表插件完整教程](docs-site/plugins/schedule-plugin-tutorial.md)、[插件分级与生命周期说明](docs/help/插件相关/插件分级与生命周期说明.md)、[插件市场与受管数据](docs/help/插件相关/插件市场与受管数据-v0.9.md) |
| 当前版本 | [v13 计划书](docs/项目计划v13/00-v13版本计划书.md)、[v13 实施回顾与 v14 进入条件](docs/项目计划v13/01-v13实施回顾与v14进入条件.md)、[v0.13 进度记录](docs/进度/v0.13-dev/) |
| 历史封版与后续 | [v10 最终审计与后续路线](docs/项目计划v10/03-v10最终全方位审计与后续路线.md)、[v12 进入条件](docs/项目计划v11/01-v11实施回顾与v12进入条件.md) |

## License

见 [LICENSE](LICENSE)。
