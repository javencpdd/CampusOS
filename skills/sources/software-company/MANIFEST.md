# software-company 专家包导出清单（MANIFEST）

> 导入快照说明（2026-09-25）：下文文件数、字节数与哈希只描述适配前的 WorkBuddy 导出包，不是当前目录的完整清单。CampusOS 新增的规范入口为 [SKILL.md](SKILL.md)，使用方法见 [软件团队协作 Skill 使用说明](../../guides/软件团队协作Skill使用说明.md)。原始角色及元信息保留用于溯源。
>
> WorkBuddy「软件开发团队（software-company）」专家包完整导出。
> 本包为纯 Markdown 配置型的多智能体（Agent Team）专家，零运行时依赖。

## 一、导出信息

| 项目 | 值 |
|------|-----|
| 专家包名称 | `software-company` |
| 版本 | `1.1.0` |
| 专家类型 | `team`（多智能体团队型） |
| 主入口 Agent | `software-team-lead` |
| 来源路径 | `C:/Users/19046/.workbuddy/plugins/marketplaces/experts/plugins/software-company` |
| 来源下载时间 | `2026-09-24T16:20:04.242Z` |
| 导出时间 | `2026-09-25` |
| 原始文件总数 | 7 个 |
| 含文档后总文件数 | 9 个（7 原始 + MANIFEST.md + MIGRATION.md） |
| 原始文件总字节 | 35127 B |
| 依赖 Skill | 无（包内不存在 `skills/` 目录） |
| 外部资源引用 | 无（5 份 Markdown 内无任何包外路径引用） |

## 二、角色配置清单

| # | 文件（包内相对路径） | 角色 | 角色名 | 职业 | 字节 | SHA256（前10位） |
|---|---------------------|------|--------|------|------|-----------------|
| 1 | `agents/software-team-lead.md` | 主理人 / Team Lead | 齐活林（Qi） | 交付总监（Delivery Director） | 11486 | `92364f5a97` |
| 2 | `agents/software-product-manager.md` | 产品经理 / Product Manager | 许清楚（Xu） | 产品经理（Product Manager） | 3897 | `c4fbb12e81` |
| 3 | `agents/software-architect.md` | 架构师 / Architect | 高见远（Gao） | 架构师（Software Architect） | 5460 | `2477277c2c` |
| 4 | `agents/software-engineer.md` | 工程师 / Engineer | 寇豆码（Kou） | 工程师（Software Engineer） | 7286 | `5da233c880` |
| 5 | `agents/software-qa-engineer.md` | QA 工程师 / QA Engineer | 严过关（Yan） | QA 工程师（QA Engineer） | 5312 | `a7d702ac28` |

### 角色职责说明

- **齐活林（Qi）**（主理人 / Team Lead，Agent ID：`software-team-lead`）：SOP 编排：判断工作流类型（快速模式 / BugFix / 标准 SOP / 部分工作流）、创建团队、调度成员、消息中转、成果汇总
- **许清楚（Xu）**（产品经理 / Product Manager，Agent ID：`software-product-manager`）：创建 PRD（简单 PRD 默认、完整 PRD 含竞品分析）、市场 / 竞品调研
- **高见远（Gao）**（架构师 / Architect，Agent ID：`software-architect`）：系统架构设计 + 任务分解（框架选型、文件列表、类图、时序图、依赖包、任务列表）
- **寇豆码（Kou）**（工程师 / Engineer，Agent ID：`software-engineer`）：批量编写代码、执行全局一致性审查（IS_PASS: YES / NO），最多 2 轮修正
- **严过关（Yan）**（QA 工程师 / QA Engineer，Agent ID：`software-qa-engineer`）：编写并运行测试、智能路由判定（源码 Bug 回工程师 / 测试 Bug 自修 / 通过报告），最多 2 轮测试

## 三、元信息文件说明

### 1. `.codebuddy-plugin/plugin.json`（1662 B）

WorkBuddy 专有插件元信息文件，核心字段如下：

| 字段 | 值 | 说明 |
|------|-----|------|
| `name` | `software-company` | 专家包标识 |
| `version` | `1.1.0` | 版本号 |
| `description` | Software Development Team - Optimized multi-agent SOP workflow... | 简介 |
| `expertType` | `team` | 团队型专家（WorkBuddy 专有字段） |
| `agentName` | `software-team-lead` | 默认主入口 Agent |
| `teamInfo.leadAgent` | `software-team-lead` | 团队主理人 |
| `teamInfo.memberAgents` | `software-product-manager` / `software-architect` / `software-engineer` / `software-qa-engineer` | 4 名成员 Agent |

