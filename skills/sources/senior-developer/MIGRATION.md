# 迁移指南 — senior-developer 专家包

本包为「吴八哥 · 高级开发工程师」专家包的完整导出，可在任意支持 Markdown 提示词 / Skill 目录约定的 Agent 软件中复用。

## 1. 包结构

```
senior-developer/
├── README.md                      # 专家简介（一句话）
├── agents/senior-developer.md     # 角色主配置（人格、工作流、输出规范、身份声明）
├── avatars/expert.png             # 头像
├── skills/
│   ├── fullstack-dev/             # 全栈架构与开发指南
│   │   ├── SKILL.md
│   │   └── references/            # 9 份：api-design / architecture-patterns / auth-flow /
│   │                              # db-schema / django-best-practices / environment-management /
│   │                              # release-checklist / technology-selection / testing-strategy
│   ├── frontend-dev/              # 前端 UI + 动效 + AI 媒体生成
│   │   ├── SKILL.md
│   │   ├── references/            # 11 份：asset-prompt-guide / design-rules / env-setup /
│   │   │                          # minimax-cli-reference / minimax-image-guide / minimax-music-guide /
│   │   │                          # minimax-tts-guide / minimax-video-guide / minimax-voice-catalog /
│   │   │                          # motion-recipes / troubleshooting
│   │   ├── scripts/               # minimax_{image,music,tts,video}.py
│   │   ├── templates/             # generator_template.js / viewer.html
│   │   └── canvas-fonts/          # 34 款字体 ttf + 对应 OFL 许可证
│   └── browser-use/               # 浏览器自动化
│       ├── SKILL.md
│       └── references/            # cdp-python.md / multi-session.md
├── MANIFEST.md                    # 文件清单与校验值
└── MIGRATION.md                   # 本文件
```

## 2. 各平台的落地方式

| 目标平台 | 角色配置放哪里 | Skill 放哪里 |
|---|---|---|
| Claude Code | `~/.claude/agents/senior-developer.md`（或项目 `.claude/agents/`） | `~/.claude/skills/<skill名>/SKILL.md` |
| Cursor | `.cursor/agents/` 或写入 `.cursor/rules/*.mdc` | `.cursor/skills/<skill名>/SKILL.md` |
| Codex CLI | 追加进 `AGENTS.md` | `~/.codex/skills/<skill名>/SKILL.md` |
| OpenCode / Cline 等 | 写入系统提示词或 `AGENTS.md` | 项目 `.skills/<skill名>/SKILL.md` |
| Dify / Coze 类平台 | 把 `agents/senior-developer.md` 全文贴入「人设与回复逻辑」 | 把需要的能力以知识库文档或工具描述方式挂载 |

通用兜底方案（任何平台都能用）：
1. 把 `agents/senior-developer.md` 全文作为 System Prompt；
2. 把 `skills/*/SKILL.md` 及 `references/*.md` 放入项目知识库或 `.md` 上下文目录，让 Agent 按需读取；
3. 在 System Prompt 末尾追加一段索引，说明三个 Skill 的触发条件与路径。

## 3. 需要安装端自行准备的依赖

| 依赖 | 用途 | 说明 |
|---|---|---|
| `MINIMAX_API_KEY` / `MINIMAX_API_BASE` | frontend-dev 的 AI 图片/视频/语音/音乐生成脚本 | 环境变量，非本包内容 |
| Python 3 + `requests` | 运行 `skills/frontend-dev/scripts/*.py` | 脚本为标准库 + requests |
| `browser-use` CLI | browser-use 技能 | SKILL.md 中标注安装命令：`curl -fsSL https://browser-use.com/cli/install.sh \| bash` |
| 字体文件 | Canvas 生成艺术 | 已在 `canvas-fonts/` 内，OFL 许可证须随附保留 |

## 4. 迁移后需人工调整的字段

- `SKILL.md` frontmatter 中的 `allowed-tools: Bash(browser-use:*)` 为 WorkBuddy 专有字段，其他平台可直接删除或替换为其工具白名单语法。
- `metadata.clawdbot`、`description_zh/description_en` 为可选扩展字段，不影响使用。
- 角色配置中「内置 Skill 使用场景」一节写死了三个 Skill 名，迁移时请同步为新平台的实际路径/调用方式。

## 5. 完整性自检

- 文档内相对引用全部命中，无失效链接；
- 无任何指向 `~/.workbuddy`、绝对路径或上级目录的硬编码引用，包可独立搬运；
- 唯一外部耦合为第 3 节列出的运行时依赖。
