# CampusOS 项目上手 Skill 使用说明

> 日期：2026-07-07
> Skill 名称：`campusos-project-onboarding`
> 仓库内位置：`/home/jack/bbs/bbs01/CampusOS/skills/campusos-project-onboarding`
> Codex 用户级位置：`/home/jack/.codex/skills/campusos-project-onboarding`
> 用途：帮助 agent 快速了解 CampusOS 当前项目进度、代码结构、验证命令和开发边界

## 1. 用途

`campusos-project-onboarding` 用于在 agent 开始处理 CampusOS 任务前快速建立项目上下文。

它解决的问题是：后续会话或新 agent 可能不知道当前项目已经完成到哪里、哪些能力是真实落地、哪些只是计划或暂缓项。使用该 Skill 后，agent 会先读取项目进度快照，再根据任务读取相关代码和文档，避免从零开始翻完整个 `docs/`。

适用场景：

| 场景 | 说明 |
| --- | --- |
| 新 agent 接手项目 | 快速了解项目当前阶段和关键模块 |
| 上下文压缩后继续开发 | 用快照恢复项目背景 |
| 开始新版本阶段任务 | 先确认 v0.5-dev 完成状态和后续边界 |
| 做代码审查或缺陷修复 | 先明确相关目录、验证命令和风险点 |
| 更新 README 或计划文档 | 先确认当前真实状态，避免文档夸大 |

## 2. 推荐触发方式

中文触发：

```text
使用 campusos-project-onboarding 快速了解 CampusOS 当前项目进度后，再继续处理任务。
```

英文显式触发：

```text
Use $campusos-project-onboarding to understand the current CampusOS project status before working.
```

也可以在具体任务前加上：

```text
先使用 campusos-project-onboarding 读取项目当前状态，然后修复 xxx 问题。
```

## 3. Skill 结构

```text
skills/campusos-project-onboarding/
├── SKILL.md
├── agents/
│   └── openai.yaml
├── references/
│   └── current-project-status.md
└── scripts/
    └── context_snapshot.sh
```

| 文件 | 作用 |
| --- | --- |
| `SKILL.md` | 定义触发条件和 agent 上手流程 |
| `references/current-project-status.md` | 保存当前项目进度快照 |
| `scripts/context_snapshot.sh` | 只读输出最新仓库状态、README 摘要、迁移、文档和目录 |
| `agents/openai.yaml` | Skill UI 元数据 |

## 4. 当前快照内容

`references/current-project-status.md` 记录了以下信息：

| 内容 | 说明 |
| --- | --- |
| 当前基线 | `v0.5-dev` 已完成到 `v0.5.7-dev` |
| 已实现模块 | 用户前台、管理后台、后端 API、插件、个人主页、Webhook、MCP-like、Message、Metrics 等 |
| 版本回顾 | v0.1 到 v0.5-dev 的核心完成内容 |
| 数据库迁移 | `000001` 到 `000011` 的用途 |
| 关键目录 | `internal/`、`web/`、`admin/`、`sdk/`、`examples/` 等 |
| 运行命令 | `make dev-all`、`make migrate-up`、`go test`、前端构建命令等 |
| 服务和账号 | 本地端口、默认管理员、PostgreSQL、pgAdmin 账号 |
| 文档地图 | README、v5 计划、v0.5 进度、help 文档入口 |
| 开发边界 | 标准 MCP、真实 IM、插件市场、AI 审核、原生 Windows 等暂未完成项 |

## 5. 快照脚本

脚本位置：

```bash
skills/campusos-project-onboarding/scripts/context_snapshot.sh
```

运行：

```bash
./skills/campusos-project-onboarding/scripts/context_snapshot.sh
```

脚本只读取项目状态，不修改文件。输出内容包括：

| 输出项 | 说明 |
| --- | --- |
| Repository | 根目录、当前分支、HEAD、`git status -sb` |
| README 摘要 | 当前 README 开头状态说明 |
| v0.5 进度文档 | `docs/进度/v0.5-dev/` 中的进度文件 |
| Migrations | 当前 up migration 文件 |
| Help docs | `docs/help/` 下的帮助文档 |
| Project skills | 仓库内 Skill 列表 |
| Make targets | 当前 Makefile 命令入口 |
| Key implementation directories | 关键实现目录 |

## 6. 与其它 Skill 的关系

| Skill | 作用 | 与本 Skill 的关系 |
| --- | --- | --- |
| `campusos-project-onboarding` | 快速理解项目当前状态 | 建议作为接手项目的第一步 |
| `campusos-dev-workflow` | 继续完成版本阶段开发任务 | 上手后用于执行具体开发任务 |
| `campusos-dev-nocommit` | 完成任务但不提交 | 适合用户要求不 commit 的开发流程 |
| `campusos-readme-update` | 同步 README | 上手确认状态后再更新 README |

建议顺序：

```text
campusos-project-onboarding
        ↓
根据任务选择 campusos-dev-workflow / campusos-dev-nocommit / campusos-readme-update
```

## 7. 同步到 Codex 用户级目录

仓库内副本用于版本管理。为了让新会话能自动发现该 Skill，同步到用户级目录：

```bash
mkdir -p /home/jack/.codex/skills
rsync -a skills/campusos-project-onboarding/ /home/jack/.codex/skills/campusos-project-onboarding/
```

同步后可用：

```text
Use $campusos-project-onboarding to understand the current CampusOS project status before working.
```

## 8. 维护规则

当以下内容变化时，应同步更新 `references/current-project-status.md`：

| 变化 | 需要更新 |
| --- | --- |
| 新版本阶段开始或完成 | 当前基线、版本回顾、后续边界 |
| 新增 migration | 迁移清单 |
| 新增核心模块 | 已实现模块和关键目录 |
| 运行方式变化 | 运行命令、服务端口、默认账号 |
| 新增 help 文档 | 文档地图 |
| 暂缓项变为已完成 | 开发边界说明 |

更新后建议执行：

```bash
python3 /home/jack/.codex/skills/.system/skill-creator/scripts/quick_validate.py skills/campusos-project-onboarding
bash -n skills/campusos-project-onboarding/scripts/context_snapshot.sh
./skills/campusos-project-onboarding/scripts/context_snapshot.sh
git diff --check -- skills/campusos-project-onboarding docs/help/skills相关/CampusOS-项目上手Skill使用说明.md
```

如同步了用户级副本，也建议验证：

```bash
python3 /home/jack/.codex/skills/.system/skill-creator/scripts/quick_validate.py /home/jack/.codex/skills/campusos-project-onboarding
```

## 9. 注意事项

| 项目 | 要求 |
| --- | --- |
| 快照准确性 | 快照用于快速上手，但具体开发前仍要读取当前代码和文档 |
| 工作区安全 | agent 必须先看 `git status --short`，不要覆盖无关用户改动 |
| 已完成/未完成边界 | 不要把标准 MCP、真实 IM 适配器、插件市场、AI 审核、原生 Windows 写成已完成 |
| 验证范围 | 根据实际改动选择 Go 测试、迁移、web/admin 构建或脚本语法检查 |
| 文档同步 | 项目状态、运行方式或目录结构变化后应更新该 Skill 快照 |
