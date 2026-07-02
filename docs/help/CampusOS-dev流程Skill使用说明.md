# CampusOS 通用开发流程 Skill 使用说明

> 日期：2026-07-01
> 推荐 Skill 名称：`campusos-dev-workflow`
> 仓库内位置：`/home/jack/bbs/bbs01/CampusOS/skills/campusos-dev-workflow`
> Codex 用户级位置：`/home/jack/.codex/skills/campusos-dev-workflow`
> 兼容 Skill：`campusos-v03-dev-workflow`

## 1. 用途

`campusos-dev-workflow` 是 CampusOS 项目的通用开发流程 skill，不再只绑定 `v0.3-dev`。

它适用于：

| 阶段 | 是否适用 |
| --- | --- |
| `v0.3-dev` | 适用 |
| `v0.4-dev` | 适用 |
| `v0.5-dev` | 适用 |
| 后续 `v0.X-dev` | 适用 |

它固化以下流程：

1. 识别当前目标阶段，例如 `v0.4-dev`。
2. 阅读对应项目计划和最新进度文档。
3. 一次只完成一个明确任务。
4. 更新对应阶段的进度文档。
5. 执行验证。
6. 创建本地 commit。
7. 默认不 push，除非用户明确要求。

## 2. 推荐触发方式

继续当前阶段的下一个任务：

```text
使用 campusos-dev-workflow 继续完成下一个 CampusOS 开发任务，更新进度文档并提交。
```

指定版本阶段：

```text
使用 campusos-dev-workflow 继续完成 v0.4-dev 的下一个任务，更新进度文档并提交。
```

英文显式触发：

```text
Use $campusos-dev-workflow to complete the next CampusOS v0.4-dev task with docs and commit.
```

## 3. 版本阶段识别规则

skill 会按以下顺序判断目标阶段：

| 顺序 | 规则 |
| --- | --- |
| 1 | 用户明确写出 `v0.4-dev`、`v0.5-dev` 等阶段 |
| 2 | 读取 `docs/进度/` 下最新的 `v0.X-dev` 目录 |
| 3 | 读取 `docs/项目计划v*` 下最新计划 |
| 4 | 仍不明确时，先询问用户 |

## 4. 文档路径规范

计划文档通常位于：

```text
docs/项目计划v3/
docs/项目计划v4/
docs/项目计划v5/
```

进度文档位于：

```text
docs/进度/v0.4-dev/v0.4.2-dev.md
docs/进度/v0.5-dev/v0.5.0-dev.md
```

新阶段如果还没有进度目录，可以在实际开始该阶段任务时创建：

```bash
mkdir -p docs/进度/v0.5-dev
```

## 5. 辅助检查脚本

仓库内通用脚本：

```bash
skills/campusos-dev-workflow/scripts/check_task.sh
```

显式指定阶段：

```bash
skills/campusos-dev-workflow/scripts/check_task.sh /home/jack/bbs/bbs01/CampusOS v0.4-dev
```

只指定阶段，仓库路径使用默认推断：

```bash
skills/campusos-dev-workflow/scripts/check_task.sh v0.4-dev
```

脚本会执行：

| 检查项 | 说明 |
| --- | --- |
| Git 状态 | 显示当前分支和工作区 |
| 阶段识别 | 显示当前目标阶段 |
| 计划文档 | 列出对应 `docs/项目计划v*` 文件 |
| 进度文档 | 列出对应 `docs/进度/v0.X-dev` 文件 |
| 空白检查 | 执行 `git diff --check` |
| Go 测试 | 执行 `GOCACHE=/tmp/campusos-go-cache go test ./...` |

## 6. 与旧 v0.3 Skill 的关系

旧 skill：

```text
campusos-v03-dev-workflow
```

保留为历史兼容入口，适合已有提示词继续使用。但后续新任务建议使用：

```text
campusos-dev-workflow
```

原因：

| 项目 | `campusos-v03-dev-workflow` | `campusos-dev-workflow` |
| --- | --- | --- |
| 目标阶段 | 固定 `v0.3-dev` | 支持 `v0.3-dev`、`v0.4-dev`、`v0.5-dev` 和后续阶段 |
| 进度目录 | 固定 `docs/进度/v0.3-dev/` | 根据阶段选择 `docs/进度/<stage>/` |
| 计划文档 | 固定 v3 计划 | 根据阶段读取 v3/v4/v5 等计划 |
| 后续维护 | 兼容维护 | 推荐主线 |

## 7. 同步到 Codex 用户级目录

仓库内副本用于版本管理。若要让 Codex 在新环境自动发现该 skill，执行：

```bash
mkdir -p /home/jack/.codex/skills
rsync -a skills/campusos-dev-workflow/ /home/jack/.codex/skills/campusos-dev-workflow/
```

如需保留旧触发方式，也可以继续同步旧目录：

```bash
rsync -a skills/campusos-v03-dev-workflow/ /home/jack/.codex/skills/campusos-v03-dev-workflow/
```

## 8. 注意事项

- 每次只处理一个明确任务。
- 不要把无关改动混入同一个 commit。
- 如果用户指定了版本阶段，以用户指定为准。
- 如果当前任务是纯文档或 skill 更新，也应运行 `git diff --check` 和 skill 校验。
- 默认只提交本地 commit，不 push。

