# CampusOS 文档门户

> 更新时间：2026-09-25；当前统一版本：`v1.1.0-dev`（`v1.1-dev` 开发主线）；尚未宣布正式发布。
> 仓库文档：架构、合同、Help、计划和可复验证据
> 官方文档前端：[docs-site](../docs-site/README.md)

仓库根 [README](../README.md) 只提供项目概要和最短启动命令。本目录保存开发和维护所需的详细材料；
第一次接触项目时不要从版本计划或旧故障记录随机开始。

## 1. 新开发者四步入口

1. **认识系统**：[开发者递进入门路线](help/开发者递进入门路线.md)。
2. **启动系统**：[开发、验证与贡献指南](help/系统设计相关/开发运行与验证指南.md)，或
   [Docker 跨平台开发](../docs-site/deployment/docker-development.md)。
3. **理解边界**：[当前架构概览](architecture/当前架构概览.md)、
   [模块与插件边界](../docs-site/guide/module-plugin-resource-boundaries.md)。
4. **完成贡献**：[官方贡献与 CI 工作流](../docs-site/contributing/workflow.md)。

完成这四步后，再根据任务进入 API、插件、权限、数据库或运维专项文档。

## 2. 按任务选择入口

| 任务 | 当前入口 |
| --- | --- |
| 本机 Go/Node 开发 | [开发、验证与贡献指南](help/系统设计相关/开发运行与验证指南.md) |
| Windows/Linux Docker 开发 | [Docker 开发](../docs-site/deployment/docker-development.md) |
| 单主机部署、备份和迁移 | [Docker 部署](../docs-site/deployment/docker.md)、[备份恢复](help/系统设计相关/备份恢复说明.md) |
| 后端模块或数据所有权 | [当前架构](architecture/当前架构概览.md)、[数据库 ER 图与关系说明](../migrations/er/current/CampusOS数据库实体关系说明.md)、[可靠命令与数据所有权](architecture/v0.11可靠命令事件与数据所有权.md) |
| HTTP API | [API 与机器合同](api/README.md)、[接口约定](../docs-site/api/overview.md) |
| Web/Admin 前端 | [官方完整入门](../docs-site/guide/getting-started.md)、[权限配置](../docs-site/guide/permission-configuration.md) |
| External Plugin | [插件体系](../docs-site/plugins/overview.md)、[PDF Viewer v4 实战教程](../docs-site/plugins/pdf-viewer-tutorial.md) |
| Resource Package 或风格包 | [风格包与沙箱 SDK](../docs-site/plugins/style-packs.md)、[双端交付标准](help/系统设计相关/v0.13风格包双端交付标准.md) |
| 权限、版主和审计 | [权限配置入门](../docs-site/guide/permission-configuration.md)、[权限与可靠审计](help/系统设计相关/v0.11权限管理与可靠审计设计入门.md) |
| 可靠任务与 Webhook | [可靠任务运维](../docs-site/operations/reliable-tasks.md)、[故障恢复](help/系统设计相关/v0.13可靠任务指标告警与故障恢复Runbook.md) |
| Skill 使用和维护 | [仓库 Skills 索引](../skills/guides/README.md) |
| 版本历史和未来路线 | [历史阶段导读](help/计划书总结/README.md)、[当前计划治理](计划书总结/README.md) |
| 判断文档是否仍有效 | [Help 生命周期](help/README.md)、[文档审计与整理](help/文档审计与整理说明.md) |
| v1.1 逐项完成情况 | [代码审查与项目回顾](项目计划书v1/项目计划v1.1/01-v1.1代码完成情况审查与项目回顾.md)、[最新开发进度](进度/v1.1-dev/) |
| v1.1 新插件架构规划 | [自包含与动态加载重构计划](项目计划书v1/项目计划v1.1/02-v1.1插件自包含与动态加载重构计划书.md)、[用户配置与授权设计](help/系统设计相关/插件自包含动态加载与用户配置设计.md)；以计划中的最新状态表和进度记录为准 |
| v1.2 下一版规划 | [优化版计划书](项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md)、[细粒度 RBAC](help/系统设计相关/v1.2细粒度授权与资源策略设计.md)、[校园知识库与 RAG](help/系统设计相关/v1.2校园知识库与RAG接入设计.md)；均为规划，未实施 |

