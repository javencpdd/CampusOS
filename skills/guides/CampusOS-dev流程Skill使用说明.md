# CampusOS 开发流程 Skill 使用说明

> 更新时间：2026-08-28
> 当前基线：`v0.13.0`
> 实现目录：`skills/sources/campusos-dev-nocommit/`、`skills/sources/campusos-dev-workflow/`

## 1. 两个入口的区别

| Skill | 适用场景 | Git 行为 |
| --- | --- | --- |
| `campusos-dev-nocommit` | 普通实现、修复、文档同步和当前阶段任务；推荐默认入口 | 不 commit、不 push |
| `campusos-dev-workflow` | 用户明确要求“完成并本地提交”，或显式点名该 Skill | 验证后只提交本任务文件；不自动 push |

旧文档曾让 `campusos-dev-nocommit` 的描述和界面提示指向 commit 流程，现已纠正。没有明确提交授权时，Agent
必须使用无提交入口，不能因为“任务已完成”自行 commit。

## 2. 推荐调用

普通任务：

```text
使用 $campusos-dev-nocommit 完成当前 CampusOS 任务，更新进度文档并验证，但不要提交或 push。
```

明确需要本地 commit：

```text
使用 $campusos-dev-workflow 完成、验证并本地提交一个 CampusOS 任务，不要 push。
```

两个 Skill 都会优先使用用户明确指定的阶段；未指定时从 `docs/进度/`、`docs/计划书总结/` 和当前计划推断。
当前实施阶段是 `v0.14-dev`，发布基线仍为 `v0.13.0`；旧 v0.3–v0.13 计划只在兼容或历史审查时读取。

## 3. 工作流程

1. 从当前工作区或 Skill 相对位置识别仓库，不硬编码 Windows 盘符或 Linux Home。
2. 执行 `git status --short`，保护用户已有修改。
3. 读取当前计划权威入口、对应模块、最新进度和任务相关文档。
4. 一次完成一个可独立验证的任务。
5. 在 `docs/进度/<stage>/` 使用下一个可用编号记录真实实现和验证。
6. 运行基础门禁与影响范围测试。
7. 只有 `campusos-dev-workflow` 被明确授权时才创建本地 commit；两个入口都不自动 push。

## 4. 基础检查

Windows PowerShell：

```powershell
git diff --check
python scripts/check-line-endings.py --include-untracked
$env:GOCACHE = (Join-Path (Get-Location) '.cache\go-build')
go test ./internal/modules/<owner>/...
```

Linux、WSL2、Git Bash：

```bash
git diff --check
python3 scripts/check-line-endings.py --include-untracked
GOCACHE=/tmp/campusos-go-cache go test ./internal/modules/<owner>/...
```

文档任务还需执行 `python scripts/check-doc-links.py`；Docker、migration、Web/Admin、OpenAPI 等任务按专项 Skill
和 Makefile 门禁追加验证。纯文档任务不必无条件运行全仓 Go 测试，状态性数据库命令也不能在没有范围确认时
直接作用于共享开发数据。

## 5. 辅助脚本

```bash
skills/sources/campusos-dev-nocommit/scripts/check_task.sh v0.14-dev
skills/sources/campusos-dev-workflow/scripts/check_task.sh v0.14-dev
```

脚本适用于 Bash，会显示阶段、计划、进度和 Git 状态。Linux 默认继续执行全仓 Go 测试；Windows Git Bash
会在 `auto` 模式明确跳过全仓测试，因为当前 managed-process 测试要求 Linux 可执行文件环境，并提示改在
Docker/WSL2/Linux 跑完整门禁。可以用 `CAMPUSOS_SKILL_RUN_GO=true|false` 显式覆盖。Windows 仍须直接执行
任务相关的定向 Go 测试；脚本跳过不是“测试通过”的替代证据。

## 6. 仓库直用与维护

仓库 `skills/sources/` 是版本化事实源，`.agents/skills/` 是 clone 后直接可发现的桥接，不需要安装到用户级
Codex Skill 目录。Skill 的 `SKILL.md` 正文和本使用说明都要写明更新时间。版本、migration、运行方式、文档入口
或验证门禁变化时，需要同步引用资料和 `agents/openai.yaml`，再运行仓库 Skill 同步与校验。
