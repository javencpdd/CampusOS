# v0.1-v0.14 版本演进

CampusOS 的版本计划是架构决策和验收历史，不是把所有旧愿景同时保留为当前承诺。当前发布基线是
`v0.13.0`；v0.1-v0.12 均已退出执行状态，v0.13 也已经封版。项目所有者已确认 v0.14-dev 当前代码验证完成并完成开发收尾，但仍未通过 Final 发布门禁。

## 演进主线

| 版本 | 核心问题 | 主要结果 |
| --- | --- | --- |
| v0.1 | 建立校园社区平台蓝图 | 身份、社区、事件、权限和插件的早期总体设计 |
| v0.2 | 让计划符合首批真实实现 | Web/Admin 分离，重排 RBAC、API 和插件阶段 |
| v0.3 | 插件隔离和交付 | Wasm、Host API 权限、SDK/CLI 和插件包治理 |
| v0.4 | 扩展平台产品能力 | AI Gateway、Personal Space、风格包和导入闭环；未完成范围转入 v0.5 |
| v0.5 | 从功能转向可运营 | 集成中心、Webhook、MCP-like、Message Local、备份和治理 |
| v0.6 | 统一插件前后端合同 | 动态 UI、Extension Gateway、生命周期和 App Shell 风格包 |
| v0.7 | 内置能力与外部插件分界 | Module Kernel 与 Core/Feature/Plugin/Resource 四分类 |
| v0.8 | 功能域和兼容装配收敛 | 模块目录、Feature 装配、Port 和所有权规则 |
| v0.9 | 多端与本地插件生态 | Manifest v2、Catalog、用户 Grant、受管记录/文件和签名预检 |
| v0.10 | 内容、权限与物理目录治理 | 内容事实、Permission Code、扩展清单、模块/插件/资源物理隔离 |
| v0.11 | 可靠命令与事件 | TxKernel、Outbox、lease/fencing、dead-letter、重放和 Webhook |
| v0.12 | 可信账号和结构化社区 | 邮件验证/恢复、Session、管理员准入、两级板块、互助和二手 |
| v0.13 | 可运营、双端和可移植交付 | 错误合同、指标、MFA、外观双端门禁、响应式、通知/批量治理和 Docker |
| v0.14 | 学期治理与个人工作区基础 | AcademicTerm、对象账本、受管课表、私有文档版本、安全内容编辑核心与受限运营摘要；Final 保持未发布 |

## 如何阅读历史计划

历史计划有三类内容：

1. **正式计划和验收回顾**：用于追溯当时承诺、取舍和证据。
2. **计划思路或提示词**：原始输入，不代表正式范围。
3. **基线与 evidence 文件**：验收附件，不是新的产品计划。

旧计划中的端口、表数量、目录、默认账号和“未来功能”可能已经变化。当前行为应按代码、migration、
生成合同、检查器和最新验收证据判断。

## 当前有效结论

- CampusOS 是模块化单体，不是单模块，也没有拆成大量微服务。
- Core Module、Built-in Feature、External Plugin 和 Resource Package 使用不同生命周期和数据目录。
- 当前数据库包含 49 个追加 migration；静态旧表清单不能替代 Admin 只读架构和数据库检查器。
- v0.14-dev 的 P0/P1 实现可在仓库 `docs/进度/v0.14-dev/v0.14.5-dev.md` 中追溯；当前逐项审查、可恢复对账、预览安全降级 receipt 和文档同步见 `v0.14.9-dev.md`，项目所有者验证后的开发收尾与命名统一见 `v0.14.11-dev.md`。真实历史数据 apply、Linux 目标环境和正式发布审查仍是 Final 门禁。
- `runtime: grpc` 是历史兼容名称，当前进程扩展使用受限 loopback HTTP 合同，不是标准 protobuf gRPC。
- 当前 MCP 能力是受控的 MCP-like 集成，不应描述为完整标准 MCP Server。
- Windows 支持路径是 Docker Desktop 的 Linux Containers；当前真实发行证据仍以 Linux `amd64` 为主。

继续阅读：[文档状态与历史替代](/project/document-lifecycle)、
[当前规划与后续路线](/project/current-roadmap)。
