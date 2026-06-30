# CampusOS v0.3-dev 流程 Skill 使用说明

> 日期：2026-07-01
> Skill 名称：`campusos-v03-dev-workflow`
> 仓库内位置：`/home/jack/bbs/bbs01/CampusOS/skills/campusos-v03-dev-workflow`
> Codex 用户级位置：`/home/jack/.codex/skills/campusos-v03-dev-workflow`

## 1. 用途

`campusos-v03-dev-workflow` 用于复用 CampusOS v0.3-dev 阶段的开发流程。

当前项目已将该 skill 及其相关工具移植到仓库内 `skills/campusos-v03-dev-workflow/`，便于后续开发、备份和迁移。`/home/jack/.codex/skills/campusos-v03-dev-workflow` 仍可作为 Codex 自动发现位置；如果更换机器或重装 Codex，可以从仓库内副本同步到用户级 skill 目录。

它固化以下规则：

1. 一次只完成一个明确的 v0.3-dev 任务。
2. 每次任务完成后，在 `docs/进度/v0.3-dev/` 下新增或更新 `v0.3.X-dev.md`。
3. 每次提交前运行必要验证。
4. 每次完成后创建本地 commit。
5. 默认不 push，除非用户明确要求。

## 2. 触发方式

在后续对 Codex 发起任务时，可以直接说明使用该 skill：

```text
使用 campusos-v03-dev-workflow 继续完成下一个 v0.3-dev 任务，更新进度文档并提交。
```

也可以使用显式 skill 名称：

```text
Use $campusos-v03-dev-workflow to complete the next CampusOS v0.3-dev task with docs and commit.
```

适用任务：

| 场景 | 是否适用 |
| --- | --- |
| Wasm Runtime 开发 | 适用 |
| Host API 权限检查 | 适用 |
| 插件日志、插件存储 | 适用 |
| SDK/CLI 初版 | 适用 |
| v0.3-dev 进度文档维护 | 适用 |
| 非 CampusOS 项目 | 不适用 |

## 3. Skill 执行流程

该 skill 会要求 Codex 按以下顺序执行：

| 步骤 | 内容 |
| --- | --- |
| 1 | 检查 `git status -sb` 和当前分支 |
| 2 | 阅读 v0.3-dev 计划和最新进度文档 |
| 3 | 从首轮任务清单中选择一个明确任务 |
| 4 | 实现代码或文档改动 |
| 5 | 新增 `docs/进度/v0.3-dev/v0.3.X-dev.md` |
| 6 | 执行验证 |
| 7 | 本地 commit |
| 8 | 输出完成内容、验证结果和 commit hash |

## 4. 文档命名规范

进度文档必须使用：

```text
docs/进度/v0.3-dev/v0.3.X-dev.md
```

示例：

```text
v0.3.0-dev.md
v0.3.1-dev.md
v0.3.2-dev.md
```

如果目录中还没有 `v0.3.X-dev.md`，从 `v0.3.0-dev.md` 开始。

## 5. 默认验证命令

所有任务至少运行：

```bash
git diff --check
go test ./...
```

涉及前端时运行：

```bash
cd web && pnpm build
cd admin && pnpm build
```

涉及迁移时运行：

```bash
make migrate-up
make migrate-status
```

涉及 workflow 时运行：

```bash
python3 - <<'PY'
from pathlib import Path
import yaml
for p in Path('.github/workflows').glob('*.yml'):
    yaml.safe_load(p.read_text())
    print(p, 'ok')
PY
```

## 6. 辅助检查脚本

skill 内置了辅助脚本。仓库内副本推荐使用：

```bash
skills/campusos-v03-dev-workflow/scripts/check_v03_task.sh
```

也可以显式指定仓库路径：

```bash
skills/campusos-v03-dev-workflow/scripts/check_v03_task.sh /home/jack/bbs/bbs01/CampusOS
```

该脚本会检查：

| 检查项 | 说明 |
| --- | --- |
| Git 状态 | 显示当前分支和工作区状态 |
| 进度文档 | 列出已有 `v0.3.*-dev.md` |
| 空白检查 | 执行 `git diff --check` |
| 后端测试 | 执行 `go test ./...` |

脚本默认使用：

```bash
GOCACHE=/tmp/campusos-go-cache
```

这样可以避免受限环境中默认 Go 构建缓存目录不可写导致测试失败。

## 7. 仓库内 Skill 目录结构

当前仓库内副本包含：

```text
skills/campusos-v03-dev-workflow/
├── SKILL.md
├── agents/
│   └── openai.yaml
├── references/
│   └── campusos-v03-dev.md
└── scripts/
    └── check_v03_task.sh
```

| 文件 | 作用 |
| --- | --- |
| `SKILL.md` | skill 主说明，定义触发场景、执行流程、验证和提交规则 |
| `agents/openai.yaml` | UI 展示元数据 |
| `references/campusos-v03-dev.md` | v0.3-dev 任务优先级、文档规范和验证矩阵 |
| `scripts/check_v03_task.sh` | 本地检查脚本，执行状态检查、空白检查和 Go 测试 |

## 8. 同步到 Codex 用户级 Skill 目录

如果需要让 Codex 在新环境中自动发现该 skill，可以从仓库内副本同步到用户级目录：

```bash
mkdir -p /home/jack/.codex/skills
rsync -a skills/campusos-v03-dev-workflow/ /home/jack/.codex/skills/campusos-v03-dev-workflow/
```

同步后可通过以下方式触发：

```text
使用 campusos-v03-dev-workflow 继续完成下一个 v0.3-dev 任务，更新进度文档并提交。
```

## 9. 当前 v0.3-dev 首轮建议顺序

| 顺序 | 任务 |
| --- | --- |
| 1 | Runtime 接口复核 |
| 2 | Wasm Runtime 骨架 |
| 3 | Wasm 事件调用 |
| 4 | Runtime 超时和错误隔离 |
| 5 | Host API 权限检查 |
| 6 | 插件日志落库 |
| 7 | `hello-wasm` 示例插件 |
| 8 | SDK/CLI 初版 |

## 10. 注意事项

- skill 已同时保留在 CampusOS 仓库内和用户级 Codex 目录中。
- 仓库内副本用于项目归档、迁移和版本管理。
- 用户级目录用于 Codex 自动发现和直接触发。
- 如果后续更新 skill，建议先更新仓库内副本，再按需同步到 `/home/jack/.codex/skills/campusos-v03-dev-workflow`。
- 每次任务只提交相关文件，不混入无关修改。
