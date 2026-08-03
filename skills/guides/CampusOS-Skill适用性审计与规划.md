# CampusOS Skill 适用性审计与规划

> 更新时间：2026-08-03
> 审计范围：`skills/sources/*/SKILL.md`、Bundled Resources、`agents/openai.yaml`、`skills/guides/` 与 v0.13.21–v0.13.34 近期任务

## 1. 审计结论

原有 5 个可发现 Skill 的方向仍然有效，但只有 README Skill 基本符合当前路径；其他 Skill 存在 Linux Home
硬编码、旧版本示例、migration 基线落后一位、Admin 文件旧路径或 commit/no-commit 触发冲突。全部已修订并
补充 `2026-08-03` 更新时间。

`skills/guides/gpt-5.5-base-instructions.md` 没有标准目录和 `SKILL.md`，不是可发现 Skill，本轮不把它登记为
项目 Skill，也不让它参与触发。若未来仍需保留，应作为明确的历史提示参考治理，而不是伪装成 Skill。

## 2. 现有 Skill 适用性

| Skill | 当前判断 | 本轮修订 |
| --- | --- | --- |
| `campusos-project-onboarding` | 继续适用，作为新会话/上下文恢复入口 | 动态仓库根、migration `000042`、Windows Docker 证据、`up/rebuild` 边界 |
| `campusos-dev-nocommit` | 继续适用，并作为普通开发默认入口 | 删除 commit 暗示，修复错误的 `openai.yaml`，明确不 commit/push |
| `campusos-dev-workflow` | 有条件适用 | 仅在用户显式点名或要求 commit 时触发，保留不自动 push |
| `campusos-readme-update` | 继续适用 | 增加 `docs-site` 同步和 Windows Go cache/跨平台说明 |
| `campusos-data-architecture-sync` | 继续适用并扩展 | 修正 Admin 路径，增加 migration 不可变、冗余、回滚与 drill 合同 |

开发助手的真实运行还发现 Windows Git Bash 会调用 Windows Go，而当前 `internal/plugin/grpc` managed-process
测试使用 Unix 无扩展名可执行文件假设。辅助脚本现会在 Windows `auto` 模式明确跳过全仓测试并提示转到
Docker/WSL2/Linux；它不会把跳过写成通过。任务相关的 Windows 定向测试仍必须运行。

## 3. 近期任务到 Skill 的覆盖

| 近期任务 | 处理方式 |
| --- | --- |
| README、Help、官方文档、Git/PR 使用说明 | 现有 README Skill + 开发流程足够，不新增 Git 专用 Skill |
| 跨平台换行、PowerShell/Bash 脚本 | 现有门禁纳入开发/Docker Skill，不再创建单一格式转换 Skill |
| migration 冗余、Schema、数据目录 | 扩充数据架构 Skill；避免拆成重叠的 migration Skill |
| Docker 首配、SMTP、代理、LAN、日志、热更新、`up/rebuild` | 新增 `campusos-docker-development` |
| 分组聚合、二手价格、通知、头像、空间、后台邮箱、动态路由 | 新增 `campusos-webui-regression` |

## 4. 暂不新增的候选

- GitHub/PR：现有 `sh/git_*`、Help 和 `gh` 流程已确定，单独 Skill 的复用收益不足。
- OpenAPI 单项：已有 contracts/generated-files 门禁；等接口版本治理成为独立主线再评估。
- 生产发布/安全供应链：后续路线尚未正式立项 v14，当前创建会把候选计划误写成已承诺流程。
- 插件市场/MCP/Agent Runner：仍是受控或候选范围，必须等协议和安全边界正式立项。

## 5. 维护规则

1. 每个项目 `SKILL.md` 正文和 `skills/guides/` 使用说明写明更新时间；frontmatter 仍只保留 `name` 和
   `description`。
2. 版本、migration、目录、运行命令、文档权威入口或验证门禁变化时，同步 Skill、references 和界面元数据。
3. 新 Skill 必须解决独立且重复的工作流；能扩充现有 Skill 时不新增。
4. 所有 Skill 运行 `quick_validate.py`；有脚本的 Skill 还要执行语法和真实只读/隔离验证。
5. 仓库 `skills/` 是版本化事实源；安装到用户级目录需要单独授权，不在审计任务中自动覆盖。
