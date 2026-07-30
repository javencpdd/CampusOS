# CampusOS 项目上手 Skill 使用说明

> 更新日期：2026-07-29
> Skill：`campusos-project-onboarding`
> 当前项目基线：`v0.13.0`

## 1. 什么时候使用

在新 Agent 接手项目、上下文压缩后继续开发、审查版本完成度或准备修改架构前，先使用该 Skill。它会先
建立当前版本、四类扩展、migration、文档权威性和验证命令的共同上下文，避免从旧 v0.5/v0.6 文档开始工作。

推荐提示：

```text
Use $campusos-project-onboarding to understand the current CampusOS project status before working.
```

或者：

```text
先使用 campusos-project-onboarding 核对当前 CampusOS 基线，再处理这个任务。
```

## 2. Skill 结构

```text
skills/campusos-project-onboarding/
├── SKILL.md
├── agents/openai.yaml
├── references/current-project-status.md
└── scripts/context_snapshot.sh
```

| 文件 | 作用 |
| --- | --- |
| `SKILL.md` | 定义上手步骤、必读文件和按影响选择的验证 |
| `current-project-status.md` | `v0.13.0` 的架构、能力、限制、目录和运行快照 |
| `context_snapshot.sh` | 只读输出工作树、计划权威入口、最新进度、migration、Help/Skill 和关键目录 |
| `openai.yaml` | Skill 的界面元数据 |

## 3. 当前快照覆盖什么

- 模块化单体和 Core/Built-in Feature/External Plugin/Resource Package 四分类。
- Identity、Community、Reliability、Appearance、插件平台和 Docker 交付的当前边界。
- migration `000001-000041` 和关键代码/数据目录。
- 原生开发、Docker 开发、总编排和分组件启动方式。
- 当前仍未实现的标准 MCP、标准 protobuf gRPC、远程市场、真实第三方 Adapter 和高可用。
- Help 文档生命周期、v1-v13 计划总结和当前候选路线。

快照不是代码事实的替代品。具体任务仍要读取所属模块、Port、migration、路由和测试。

## 4. 运行动态快照

```bash
./skills/campusos-project-onboarding/scripts/context_snapshot.sh
```

脚本不会修改文件。它不再硬编码只列 v0.5/v0.6，而是动态显示最新 30 份进度记录，并从
`docs/计划书总结/README.md` 输出当前计划权威关系。

## 5. 推荐工作顺序

```text
campusos-project-onboarding
  -> 确认 Core / Feature / Plugin / Resource 归属
  -> 读取所属代码、migration、前端和当前文档
  -> 使用 campusos-dev-nocommit 或 campusos-dev-workflow
  -> 按影响运行测试、构建、架构和发布门禁
  -> 更新唯一权威文档和进度证据
```

开始修改前必须运行：

```bash
git status --short
```

不要覆盖其他开发者的改动，也不要把计划中的能力描述成已实现。

## 6. 维护与验证

以下变化需要同步该 Skill：

| 变化 | 更新位置 |
| --- | --- |
| 发布版本或版本路线变化 | `current-project-status.md`、计划总结链接 |
| 新增 Core/Feature 或目录迁移 | 四分类和关键目录 |
| 新增 migration | migration 基线 |
| Docker/端口/启动方式变化 | Run Modes |
| 完成原受控限制 | Important Boundaries |
| 文档入口变化 | Documentation Authority |

验证：

```bash
bash -n skills/campusos-project-onboarding/scripts/context_snapshot.sh
./skills/campusos-project-onboarding/scripts/context_snapshot.sh
python3 /home/jack/.codex/skills/.system/skill-creator/scripts/quick_validate.py \
  skills/campusos-project-onboarding
git diff --check
```

仓库副本是版本化事实源。同步到用户级 Codex Skill 目录属于本机安装操作，应在确认当前仓库版本后显式执行，
不要让自动脚本覆盖用户的其他 Skill。
