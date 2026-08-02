# CampusOS 文档门户

> 当前发布基线：`v0.13.0`
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
| 后端模块或数据所有权 | [当前架构](architecture/当前架构概览.md)、[可靠命令与数据所有权](architecture/v11可靠命令事件与数据所有权.md) |
| HTTP API | [API 与机器合同](api/README.md)、[接口约定](../docs-site/api/overview.md) |
| Web/Admin 前端 | [官方完整入门](../docs-site/guide/getting-started.md)、[权限配置](../docs-site/guide/permission-configuration.md) |
| External Plugin | [插件体系](../docs-site/plugins/overview.md)、[课表插件教程](../docs-site/plugins/schedule-plugin-tutorial.md) |
| Resource Package 或风格包 | [风格包与沙箱 SDK](../docs-site/plugins/style-packs.md)、[双端交付标准](help/系统设计相关/v13风格包双端交付标准.md) |
| 权限、版主和审计 | [权限配置入门](../docs-site/guide/permission-configuration.md)、[权限与可靠审计](help/系统设计相关/v11权限管理与可靠审计设计入门.md) |
| 可靠任务与 Webhook | [可靠任务运维](../docs-site/operations/reliable-tasks.md)、[故障恢复](help/系统设计相关/v13可靠任务指标告警与故障恢复Runbook.md) |
| Skill 使用和维护 | [Skills 索引](skills/README.md) |
| 版本历史和未来路线 | [Help 版计划总结](help/计划书总结/README.md)、[详细计划治理](计划书总结/README.md) |
| 判断文档是否仍有效 | [Help 生命周期](help/README.md)、[文档审计与整理](help/文档审计与整理说明.md) |

## 3. 文档类型和职责

| 路径 | 作用 | 是否作为当前操作入口 |
| --- | --- | --- |
| `help/` | 开发、部署、权限、插件、风格和运维说明 | 需要先看生命周期索引 |
| `api/` | [HTTP、Host API、权限和机器可读合同](api/README.md) | 当前合同优先于手写示例 |
| `architecture/` | [模块边界、数据所有权和安全设计](architecture/README.md) | 当前概览有效；带旧版本号的是决策历史 |
| `skills/` | 项目 Skills 的调用和维护 | 当前 |
| `项目计划v1/` 至 `项目计划v13/` | 原始计划、回顾和输入材料 | 已封版，不是当前待办 |
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

当前数据库有 42 个追加 migration。静态旧表清单不能替代 Admin `/architecture`、`migrations/`、
`make database-check` 和架构同步检查器；索引与约束冗余治理见
[数据库迁移与 Schema 冗余治理](help/系统设计相关/数据库迁移与Schema冗余治理.md)。

## 5. 当前架构和产品边界

- CampusOS 是模块化单体，不是单模块，也没有拆成大量微服务。
- Core Module、Built-in Feature、External Plugin、Resource Package 使用不同生命周期和数据目录。
- `data/plugins` 只保存 External Plugin 实现；`data/plugin_data` 保存其运行数据；
  `data/resources` 保存无 Runtime 的资源包。
- 管理平面使用独立管理员准入事实，但密码凭据仍由 Identity 的 `accounts` 安全拥有。
- 标准 MCP Server、真实 protobuf gRPC、Discord/OneBot 生产 Adapter、远程公共插件市场和完整 Agent
  产品尚未实现。
- Windows 支持路径是 Docker Desktop Linux Containers；当前真实发行证据仍以 Linux `amd64` 为主。

## 6. 版本与计划

v1-v13 都已退出待执行状态，v13 是当前发布基线但也已封版。下一版尚未正式立项：

- [v1-v13 迭代内容与有效性](help/计划书总结/01-v1-v13迭代内容与有效性.md)
- [当前项目状态与未来规划](help/计划书总结/02-当前项目状态与未来规划.md)
- [v13 最终专业审计](项目计划v13/02-v13最终专业审计与后续路线.md)
- [v0.13 进度证据](进度/v0.13-dev/)

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
