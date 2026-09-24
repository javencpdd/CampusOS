# software-company 专家包迁移指南（MIGRATION）

> 历史导入资料（2026-09-25）：本文记录原 WorkBuddy 包的跨平台迁移设想，其他平台的具体行为与工具可用性未在当前环境逐项验证。原导出稿中“Codex CLI 只读取单一 AGENTS.md”、必须组建团队等说法已不适用于 CampusOS；当前项目请使用 [适配后 SKILL.md](SKILL.md) 与 [使用说明](../../guides/软件团队协作Skill使用说明.md)，不要复制角色提示词覆盖现有项目规则。
>
> 目标：把 WorkBuddy 的「软件开发团队（software-company）」专家包，
> 迁移到 Claude Code、Cursor、Codex CLI、OpenCode / Cline、Dify / Coze 等其他 Agent 平台。

迁移前请先阅读同目录的 `MANIFEST.md`，了解文件清单与已知缺失项（头像）。

## 一、包结构

```
software-company/
├── .codebuddy-plugin/
│   └── plugin.json          WorkBuddy 专有元信息（team 结构 / 成员声明 / 头像路径）
├── .downloaded_at           下载时间戳（可忽略，可删除）
├── agents/                  5 份角色定义 Markdown（本包的核心资产）
│   ├── software-team-lead.md          主理人 齐活林（Qi） · 交付总监
│   ├── software-product-manager.md    产品经理 许清楚（Xu）
│   ├── software-architect.md          架构师 高见远（Gao）
│   ├── software-engineer.md           工程师 寇豆码（Kou）
│   └── software-qa-engineer.md        QA 工程师 严过关（Yan）
├── MANIFEST.md
└── MIGRATION.md
```

**核心资产**：`agents/` 下 5 份 Markdown。每份文件以 YAML frontmatter 开头（`name` + `description`），其后即为该角色的完整 System Prompt，可直接迁移。

**非核心资产**：`.codebuddy-plugin/plugin.json` 是 WorkBuddy 专有格式，其他平台无法识别；`.downloaded_at` 仅作溯源。

## 二、各平台落地方式对照表

| 平台 | 落地位置 | 文件格式要求 | 适配难度 |
|------|----------|--------------|----------|
| **Claude Code** | 全局：`~/.claude/agents/`；项目级：`<repo>/.claude/agents/` | 每个 Agent 一个 `.md` 文件，保留 YAML frontmatter（`name` / `description` 会被自动解析） | 极低 |
| **Cursor** | 推荐：`<repo>/.cursor/agents/`（新版本）；兼容：`<repo>/.cursor/rules/*.mdc` | `.md` 或 `.mdc`，frontmatter 中的 `description` 用于触发条件描述 | 低 |
| **CampusOS 仓库内 Codex** | `skills/sources/software-company/SKILL.md` + `.agents/skills/software-company/SKILL.md` 桥接 | 仓库级 Skill 自动发现，按需读取角色交接参考 | 已适配 |
| **OpenCode / Cline** | ①系统提示词 / `CLAUDE.md`、`CLINE.md` 等规则文件；②自定义 `.agents/` 目录（部分版本支持） | Markdown 明文 | 中 |
| **Dify / Coze** | 平台内的「人设与回复逻辑」（Prompt 编排画布） | 无文件系统，需把 Markdown 文本粘贴进 Prompt 编排框 | 中高 |

### 1. Claude Code

```bash
mkdir -p ~/.claude/agents
cp software-company/agents/*.md ~/.claude/agents/
```

Claude Code 原生支持子智能体（Sub-Agent）与 `Task` 工具：把 5 份 md 放入 `agents/` 后，
各 Agent 的 ID 即 frontmatter 中的 `name`，可被主 Agent 直接按名称调用。
若只想对单个项目生效，复制到项目内 `.claude/agents/` 即可。

### 2. Cursor

```bash
mkdir -p .cursor/agents
cp software-company/agents/*.md .cursor/agents/
```

旧版 Cursor 不支持多 Agent 目录，可改为规则文件：

```bash
mkdir -p .cursor/rules
cp software-company/agents/software-team-lead.md .cursor/rules/team-lead.mdc
```

`.mdc` 需在 frontmatter 中补充 `description` 与可选的 `globs`（本包 md 已自带 `description`，可直接沿用）。

