---
name: campusos-dev-nocommit
description: Complete CampusOS implementation or documentation tasks with version-stage context, progress evidence, and impact-based validation, but never commit or push. Use for ordinary CampusOS development, bug fixes, documentation synchronization, or continuing the current v0.X-dev task when the user has not explicitly requested a Git commit.
---

# CampusOS Dev No Commit

> 更新时间：2026-08-31

## Core Rule

Complete one coherent CampusOS development task at a time for the active version stage. For each completed task:

1. Identify the target stage from the explicit request and current plan status. `v0.14-dev` is the latest completed development stage; do not silently reopen it or start the reserved `v1.1` stage without an explicit task and formal plan.
2. Read the matching plan and latest progress documents.
3. Implement the scoped code or documentation change.
4. Add or update one progress document under `docs/进度/<stage>/`.
5. Run validation appropriate to the touched files.
6. Leave all changes uncommitted and never push.

Read `references/campusos-versioned-dev.md` when selecting a stage, choosing the next task, writing progress docs, or deciding validation scope.

## Repository Assumptions

Use the active CampusOS workspace or derive the repository root from this Skill directory. Never assume a particular
drive, home directory, or operating system.

Common version-stage paths:

```text
docs/项目计划v0.14/
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
3. Use the latest project plan directory under `docs/项目计划v0.*`, but first confirm it is still an active implementation authority.
4. If still unclear, ask one concise question before editing.

### 2. Read Context

Read the matching plan and latest progress docs for the chosen stage.

Examples:

```bash
find docs/进度/v0.13-dev -maxdepth 1 -type f | sort -V
sed -n '1,240p' docs/项目计划v0.13/00-v0.13版本计划书.md
```

For historical compatibility work, read that stage's plan and progress only after confirming it is not the current
operation authority.

### 3. Select Scope

Prefer the next incomplete P0/P1 item from the current version plan. Keep the scope narrow and coherent.

Examples:

- Core/Feature work belongs under `internal/modules/`, with descriptors under `modules/` when needed.
- Plugin runtime and Host API work belongs under `internal/plugin`.
- Docker development belongs under `scripts/docker-dev.*`, `compose.dev.yml`, and `deploy/docker/`.
- Frontend user experience work belongs under `web/` or `admin/` and needs API-contract verification when data is involved.
- Progress documentation belongs under `docs/进度/<stage>/`.
- User-facing plan updates belong under `docs/项目计划v*/` only when plan scope or status changes.

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

Add validation based on changed files. Run targeted Go tests before `go test ./...`; use a workspace-local `GOCACHE` on
Windows and `/tmp/campusos-go-cache` on Linux. Do not run stateful migration commands against the developer database merely
for a documentation-only change.

Examples:

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
skills/sources/campusos-dev-nocommit/scripts/check_task.sh
skills/sources/campusos-dev-nocommit/scripts/check_task.sh v0.13-dev
```

## Output Summary

Report:

- Completed task.
- Target stage.
- Progress doc path, if one was created or updated.
- Validation commands and result.
- Explicitly state that no commit or push was performed.
