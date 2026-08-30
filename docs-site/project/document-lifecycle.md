# 文档状态与历史替代

> 当前基线：`v0.13.0`  
> 最近审查：2026-08-30

CampusOS 保留历史计划、事故复盘和旧版教程，便于追溯决策与迁移旧实例。但“文件仍存在”不表示它仍是
当前操作标准。本页说明如何判断文档是否有效，以及旧文档应由什么入口替代。

## 判断当前事实

当前事实按以下顺序判断：

```text
可执行代码与 migration
  > 生成的 API、路由、数据库和资源合同
  > 最新进度验收证据
  > 当前架构与帮助文档
  > 已封版计划和历史设计稿
```

| 状态 | 含义 | 使用方式 |
| --- | --- | --- |
| 当前指南 | 已按当前代码和门禁核验 | 可以直接用于开发、部署或运维 |
| 专项参考 | 只覆盖一个兼容场景或故障 | 先读当前指南，再按需查阅 |
| 历史快照 | 记录旧版本的真实状态 | 只用于旧实例迁移和问题追溯 |
| 已被替代 | 新文档已完整接管其用途 | 不再作为新开发的执行依据 |
| 规划参考 | 描述候选能力 | 不等于已经实现或正式承诺 |

## 已失去当前操作作用的 Help 文档

这些文件保留在仓库 `docs/help/` 中，不建议删除，因为历史进度、Issue 或排障记录仍可能引用它们。

| 历史文件 | 不再适用的原因 | 当前入口 |
| --- | --- | --- |
| `v0.5回归Smoke测试说明.md` | 只覆盖 v0.5 Smoke | [构建与发布](/deployment/release) |
| `v0.5集成中心与低风险集成指南.md` | 是 v0.5 边界快照 | [集成中心与能力边界](/operations/integrations) |
| `数据库v0.6体检与模型决策.md` | 只记录 migration `000016` 时点 | [系统架构](/guide/architecture) 和 Admin 只读架构 |
| `数据库管理指南.md` 的静态表清单 | 表数量只更新到早期 migration | 当前 `migrations/`、Admin 只读架构和数据库检查器 |
| `RBAC权限与版主管理说明.md` | v10 前后的权限快照 | [权限配置入门](/guide/permission-configuration) |
| `v10权限管理设计与使用入门.md` | 已被 v11 权限、作用域和可靠审计模型扩展 | [权限配置入门](/guide/permission-configuration) |
| `插件开发与工具链v0.6.md` | 只覆盖 Manifest v1 和旧工具链 | [插件体系](/plugins/overview)、[课表插件教程](/plugins/schedule-plugin-tutorial) |
| `插件包治理与回滚说明.md` | 早期导入流程 | [打包、导入与更新](/plugins/package-import) |
| `个人主页风格包说明.md` | 旧 Personal Space 风格合同 | [风格包与沙箱 SDK](/plugins/style-packs) |
| `PostgreSQL双实例数据不一致说明.md` | 单次本地事故复盘 | [Docker 部署与迁移](/deployment/docker) |

历史文档不是“没有保留价值”，而是已经失去当前操作手册的作用。仓库中的
`docs/help/README.md` 保存完整清单、状态横幅和替代链接。

以下完全重复且没有历史进度引用的短文已经合并删除：

- `关于env文件配置.md`，内容进入 [配置与端口](/deployment/configuration)。
- `一键启动前后端脚本说明.md`，内容进入 [开发环境](/deployment/development)。
- 旧 GitHub Actions 与 PR 自测两份说明，内容进入
  [贡献、Pull Request 与 CI/CD](/contributing/workflow)。

仓库的完整逐文件审计位于 `docs/help/文档审计与整理说明.md`。

## 已失去当前执行作用的计划

- v1-v3 计划只作早期蓝图和实现快照，不再指导当前目录、端口、数据库或插件开发。
- v4 原计划没有全量闭合，应以 v4 实现状态总结为历史结论。
- v5 第一版已被第二版替代；v6 早期计划已被第三版替代。
- v7-v13 正式计划均已封版。它们仍是架构和验收历史，但不再接收新任务。
- 名称含“计划思路”“提示词”的文件是原始输入，不是正式范围或完成证明。

当前版本结论见 [v1-v14 版本演进](/project/version-evolution)，后续候选见
[当前规划与后续路线](/project/current-roadmap)。

v14-dev 的 P0/P1 已有实际代码、migration 与进度证据；它既不是纯“规划参考”，也尚未成为发布版本。当前应以
仓库 `docs/进度/v0.14-dev/v0.14.8-dev.md` 判断当前实现与修复事实（P0/P1 代码证据见 `v0.14.5-dev.md`），以仓库
`docs/项目计划v14/00-v14版本计划书.md` 判断未通过的 Final 门禁。

仓库贡献者还可以从 `docs/help/计划书总结/README.md` 进入逐版计划有效性和未来规划导读；原始计划与
验收证据继续保存在 `docs/项目计划v*/` 和 `docs/进度/`。

## 开发者上手文档覆盖

当前已经补齐新贡献者完成一次开发闭环所需的主要入口：

| 任务 | 当前指南 |
| --- | --- |
| 认识项目并选择运行方式 | [完整入门路径](/guide/getting-started) |
| 按阶段完成第一次贡献 | [开发者学习路线](/guide/developer-learning-path) |
| 本机开发与统一验证 | [开发环境](/deployment/development) |
| Windows/Linux 使用 Docker 开发 | [Docker 跨平台开发](/deployment/docker-development) |
| 判断 Module、Plugin 和 Resource 的归属 | [模块与插件边界](/guide/module-plugin-resource-boundaries) |
| 理解和配置权限 | [权限配置入门](/guide/permission-configuration) |
| 编写、导入和测试外部插件 | [课表插件完整教程](/plugins/schedule-plugin-tutorial) |
| 调用 API 和处理错误 | [接口约定](/api/overview) |
| 发布、备份和恢复 | [构建与发布](/deployment/release)、[备份与恢复](/operations/recovery) |
| 提交 PR 并理解 CI | [贡献、Pull Request 与 CI/CD](/contributing/workflow) |

因此当前没有阻断一般开发者上手的 P0 文档缺口。以下内容需要在对应能力和真实证据形成后补充，不能提前
编写成已经可用的教程：

- 按发行版本切换的公开文档和 release notes。
- 真实 Windows 与 Linux `arm64` 发行认证报告。
- 生产反向代理/TLS、镜像签名、SBOM 和高可用参考部署。
- 标准 MCP、真实 protobuf gRPC、远程插件市场和隔离 Agent Runner 教程。

## 维护规则

1. 一个主题只保留一个权威正文，README、Help 和 docs-site 只互相提供入口。
2. 旧文档不静默删除；先标记状态、提供替代入口，再评估归档。
3. API、migration、权限、数据目录或 Manifest 变化时同步更新生成合同和文档。
4. 文档变更至少执行链接检查、README 检查、docs-site 构建和差异检查。
5. 没有代码与测试证据的规划内容必须明确标注“未实现”或“候选”。