### 3. CampusOS 仓库内 Codex

直接使用仓库级 `$software-company`：规范源位于 `SKILL.md`，发现桥接由同步脚本维护在 `.agents/skills/software-company/`。无需拼接原始角色文本到 `AGENTS.md`，也无需安装到用户目录；使用细节见 [中文指南](../../guides/软件团队协作Skill使用说明.md)。

### 4. OpenCode / Cline

- **方式 A（推荐）**：放到 `.agents/` 目录（如 `mkdir -p .agents && cp agents/*.md .agents/`），若当前版本支持自定义 Agent，可直接生效；
- **方式 B（兜底）**：把主理人 md 的正文贴入全局系统提示词 / `CLAUDE.md` / `CLINE.md`，其余 4 份作为项目内的规则文件放置，由主理人在需要时读取。

### 5. Dify / Coze

这类平台没有本地文件系统，需在界面中手工录入：

- **主智能体**：新建 Bot / Agent，把 `software-team-lead.md` 的正文（去掉 frontmatter）整段贴入「人设与回复逻辑」；
- **子角色**：把其余 4 份 md 分别粘贴进各自的对话节点、分支节点或知识库，再由工作流编排按 SOP 顺序调用；
- **多智能体模式**（若平台支持）：为每个角色各建一个 Agent，并以 `software-team-lead` 作为 orchestrator。

## 三、团队型专家的特殊说明（重要）

本专家包**不是单个角色**，而是 **5 个角色协同**的团队型配置：

```
主理人 software-team-lead（齐活林）
  ├── software-product-manager（许清楚）产品经理
  ├── software-architect        （高见远）架构师
  ├── software-engineer         （寇豆码）工程师
  └── software-qa-engineer      （严过关）QA 工程师
```

### 3.1 平台不支持多智能体编排时

大多数目标平台并不具备 WorkBuddy 的「TeamCreate + spawn teammate」能力。此时建议降级为**单 Agent + 按需加载角色定义**：

1. **主理人 md 作为 System Prompt**：把 `software-team-lead.md` 的正文设为该 Agent 的人设；
2. **其余 4 份 md 作为可按需加载的角色定义**：
   - Claude Code / Cursor：放入 `agents/` 或规则目录，由主 Agent 通过 Task / Read 调用；
   - CampusOS Codex：调用 `$software-company` 并按需读取 `references/campusos-roles.md`，不复制原始角色提示词；
   - Dify / Coze：放进知识库或子节点，用工作流串接。
3. **保留 SOP 语义**：即使物理上是单 Agent，也要让主理人文本中的阶段推进、
质量关卡（`IS_PASS: YES / NO`）、反馈回路规则继续保持生效。

### 3.2 「调度成员」机制的等价替换（关键）

主理人 md 中的团队协作机制依赖平台的 subagent / 多 Agent 能力，具体包括两类操作：

| 主理人 md 中的机制 | WorkBuddy 中的实现 | 迁移时需替换成的等价机制 |
|--------------------|--------------------|--------------------------|
| 建立团队 | `TeamCreate`（须由主理人执行） | 平台的 Sub-Agent 会话 / 多 Agent 工作区创建；不支持时直接省略此步 |
| 调度成员 | Agent 工具，`name` 与 `subagent_type` 均传 Agent ID | CampusOS 中仅在用户明确要求多代理协作时使用当前环境提供的子代理能力；其他平台须核实自身工具后再决定。 |
| 消息中转 | 成员产出回传主理人后再转发 | 由编排层（orchestrator prompt 或工作流连线）承接 |

WorkBuddy 中对应的 **Agent ID** 如下，迁移时必须替换为新平台的等价调用方式：

| Agent ID | 角色 | 迁移后的处理 |
|----------|------|--------------|
| `software-architect` | 架构师 高见远 | 替换为新平台的 Agent / 工具名 |
| `software-engineer` | 工程师 寇豆码 | 替换为新平台的 Agent / 工具名 |
| `software-product-manager` | 产品经理 许清楚 | 替换为新平台的 Agent / 工具名 |
| `software-qa-engineer` | QA 工程师 严过关 | 替换为新平台的 Agent / 工具名 |