**team 结构（leadAgent + 4 members）：**

```json
"teamInfo": {
  "leadAgent": "software-team-lead",
  "memberAgents": [
    "software-product-manager",
    "software-architect",
    "software-engineer",
    "software-qa-engineer"
  ]
}
```

`members` 数组共 5 条，每条含 `id` / `name`(en+zh) / `profession`(en+zh) / `avatar` / `role`，
与上表角色清单一一对应，其中 `role` 取值为 `lead`（主理人）或 `member`（成员）。

### 2. `.downloaded_at`（24 B）

市场包下载时间戳，内容为 `2026-09-24T16:20:04.242Z`。仅作溯源用，功能上可被忽略；
迁移到其他平台时可删除，不影响任何 Agent 定义。

## 四、完整性校验结论

| 校验项 | 结论 | 说明 |
|--------|------|------|
| 文件数量完整性 | 通过 | 源目录 7 个文件全部复制，无遗漏、无多余 |
| 元信息完整性 | 通过 | `.codebuddy-plugin/plugin.json` 与 `.downloaded_at` 均已包含 |
| 内容一致性 | 通过 | 逐字节复制（`copy2`），字节数与源文件完全一致 |
| Skills 依赖 | 无 | 包内不存在 `skills/` 目录；5 份 Markdown 全文检索 `skill` / `Skill` 关键字结果为 0 |
| 包外引用 | 无 | 5 份 Markdown 中不存在 `../`、`references/`、`scripts/`、`assets/` 等相对路径引用；主理人 md 成员表中出现的 md 文件名仅为文内示意，不构成文件依赖 |
| 运行时依赖 | 无 | 纯 Markdown + JSON 文本，无需任何第三方库、API Key 或网络访问 |
| **头像资源** | **缺失（已知缺口）** | 见下方专项说明 |

### 缺失项专项说明：`avatars/` 目录不存在

`plugin.json` 的 `members[].avatar` 共声明了 5 个头像路径：

| 成员 ID | 声明的头像路径 | 是否存在 |
|---------|----------------|----------|
| `software-team-lead` | `avatars/software-team-lead.png` | 否 |
| `software-product-manager` | `avatars/software-product-manager.png` | 否 |
| `software-architect` | `avatars/software-architect.png` | 否 |
| `software-engineer` | `avatars/software-engineer.png` | 否 |
| `software-qa-engineer` | `avatars/software-qa-engineer.png` | 否 |

经核实，源目录 `.../plugins/software-company` 下**不存在 `avatars/` 子目录**，
5 个 `.png` 头像文件在源侧即已缺失，本导出包未做伪造或占位填充，如实保留该缺口。

**影响范围**：头像仅用于 WorkBuddy 界面的角色图标展示，
**不影响任何 Agent 的行为定义与工作流执行**；迁移到其他不支持头像的平台时该字段可直接丢弃。
如需还原，处理方式见 `MIGRATION.md` 中「需人工调整的字段」一节。

## 五、完整文件树

包根目录：`software-company/`

```
software-company/
├── .codebuddy-plugin/
│   └── plugin.json                          1662 B
├── .downloaded_at                             24 B
├── agents/
│   ├── software-team-lead.md               11486 B
│   ├── software-product-manager.md          3897 B
│   ├── software-architect.md                5460 B
│   ├── software-engineer.md                 7286 B
│   └── software-qa-engineer.md              5312 B
├── MANIFEST.md                          （本文件，导出后生成）
└── MIGRATION.md                         （迁移指南，导出后生成）
```

合计：9 个文件（其中原始文件 7 个，共 35127 B）。

## 六、校验方式

如需复核任一文件的完整性，可在包根目录执行：

```bash
# Windows PowerShell
Get-FileHash -Algorithm SHA256 .\agents\software-team-lead.md | ForEach-Object { $_.Hash.Substring(0,10) }

# Linux / macOS
sha256sum agents/software-team-lead.md | cut -c1-10
```

---

本清单由导出脚本自动生成，导出日期 2026-09-25。
