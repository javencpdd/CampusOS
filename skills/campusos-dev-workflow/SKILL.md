---
name: campusos-dev-workflow
description: CampusOS versioned development workflow for completing implementation or documentation tasks in /home/jack/bbs/bbs01/CampusOS across v0.3-dev, v0.4-dev, v0.5-dev, and later stages. Use when a request mentions CampusOS version-stage work, asks to continue the next dev task, update progress docs, run validation, or commit after a CampusOS task.
---

# CampusOS Dev Workflow

## Core Rule

Complete one coherent CampusOS development task at a time for the active version stage. For each completed task:

1. Identify the target stage, such as `v0.4-dev` or `v0.5-dev`.
2. Read the matching plan and latest progress documents.
3. Implement the scoped code or documentation change.
4. Add or update one progress document under `docs/进度/<stage>/`.
5. Run validation appropriate to the touched files.
6. Commit locally with a focused commit message.
7. Do not push unless the user explicitly requests it.

Read `references/campusos-versioned-dev.md` when selecting a stage, choosing the next task, writing progress docs, or deciding validation scope.

## Repository Assumptions

Default repository path:

```text
/home/jack/bbs/bbs01/CampusOS
```

Common version-stage paths:

```text
docs/项目计划v3/
docs/项目计划v4/
docs/项目计划v5/
docs/进度/v0.3-dev/
docs/进度/v0.4-dev/
docs/进度/v0.5-dev/
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

1. Use the explicit user request when it names a stage, such as `v0.4-dev`.
2. Use the latest active progress directory under `docs/进度/`.
3. Use the latest project plan directory under `docs/项目计划v*`.
4. If still unclear, ask one concise question before editing.

### 2. Read Context

Read the matching plan and latest progress docs for the chosen stage.

Examples:

```bash
find docs/进度/v0.4-dev -maxdepth 1 -type f | sort -V
sed -n '1,240p' docs/项目计划v4/00-v4版本计划书.md
```

For v0.3-specific work, also read:

```text
docs/进度/v0.3-dev/02-v0.3-dev首轮任务清单.md
docs/进度/v0.3-dev-pre/前置稳定化完成说明.md
```

### 3. Select Scope

Prefer the next incomplete P0/P1 item from the current version plan. Keep the scope narrow and coherent.

Examples:

- Plugin runtime and Host API work belongs under `internal/plugin`.
- SDK/CLI work belongs under `sdk/`, `cmd/campusosctl`, and relevant docs.
- Frontend user experience work belongs under `web/` or `admin/`.
- Progress documentation belongs under `docs/进度/<stage>/`.
- User-facing plan updates belong under `docs/项目计划v*/` only when plan scope or status changes.

### 4. Progress Documentation

Create or update exactly one task progress document for each completed task:

```text
docs/进度/<stage>/<stage-without-dev>.<task-number>-dev.md
```

Examples:

```text
docs/进度/v0.4-dev/v0.4.2-dev.md
docs/进度/v0.5-dev/v0.5.0-dev.md
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

Always run:

```bash
git diff --check
GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test ./...
```

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
skills/campusos-dev-workflow/scripts/check_task.sh
skills/campusos-dev-workflow/scripts/check_task.sh /home/jack/bbs/bbs01/CampusOS v0.4-dev
```

### 6. Commit

Commit after validation passes:

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