> 提示：`plugin.json` 中 `teamInfo.memberAgents` 列出的正是这 4 个 ID，
> 与 `agents/` 下 md 文件名（去掉 `.md`）完全一致，可作为各平台的 `name` / 文件名直接复用。

### 3.3 工作流路由可在任何平台上保留

主理人 md 中的四类工作流路由属于纯文本规则，与平台无关，建议原样保留：

| 工作流 | 触发条件 | 链路 |
|--------|----------|------|
| 快速模式 | 单页面应用、小游戏、工具脚本、≤ 10 个源文件 | 工程师 → QA |
| BugFix 快捷路径 | 用户报告明确 Bug，非新功能 | 工程师（定位+修复） → QA（回归） |
| 标准 SOP | 多模块 / 前后端 / > 10 个源文件 | 产品经理 → 架构师 → 工程师 → QA |
| 部分工作流 | 仅需 PRD / 架构评审 / 代码实现 / 仅测试 / 调研 | 按需调用单个成员 |

## 四、需人工调整的字段

| 位置 | 字段 | 问题 | 处理建议 |
|------|------|------|----------|
| `plugin.json` | `members[].avatar` = `avatars/*.png` | 5 个头像文件在源目录中不存在 | 见下方「头像缺失处理建议」 |
| `plugin.json` | `expertType` = `team` | WorkBuddy 专有字段，其他平台无对应概念 | 删除，或映射为目标平台的 multi-agent 配置项 |
| `plugin.json` | `teamInfo`（`leadAgent` / `memberAgents`） | WorkBuddy 专有结构 | 在新平台用各自的编排配置重建（如 Claude Code 的 Task 白名单、Dify 的工作流节点） |
| `plugin.json` | `agentName` = `software-team-lead` | 主入口指示 | 映射为目标平台的默认 / 主 Agent |
| `plugin.json` | `name` / `version` / `description` | 包级元信息 | 用于命名目录或 Agent 组，可保留作备注 |
| `agents/*.md` | YAML frontmatter `name` / `description` | 多数平台可直接使用 | Claude Code / Cursor 原生支持；Codex / Dify 粘贴正文时可删除 `---` 包裹块 |
| `.downloaded_at` | 时间戳 `2026-09-24T16:20:04.242Z` | 仅溯源用 | 可直接删除 |

### 头像缺失处理建议

现状：`plugin.json` 声明了 5 个头像路径，但源目录不含 `avatars/` 目录，文件确实缺失。

1. **WorkBuddy 内使用**：头像缺失只影响角色图标显示，功能不受影响。如在意，可自行放入任意 PNG（建议 1:1 头像图）到 `avatars/` 下，文件名保持
   `software-team-lead.png` / `software-product-manager.png` / `software-architect.png` /
   `software-engineer.png` / `software-qa-engineer.png`；
2. **迁移到不支持头像的平台**：直接删除 `plugin.json` 中的 `avatar` 字段，整待无需补全；
3. **不建议伪造二进制文件**：本包保持源现状，未生成占位图片，以免污染包的完整性。

## 五、外部依赖

**无。本包零外部依赖。**

| 依赖类别 | 情况 |
|----------|------|
| Skill / 插件 | 不存在 `skills/` 目录，5 份 md 中无 Skill 引用 |
| 参考文档 / scripts / assets | 无任何包外相对路径引用 |
| 运行时库 | 不需要（纯 Markdown + JSON 文本） |
| API Key / 网络访问 | 不需要（除目标平台自身的模型调用） |
| 二进制资源 | 仅需 `avatars/*.png`，且当前缺失（不影响运行） |

因此在任何目标平台上，只要能吃下 Markdown 文本即可完成迁移，无需安装任何额外组件。

## 六、推荐迁移步骤

1. 解压本包，阅读 `MANIFEST.md` 核对 9 个文件；
2. 选定目标平台，按第二节对照表把 `agents/*.md` 复制到对应位置；
3. 把主理人 md 设为主 Agent / System Prompt；
4. 把 4 个成员 md 注册为同等的 Agent 或子角色，按第三节替换「调度成员」机制；
5. 按需处理 `plugin.json` 专有字段（多数平台可直接忽略该文件）；
6. 用一个小型需求（如「写一个 Todo 应用」）走一遍快速模式，验证
   主理人 → 工程师 → QA 的链路是否通畅。

---

本指南由导出脚本自动生成，导出日期 2026-09-25。
