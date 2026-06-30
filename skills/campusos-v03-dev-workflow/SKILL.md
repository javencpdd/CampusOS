---
name: campusos-v03-dev-workflow
description: CampusOS v0.3-dev development workflow for completing implementation tasks in /home/jack/bbs/bbs01/CampusOS, keeping progress docs named v0.3.*-dev.md, running project validation, and committing each completed task. Use when working on CampusOS v0.3-dev features, plugin runtime work, Host API permission work, SDK/CLI tasks, v0.3 progress documentation, or any request that says to update docs and commit after each v0.3-dev task.
---

# CampusOS v0.3-dev Workflow

This is the legacy v0.3-dev workflow. For v0.4-dev, v0.5-dev, and later CampusOS stages, prefer `campusos-dev-workflow`.

## Core Rule

Complete one coherent v0.3-dev task at a time. For each completed task:

1. Implement the code or documentation change.
2. Add or update a progress document under `docs/进度/v0.3-dev/` named `v0.3.X-dev.md`.
3. Run validation appropriate to the touched files.
4. Commit locally with a focused commit message.
5. Do not push unless the user explicitly requests it.

## Repository Assumptions

Default repository path:

```text
/home/jack/bbs/bbs01/CampusOS
```

Key planning documents:

```text
docs/项目计划v3/02-v0.3-dev计划书.md
docs/进度/v0.3-dev/00-v0.3-dev启动确认.md
docs/进度/v0.3-dev/01-v0.3-dev开发指南.md
docs/进度/v0.3-dev/02-v0.3-dev首轮任务清单.md
docs/进度/v0.3-dev-pre/前置稳定化完成说明.md
```

Read `references/campusos-v03-dev.md` when the task involves selecting the next v0.3-dev task, writing progress docs, or deciding validation scope.

## Workflow

### 1. Pre-flight

Run:

```bash
git status -sb
git branch --show-current
```

If the worktree is dirty, distinguish user changes from changes needed for the current task. Do not revert unrelated changes.

Read the current v0.3-dev plan and latest progress file before implementation:

```bash
find docs/进度/v0.3-dev -maxdepth 1 -type f | sort
sed -n '1,220p' docs/进度/v0.3-dev/02-v0.3-dev首轮任务清单.md
```

### 2. Select Scope

Prefer the next P0 item from `02-v0.3-dev首轮任务清单.md`.

Keep the scope narrow:

- Runtime interface and Wasm Runtime work belongs under `internal/plugin`.
- Example plugin work belongs under `examples/plugins`.
- Progress documentation belongs under `docs/进度/v0.3-dev`.
- User-facing plan updates belong under `docs/项目计划v3` only when the plan status changes.

### 3. Implement

Follow existing project patterns first. Do not introduce large new abstractions unless the task requires them.

For code edits, use `apply_patch`. For formatting and validation commands, use shell commands.

### 4. Progress Documentation

Create or update exactly one task progress document for each completed task:

```text
docs/进度/v0.3-dev/v0.3.X-dev.md
```

Use the next available integer X. If no `v0.3.X-dev.md` exists, start at:

```text
v0.3.0-dev.md
```

Minimum sections:

```markdown
# CampusOS v0.3.X-dev 进度说明

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
go test ./...
```

Run frontend builds when frontend files, shared API contracts, or CI/CD behavior change:

```bash
cd web && pnpm build
cd admin && pnpm build
```

Run migration checks when migrations or migration scripts change:

```bash
make migrate-up
make migrate-status
```

Run workflow YAML parsing when `.github/workflows` changes:

```bash
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
skills/campusos-v03-dev-workflow/scripts/check_v03_task.sh /home/jack/bbs/bbs01/CampusOS
```

### 6. Commit

Commit after validation passes:

```bash
git status -sb
git add <changed-files>
git commit -m "<type>: <short v0.3-dev summary>"
```

Use one focused commit per completed task. Do not commit unrelated dirty files.

Common commit types:

- `feat:` for implementation features.
- `fix:` for bug fixes.
- `docs:` for documentation-only tasks.
- `test:` for test-only changes.
- `chore:` for scripts or workflow maintenance.

## Output Summary

After committing, report:

- Completed task.
- Progress doc path.
- Validation commands and result.
- Commit hash and message.
- Whether the branch is ahead of its upstream.
