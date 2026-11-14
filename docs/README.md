# CampusOS 文档门户

> 更新时间：2026-10-29；运行版本标识：`v1.1.0-dev`；当前开发主线：`v1.2-dev`，整版 Final 未完成，尚未宣布正式发布。
> 仓库文档：架构、合同、Help、计划和可复验证据
> 项目官网与官方文档：[docs-site](../docs-site/README.md)

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
| 官网、产品与当前进展 | [官网首页](../docs-site/index.md)、[项目介绍](../docs-site/guide/introduction.md)、[当前进展](../docs-site/project/current-roadmap.md) |
| 知识检索与有据问答 | [公开检索](../docs-site/guide/public-knowledge-search.md)、[私人知识](../docs-site/guide/private-knowledge.md)、[有据问答](../docs-site/guide/cited-rag.md) |
| External Plugin | [插件体系](../docs-site/plugins/overview.md)、[插件 v5 操作](help/插件v5包与签名操作说明.md)、[UI Host / Bridge 合同](api/v1.2插件UI宿主与Bridge合同.md) |
| Resource Package 或风格包 | [风格包与沙箱 SDK](../docs-site/plugins/style-packs.md)、[双端交付标准](help/系统设计相关/v0.13风格包双端交付标准.md) |
| 权限、版主和审计 | [权限配置入门](../docs-site/guide/permission-configuration.md)、[权限与可靠审计](help/系统设计相关/v0.11权限管理与可靠审计设计入门.md) |
| 可靠任务与 Webhook | [可靠任务运维](../docs-site/operations/reliable-tasks.md)、[故障恢复](help/系统设计相关/v0.13可靠任务指标告警与故障恢复Runbook.md) |
| NapCat指定群诊断 | [登录、群范围及单条发送/读回](help/系统设计相关/NapCat指定群接入诊断.md)；仅诊断工具；实际群发送/读回与完整QQ插件分别验收 |
| QQ文档助手插件 | [QQ文档助手开发教程](../docs-site/plugins/qqbot-napcat-tutorial.md)；[源码与安装](../integrations/napcat-campusos/README.md)；[开发计划](项目计划书v1/项目计划v1.2/03-QQ文档助手插件开发计划.md)；公开文档导航，独立于CampusOS v5安装合同 |
| Docker数据库更新与恢复 | [当前数据库管理](help/系统设计相关/数据库管理指南.md)、[本机更新记录](进度/v1.2-dev/v1.2.174-dev.md)；旧库与备份保留，当前35条迁移及默认数据独立验收 |
| 独立管理员首次初始化 | [Windows PowerShell与Linux交互命令](help/系统设计相关/v1.2独立管理员初始化与账号隔离.md)；按实际API配置选择凭证，Git不会同步机器配置或账号 |
| 个人模型配置规划 | [个人模型连接与知识问答接入计划](项目计划书v1/项目计划v1.2/04-个人模型连接与知识问答接入计划.md)；API Key/地址及协议适配待实现，Coding Plan按供应商用途核验 |
| Skill 使用和维护 | [仓库 Skills 索引](../skills/guides/README.md) |
| 版本历史和未来路线 | [历史阶段导读](help/计划书总结/README.md)、[当前计划治理](计划书总结/README.md) |
| 判断文档是否仍有效 | [Help 生命周期](help/README.md)、[文档审计与整理](help/文档审计与整理说明.md) |
| v1.1 历史审查 | [代码审查与项目回顾](项目计划书v1/项目计划v1.1/01-v1.1代码完成情况审查与项目回顾.md)、[最新开发进度](进度/v1.1-dev/) |
| v1.1 插件重构历史 | [自包含与动态加载重构计划](项目计划书v1/项目计划v1.1/02-v1.1插件自包含与动态加载重构计划书.md)、[用户配置与授权设计](help/系统设计相关/插件自包含动态加载与用户配置设计.md)；历史范围移交以当前v1.2主计划为准 |
| v1.2 当前开发与验收 | [主计划](项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md)、[开发进度](进度/v1.2-dev/)、[当前公开说明](../docs-site/project/current-roadmap.md)；各阶段实施、自动化与目标环境以原始证据为准 |

## 3. 文档类型和职责

