# CampusOS README 更新 Skill 使用说明

> 更新时间：2026-08-03
> Skill：`campusos-readme-update`
> 实现目录：`skills/sources/campusos-readme-update/`

## 1. 作用

该 Skill 用于维护 CampusOS 根 `README.md` 和 `docs/README.md` 的职责边界：

- README 只保留项目定位、当前基线、最短启动、主要入口、简要目录和文档入口。
- 运行、账号、数据库、插件、Skill、API、架构、进度和计划细节进入对应 `docs/` 分类。
- 被拆出的内容必须先落到规范文档，再从文档门户可达，最后才能从 README 删除。
- 当前对外支持的开发/用户流程还需同步 `docs-site/`；详细事实仍以一个 Help/API/Architecture 文档为权威。
- 当前能力必须由代码、配置、migration 或进度记录证明，不能把规划写成已完成。

## 2. 推荐调用

```text
使用 $campusos-readme-update 审查并精简 CampusOS README，把次要内容迁入正确的 docs 分类，并检查文档入口。
```

也可以指定本次重点：

```text
使用 $campusos-readme-update，把 README 中的插件开发和 API 细节迁入 docs，并保留核心入口。
```

## 3. 内容路由

| 内容 | 目标目录 |
| --- | --- |
| 环境、启动、账号、数据库、排障和贡献 | `docs/help/` 下对应分类 |
| Skill 调用、维护和验证 | `skills/guides/` |
| HTTP/Host API、权限和错误契约 | `docs/api/` |
| 模块、数据、存储和安全边界 | `docs/architecture/` |
| 已完成任务和验证结果 | `docs/进度/<stage>/` |
| 后续范围、优先级和验收 | `docs/项目计划v*/` |

详细规则见 `skills/sources/campusos-readme-update/references/documentation-routing.md`。

## 4. Skill 结构

```text
skills/sources/campusos-readme-update/
├── SKILL.md
├── agents/openai.yaml
├── references/
│   ├── documentation-routing.md
│   └── readme-update-checklist.md
└── scripts/
    ├── audit_readme_structure.py
    └── check_readme_links.py
```

## 5. 检查命令

检查 README 大小、核心章节、docs 门户和分类入口：

```text
python skills/sources/campusos-readme-update/scripts/audit_readme_structure.py --root .
```

检查 README 和文档门户的本地 Markdown 链接：

```text
python skills/sources/campusos-readme-update/scripts/check_readme_links.py \
  --root . README.md docs/README.md
```

创建或迁移文档后，检查它是否能从 `docs/README.md` 访问：

```text
python skills/sources/campusos-readme-update/scripts/audit_readme_structure.py \
  --root . \
  --require-doc skills/guides/CampusOS-README更新Skill使用说明.md
```

旧命令仍然兼容：

```text
python skills/sources/campusos-readme-update/scripts/check_readme_links.py README.md
```

## 6. 检查结果解释

| 结果 | 含义 |
| --- | --- |
| `result: passed` | README 核心结构、门户入口和指定文档可达性通过。 |
| `warning` | 当前仍可通过，但 README 可能偏长或直接文档链接过多，建议人工复核。 |
| `error` | 缺少核心章节/门户链接、README 超出边界、分类未暴露或指定文档不可达。 |

结构检查不判断文字事实是否正确。执行者仍需根据代码、配置、migration 和最新进度文档核实能力声明。

## 7. clone 后直接使用

`.agents/skills/campusos-readme-update/` 是仓库内可提交的发现桥接。Windows、Linux 或 macOS 用户 clone 项目后，
只要从仓库目录内启动 Codex，即可通过 `$campusos-readme-update` 调用，不需要复制到个人系统目录。规范源文件
只在 `skills/sources/` 修改；修改后运行仓库 Skill 同步脚本并校验，已打开的会话若未刷新则重启一次。
