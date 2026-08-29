# CampusOS Versioned Development Reference

> 更新时间：2026-08-29

## Stage Detection

Use this order:

1. Explicit user request, for example `v0.13-dev`.
2. Existing progress directory under `docs/进度/`, sorted by version.
3. Existing project plan directory under `docs/项目计划v*`, sorted by plan version.
4. Ask a concise clarification only if no defensible stage can be inferred.

Current stage name:

```text
v0.14-dev
```

Progress documents use:

```text
docs/进度/<stage>/v0.X.Y-dev.md
```

Examples:

```text
docs/进度/v0.13-dev/v0.13.35-dev.md
```

## Plan Mapping

| Implementation stage | Preferred plan path |
| --- | --- |
| Historical stages | Matching `docs/项目计划vN/` and `docs/进度/v0.N-dev/` for traceability |
| `v0.14-dev` | `docs/项目计划v14/00-v14版本计划书.md` plus the latest numbered `docs/进度/v0.14-dev/` record |
| Future stages | `docs/计划书总结/README.md`, then the latest matching plan or user-provided plan |

If the exact plan file is unknown, list files in the plan directory and read the most relevant top-level plan first.

## Task Selection

When the user says to continue the next task:

1. Read the current stage plan.
2. Read the latest progress document for that stage.
3. Prefer the next incomplete P0 item.
4. If P0 is complete, choose the next P1 item with clear acceptance criteria.
5. Avoid large mixed tasks. Split unrelated implementation, documentation, and cleanup unless they must ship together.

## Progress Document Numbering

Use the next available numeric suffix for the selected stage.

Examples:

```bash
find docs/进度/v0.13-dev -maxdepth 1 -type f -name 'v0.13.*-dev.md' | sort -V
```

If no numbered progress document exists for a new stage, start at:

```text
v0.X.0-dev.md
```

Do not rename existing progress documents unless explicitly requested.

## Validation Matrix

| Change type | Required validation |
| --- | --- |
| Go backend | `GOCACHE=/tmp/campusos-go-cache go test ./...` |
| Plugin runtime | `GOCACHE=/tmp/campusos-go-cache go test ./...`; add runtime-specific tests where possible |
| SDK/CLI | Targeted package tests plus `GOCACHE=/tmp/campusos-go-cache go test ./...` |
| Migration | isolated migration drill, `make database-check`, architecture sync; do not mutate a shared dev database by default |
| Frontend | `cd web && pnpm build`; `cd admin && pnpm build` when relevant |
| Docker dev | `make docker-dev-test`, `make docker-deploy-check`, and runtime HTTP checks when a stack is available |
| Workflow | Python YAML parse; `git diff --check` |
| Docs only | `python scripts/check-doc-links.py`, README/Skill-specific checks, `git diff --check` |
| Skill updates | `quick_validate.py <skill-dir>`; run relevant bundled scripts |

Always run the base checks:

```bash
git diff --check
python scripts/check-line-endings.py --include-untracked
```

If validation is skipped, record the reason in the progress document or final summary.

## Documentation Standard

Each progress document should record:

- Objective.
- Actual changes.
- Files changed.
- Validation commands and pass/fail result.
- Risks or follow-up tasks.
- Explicit statement that the work remains uncommitted and was not pushed.

Prefer concise tables over long prose.

## Current Baseline

The release baseline remains `v0.13.0`; the active worktree has migrations through `000049`. The active implementation stage is
`v0.14-dev`; its formal plan is `docs/项目计划v14/00-v14版本计划书.md`, and the latest evidence is
`docs/进度/v0.14-dev/v0.14.3-dev.md`. AcademicTerm, Storage Object, Schedule Guard, historical dry-run/adoption tooling,
reconcile apply controls and Personal Documents safe degradation are implemented. The v0.14 Final gates still require an authorized
operator decision for real historical-data apply plus Docker migration/restore, browser and release evidence. Treat v0.3-v0.13 plans
as historical unless a compatibility task explicitly targets them.
