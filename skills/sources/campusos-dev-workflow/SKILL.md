---
name: campusos-dev-workflow
description: Complete and locally commit one coherent CampusOS version-stage implementation or documentation task with progress evidence and impact-based validation. Use only when the user explicitly invokes this Skill or explicitly requests a Git commit; do not use it for ordinary uncommitted development work.
---

# CampusOS Dev Workflow

> 更新时间：2026-08-31

## Core Rule

Complete one coherent CampusOS development task at a time for the active version stage. For each completed task:

1. Identify the target stage from the explicit request and current plan status. `v0.14-dev` is the latest completed development stage; do not silently reopen it or start the reserved `v1.1` stage without an explicit task and formal plan.
2. Read the matching plan and latest progress documents.
3. Implement the scoped code or documentation change.
4. Add or update one progress document under `docs/进度/<stage>/`.
5. Run validation appropriate to the touched files.
6. Commit locally with a focused commit message.
7. Do not push unless the user explicitly requests it.

Read `references/campusos-versioned-dev.md` when selecting a stage, choosing the next task, writing progress docs, or deciding validation scope.

## Repository Assumptions

Use the active CampusOS workspace or derive the repository root from this Skill directory. Never assume a particular
drive, home directory, or operating system.

Common version-stage paths:

```text
docs/项目计划书v0/项目计划v0.14/
docs/进度/v0.14-dev/
```

If a future stage does not have a directory yet, create it only when the task requires a progress document for that stage.

## Workflow

### 1. Pre-flight

Run:

```bash
git status -sb
git branch --show-current
```

If the worktree is dirty, distinguish existing user changes from changes needed for the current task. Do not revert unrelated changes.

Determine the stage in this order:

1. Use the explicit user request when it names a stage, such as `v0.13-dev`.
2. Use the latest active progress directory under `docs/进度/`.
3. Use the latest project plan directory under `docs/项目计划书v0/项目计划v0.*`, but first confirm it is still an active implementation authority.
4. If still unclear, ask one concise question before editing.

### 2. Read Context

Read the matching plan and latest progress docs for the chosen stage.

Examples:

```bash
find docs/进度/v0.13-dev -maxdepth 1 -type f | sort -V
sed -n '1,240p' docs/项目计划书v0/项目计划v0.13/00-v0.13版本计划书.md
```

For historical compatibility work, read that stage's plan and progress only after confirming it is not the current
operation authority.

### 3. Select Scope

Prefer the next incomplete P0/P1 item from the current version plan. Keep the scope narrow and coherent.

Examples:

- Plugin runtime and Host API work belongs under `internal/plugin`.
- SDK/CLI work belongs under `sdk/`, `cmd/campusosctl`, and relevant docs.
- Frontend user experience work belongs under `web/` or `admin/`.
- Progress documentation belongs under `docs/进度/<stage>/`.
- User-facing plan updates belong under `docs/项目计划书v0/项目计划v0.*/` only when plan scope or status changes.

### 4. Progress Documentation

Create or update exactly one task progress document for each completed task:

```text
docs/进度/<stage>/<stage-without-dev>.<task-number>-dev.md
```

Examples:

```text
docs/进度/v0.13-dev/v0.13.35-dev.md
```

Use the next available task number in that stage. Keep the standard sections:

```markdown
# CampusOS v0.X.Y-dev 进度说明

> 日期：YYYY-MM-DD
> 状态：已完成

## 1. 本次目标
## 2. 完成内容
## 3. 修改文件
## 4. 验证结果
## 5. 后续任务
```

Use concrete file paths and exact validation commands. If a validation step was not run, state why.

### 5. Validation

Always run the non-destructive base checks:

```bash
git diff --check
python scripts/check-line-endings.py --include-untracked
```

Run targeted and then broader tests according to the touched surface. Use a workspace-local `GOCACHE` on Windows and
`/tmp/campusos-go-cache` on Linux. Do not treat a documentation-only change as authority to run stateful migrations.

Add validation based on changed files:

```bash
cd web && pnpm build
cd admin && pnpm build
make migrate-up
make migrate-status
python3 - <<'PY'
from pathlib import Path
import yaml
for p in Path('.github/workflows').glob('*.yml'):
    yaml.safe_load(p.read_text())
    print(p, 'ok')
PY
```

Optional helper:

```bash
skills/sources/campusos-dev-workflow/scripts/check_task.sh
skills/sources/campusos-dev-workflow/scripts/check_task.sh v0.13-dev
```

### 6. Commit

Commit only because this Skill is explicitly selected or the user explicitly requested a commit, and only after validation
passes:

```bash
git status -sb
git add <changed-files>
git commit -m "<type>: <short CampusOS task summary>"
```

Use one focused commit per completed task. Do not commit unrelated dirty files.

Common commit types:

- `feat:` for implementation features.
- `fix:` for bug fixes.
- `docs:` for documentation-only tasks.
- `test:` for test-only changes.
- `chore:` for scripts, skill updates, or workflow maintenance.

## Output Summary

After committing, report:

- Completed task.
- Target stage.
- Progress doc path, if one was created or updated.
- Validation commands and result.
- Commit hash and message.
- Whether the branch is ahead of upstream.