## 3. 文档类型和职责

| 路径 | 作用 | 是否作为当前操作入口 |
| --- | --- | --- |
| `help/` | 开发、部署、权限、插件、风格和运维说明 | 需要先看生命周期索引 |
| `api/` | [HTTP、Host API、权限和机器可读合同](api/README.md) | 当前合同优先于手写示例 |
| `architecture/` | [模块边界、数据所有权和安全设计](architecture/README.md) | 当前概览有效；带旧版本号的是决策历史 |
| `../skills/sources/`、`../skills/guides/` | 项目 Skills 的规范源文件与调用维护说明 | 当前；`.agents/skills/` 提供仓库发现入口 |
| [项目计划书v0/](项目计划书v0/README.md) | v0.1-v0.14 原始计划、回顾、输入材料和迁移说明 | 已封版，不是当前待办 |
| [项目计划书v1/](项目计划书v1/README.md) | v1.0 正式计划/审查与 v1.1 新重构计划 | v1.0 为代码收尾/RC；v1.1 旧范围审查不能代替新插件重构验收 |
| `计划书总结/` | 计划有效性、逐版推理和候选路线 | 当前治理入口 |
| `进度/` | [每阶段实现、测试、兼容和回滚证据](进度/README.md) | 对应版本的历史证据 |
| `Todo/` | [未排期需求、思考草稿和样例](Todo/README.md) | 不是产品承诺 |
| `../docs-site/` | 可独立部署的官方教程前端 | 面向普通开发者和使用者 |

## 4. 当前事实如何判断

```text
可执行代码和 migration
  > 生成的 OpenAPI、路由、数据库和资源合同
  > 最新进度验收证据
  > 当前架构和 Help
  > 已封版计划与历史设计
```

`openapi-v0.6-current.yaml`、`http-routes-v0.6.json` 等名称为兼容旧链接保留；文件内容由当前代码生成，
不能根据文件名中的 `v0.6` 判断实现版本。

当前数据库使用单一 `000001_v1_1_schema_baseline`，包含 88 张业务表和 2 张 migration 系统表；旧
`000001-000049` 测试链不再支持原地升级。静态旧表清单不能替代 Admin `/architecture`、`migrations/`、
`make database-check` 和架构同步检查器；完整重构决策见
[v1.0 数据库全面重构方案](项目计划书v1/项目计划v1.0/01-v1.0数据库全面重构方案.md)，索引与约束治理见
[数据库迁移与 Schema 冗余治理](help/系统设计相关/数据库迁移与Schema冗余治理.md)。

## 5. 当前架构和产品边界

- CampusOS 是模块化单体，不是单模块，也没有拆成大量微服务。
- Core Module、Built-in Feature、External Plugin、Resource Package 使用不同生命周期和数据目录。
- `plugins/<key>/` 是 v4 外部插件源码，`plugins/.installed/` 是校验后的不可变发布包；`data/plugins` 与 `data/plugin_data` 保留给旧运行时，不作为 v4 的源码/自动发现入口；
  `data/resources` 保存无 Runtime 的资源包。
- 管理平面使用独立管理员准入事实，但密码凭据仍由 Identity 的 `accounts` 安全拥有。
- 标准 MCP Server、真实 protobuf gRPC、Discord/OneBot 生产 Adapter、远程公共插件市场和完整 Agent
  产品尚未实现。
- Windows 支持路径是 Docker Desktop Linux Containers；当前真实发行证据仍以 Linux `amd64` 为主。

## 6. 版本与计划

v0.1-v0.14 都已退出待执行状态；项目所有者已确认 v0.14 当前代码可作为版本收尾。v1.0 P0/P1 仓库实现已完成审查。
v1.1-dev 已有图文附件、PDF Viewer、自包含 v4 发布包、隔离 UI/Bridge、可信市场申请和用户配置目录等实现；通用 Runtime、跨版本迁移、目标环境浏览器/恢复/性能证据仍未完成，不能宣称 Final：