| 路径 | 作用 | 是否作为当前操作入口 |
| --- | --- | --- |
| `help/` | 开发、部署、权限、插件、风格和运维说明 | 需要先看生命周期索引 |
| `api/` | [HTTP、Host API、权限和机器可读合同](api/README.md) | 当前合同优先于手写示例 |
| `architecture/` | [模块边界、数据所有权和安全设计](architecture/README.md) | 当前概览有效；带旧版本号的是决策历史 |
| `../skills/sources/`、`../skills/guides/` | 项目 Skills 的规范源文件与调用维护说明 | 当前；`.agents/skills/` 提供仓库发现入口 |
| [项目计划书v0/](项目计划书v0/README.md) | v0.1-v0.14 原始计划、回顾、输入材料和迁移说明 | 已封版，不是当前待办 |
| [项目计划书v1/](项目计划书v1/README.md) | v1.0–v1.2计划、架构决策与验收清单 | v1.2为当前实施权威；v1.0/v1.1保留历史范围与移交 |
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

当前数据库为冻结基线与前向迁移 `000001`–`000035`，当前投影为132表（130业务/系统受管表与2执行器表）、143外键；下一项结构变更从000036追加。旧
`000001-000049` 历史测试链不支持据编号直接原地升级。静态旧表清单不能替代 Admin `/architecture`、`migrations/`、
`make database-check` 和架构同步检查器；完整重构决策见
[v1.0 数据库全面重构方案](项目计划书v1/项目计划v1.0/01-v1.0数据库全面重构方案.md)，索引与约束治理见
[数据库迁移与 Schema 冗余治理](help/系统设计相关/数据库迁移与Schema冗余治理.md)。

## 5. 当前架构和产品边界

- CampusOS 是模块化单体，不是单模块，也没有拆成大量微服务。
- Core Module、Built-in Feature、External Plugin、Resource Package 使用不同生命周期和数据目录。
- `plugins/<key>/` 是 v5 插件源码，签名包显式导入 `PLUGIN_DATA_DIR/v5`；配置由宿主 PostgreSQL 按版本和 owner 管理，旧 `.installed` / `data/plugins` / `data/plugin_data` 不能作为当前发现入口；
  `data/resources` 保存无 Runtime 的资源包。
- 管理员与普通用户采用独立账号、凭据、会话、角色与Token域；同一PostgreSQL实例不等于共享登录凭据。初始化与部署MFA说明见[管理员入门](help/系统设计相关/v1.2独立管理员初始化与账号隔离.md)。
- 受限公开知识MCP、插件有限HTTP/gRPC管道及内置AI/Knowledge已有阶段证据；完整Agent、远程公共市场自动信任、通用RPC或任意插件执行不据此成立。
- Windows必需路径是Docker Desktop Linux容器；本机自动化和主机侧LAN已留证，第二台设备登录/PDF与当前Linux Final联合验收仍待。原生Windows Go宿主不作为本版必需目标。

## 6. 版本与计划

当前实施权威为v1.2主计划与最新进度。v0.1-v0.14、v1.0及早期v1.1记录保留追溯；不能将历史v4自动发现或旧用户配置目录当作当前操作流程。v1.2各阶段已有Linux证据，但最新增量、当前Final联合验收和远端LAN反馈仍待，不能宣称正式发布。

版本命名规则：面向开发者和使用者的项目计划、进度标题、Help/API/架构文档路径及 UI 展示统一使用
对应的产品版本（例如`v0.14`、`v1.2`）。迁移文件名、测试/Make 命令、基线 schema、数据库夹具、API/Host API/Manifest/
CampusDoc 协议版本等技术标识保持其既有写法，例如 `/api/v1`。数据库重构后的 migration 技术编号重新从
`000001_v1_1_schema_baseline` 开始；这不改变产品版本命名。

- [v1.0 插件生态与三层授权体系正式项目计划](项目计划书v1/项目计划v1.0/00-v1.0版本计划书.md)；P0/P1 仓库实现已完成，发布证据待收集
- [v1.0 数据库全面重构方案](项目计划书v1/项目计划v1.0/01-v1.0数据库全面重构方案.md)
- [v1.0 计划逐条审查与项目回顾](项目计划书v1/项目计划v1.0/03-v1.0计划逐条审查与项目回顾.md)
- [v1.0 插件三层授权与运行安全说明](help/系统设计相关/v1.0插件三层授权与运行安全说明.md)
- [当前数据库管理与迁移](help/系统设计相关/数据库管理指南.md)
- [v1.1 插件自包含与动态加载重构计划](项目计划书v1/项目计划v1.1/02-v1.1插件自包含与动态加载重构计划书.md)；历史范围和移交记录，当前以v1.2主计划为准
- [v1.2 版本优化计划书](项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md)；当前 v1.2 按依赖分阶段实施，未验收项保持未完成
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
