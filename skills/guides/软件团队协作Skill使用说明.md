# 软件团队协作（software-company）Skill 使用说明

> 更新时间：2026-09-25（Asia/Shanghai）
>
> 规范源：[`skills/sources/software-company/SKILL.md`](../sources/software-company/SKILL.md)  
> 仓库发现入口：[`.agents/skills/software-company/SKILL.md`](../../.agents/skills/software-company/SKILL.md)

## 作用

该 Skill 将导入的 WorkBuddy `software-company` 专家包适配为 CampusOS 的跨职责交付方法：从产品目标、架构边界、工程切片、测试证据到交付复盘。它不引入新的运行时插件，也不会自动创建五个代理。原包中的角色分工是参考素材，CampusOS 的代码、当前计划和权限约束才是实施依据。

适合跨模块新功能、插件/数据/权限重构、需要完整验收闭环的任务。简单 Bug 或局部代码修改用 `$senior-developer` 和相应专项 Skill 即可，不需要走全套产品到交付流程。

## 如何调用

从 CampusOS 仓库打开 Codex 后，可以这样提出任务：

```text
使用 $software-company 与 $campusos-dev-nocommit，针对插件安装流程梳理用户场景、架构边界、最小实现切片和验证证据。保持未提交，不修改范围外的数据。
```

只做设计时明确停止点：

```text
使用 $software-company 评审个人文档与图文附件的权限模型，只输出风险与改造方案，暂不修改代码。
```

用户明确要求多代理协作时，可按产品、架构、工程或测试拆分互不冲突的有限子任务；没有此要求时，单个代理按需采用这些视角即可。Skill 不要求调用 WorkBuddy 的 `TeamCreate`、`SendMessage` 或不存在的角色 Agent ID。

## 与项目 Skills 的配合

| 任务 | 配合的 Skill |
| --- | --- |
| 当前版本、计划与代码状态不清楚 | `$campusos-project-onboarding` |
| 普通开发及进度文档 | `$campusos-dev-nocommit`；明确要求本地提交才用 `$campusos-dev-workflow` |
| 单一纵向路径的小步实施 | `$senior-developer` |
| Web/Admin 界面回归 | `$campusos-webui-regression` |
| migration、持久化和平台表 | `$campusos-data-architecture-sync` |
| Docker 开发环境或运行验证 | `$campusos-docker-development` |

`software-company` 负责决定“哪些视角和交接点对本任务有用”；专项 Skill 负责各领域的实际约束。每阶段只记录足够下一阶段工作的事实，不自动生成 PRD、类图、竞品分析或交付报告。

## 本次适配与原包的区别

| 原始导出约定 | CampusOS 适配方式 |
| --- | --- |
| 每次必须 `TeamCreate`，成员只能通过指定工具中转 | 无明确多代理授权时由单代理顺序完成视角审查；不虚构团队产出。 |
| 默认 Vite + React + MUI + Python + SQLite | 先读现有 Go、Vue、PostgreSQL、Docker 和 `plugins/` 代码，不擅自换技术栈。 |
| 工程师尽量一次写完全部文件再总审 | 按可验证纵向切片实现并立即运行定向检查。 |
| QA 硬性两轮、所有源码文件逐一对应测试文件 | 以行为和风险确定测试范围，修复根因并重验，不用轮次上限代替验收。 |
| 固定输出 `docs/system_design.md` 和 Mermaid 文件 | 复用当前计划、架构、API、测试和进度文档；只有关系复杂或用户要求时才新增图。 |
| WorkBuddy 插件元信息与头像 | 仅作导入溯源，不构成 Codex Skill 或 CampusOS 运行时插件。 |

导入快照保留在 `agents/`、`.codebuddy-plugin/`、`MANIFEST.md` 与 `MIGRATION.md`。其中原有跨平台迁移指南不代表当前 Codex 或 CampusOS 的执行方式；请以根级 `SKILL.md` 和本指南为准。角色交接的简表在 [`references/campusos-roles.md`](../sources/software-company/references/campusos-roles.md)。

## Clone 后使用与维护

仓库自带 `.agents/skills/software-company/` 可移植发现桥接，无需复制到用户目录。已经打开的 Codex 会话如未看到新 Skill，重启一次以刷新列表。

修改规范源后，在仓库根目录运行：

```powershell
python skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --write
python skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --check
python <skill-creator目录>/scripts/quick_validate.py skills/sources/software-company
python scripts/check-doc-links.py
python scripts/check-line-endings.py --include-untracked
git diff --check
```

把 `<skill-creator目录>` 替换为当前环境中 `skill-creator` 的真实目录；不要直接编辑生成的桥接文件，也不要把原导入快照的哈希表误认为适配后目录的当前文件清单。