版本命名规则：面向开发者和使用者的项目计划、进度标题、Help/API/架构文档路径及 UI 展示统一使用
`v0.N`（例如 `v0.14`）。迁移文件名、测试/Make 命令、基线 schema、数据库夹具、API/Host API/Manifest/
CampusDoc 协议版本等技术标识保持其既有写法，例如 `/api/v1`。数据库重构后的 migration 技术编号重新从
`000001_v1_1_schema_baseline` 开始；这不改变产品版本命名。

- [v1.0 插件生态与三层授权体系正式项目计划](项目计划书v1/项目计划v1.0/00-v1.0版本计划书.md)；P0/P1 仓库实现已完成，发布证据待收集
- [v1.0 数据库全面重构方案](项目计划书v1/项目计划v1.0/01-v1.0数据库全面重构方案.md)
- [v1.0 计划逐条审查与项目回顾](项目计划书v1/项目计划v1.0/03-v1.0计划逐条审查与项目回顾.md)
- [v1.0 插件三层授权与运行安全说明](help/系统设计相关/v1.0插件三层授权与运行安全说明.md)
- [v1.0 插件平台数据模型与开发库重置说明](help/系统设计相关/v1.0插件平台数据模型与开发库重置说明.md)
- [v1.1 插件自包含与动态加载重构计划](项目计划书v1/项目计划v1.1/02-v1.1插件自包含与动态加载重构计划书.md)；本轮范围权威，已实现与未决项均记录在状态表
- [v1.2 版本优化计划书](项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md)；下一版规划，承接尚未验收的插件任务并引入受控知识检索设计
- [v1.1 图文附件与 PDF 原始方案](项目计划书v1/项目计划v1.1/00-v1.1版本计划书.md)；保留追溯，已被自包含重构方案取代
- [v1.1 图文附件、PDF 预览插件与授权设计说明](help/系统设计相关/v1.1个人资源预览与授权设计说明.md)
- [v0.14 正式项目计划](项目计划书v0/项目计划v0.14/00-v0.14版本计划书.md)
- [v0.1-v0.14 历史计划目录与迁移清单](项目计划书v0/README.md)
- [v0.14 学期治理与个人文档使用说明](help/系统设计相关/v0.14学期治理与个人文档使用说明.md)
- [v0.1-v0.13 迭代内容与有效性](help/计划书总结/01-v0.1-v0.13迭代内容与有效性.md)
- [当前项目状态与未来规划](help/计划书总结/02-当前项目状态与未来规划.md)
- [v0.13 最终专业审计](项目计划书v0/项目计划v0.13/02-v0.13最终专业审计与后续路线.md)
- [v0.13 进度证据](进度/v0.13-dev/)
- [v0.14 规划、实施与收尾证据](进度/v0.14-dev/)；开发阶段收尾与计划命名统一见
  [v0.14.11-dev](进度/v0.14-dev/v0.14.11-dev.md)，最新历史计划迁移与引用验证见
  [v0.14.14-dev](进度/v0.14-dev/v0.14.14-dev.md)。逐项审查、可恢复对账和发布边界见
  [v0.14.9-dev](进度/v0.14-dev/v0.14.9-dev.md)，P1 原始代码实现和验证证据见
  [v0.14.5-dev](进度/v0.14-dev/v0.14.5-dev.md)。

## 7. 维护规则

1. 一个操作只保留一个当前权威正文；其他位置提供简短入口。
2. API、migration、权限、数据目录或 Manifest 变化时同步更新生成合同和对应文档。
3. 历史证据不静默删除；完全重复且无历史引用的文件可以在记录迁移关系后删除。
4. 规划内容必须明确区分已实现、候选和非目标。
5. 文档变更至少运行：

```bash
make readme-check
make docs-links
cd docs-site && pnpm build
git diff --check
```
