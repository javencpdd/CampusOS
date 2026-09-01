# CampusOS Versioned Development Reference

> 更新时间：2026-09-01（Asia/Shanghai）

## Stage Detection

Use this order:

1. Explicit user request, for example `v0.13-dev`.
2. Existing progress directory under `docs/进度/`, sorted by version.
3. Existing project plan directory under `docs/项目计划书v<major>/项目计划v<version>/`, sorted by plan version, after confirming it is not closed or planning-only.
4. Ask a concise clarification only if no defensible active stage can be inferred.

Current stage state:

```text
No active implementation stage. `v0.14-dev` is the latest completed development stage; `v1.0` has a formal plan, but implementation has not started.
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
| Historical stages | Matching `docs/项目计划书v0/项目计划v0.N/` and `docs/进度/v0.N-dev/` for traceability |
| Latest completed `v0.14-dev` | `docs/项目计划书v0/项目计划v0.14/00-v0.14版本计划书.md` plus `docs/进度/v0.14-dev/v0.14.11-dev.md` |
| Planned `v1.0` | `docs/项目计划书v1/项目计划v1.0/00-v1.0版本计划书.md`; do not treat the plan as implemented or start code without an explicit task |

If the exact plan file is unknown, list files in the plan directory and read the most relevant top-level plan first.

## Task Selection

When the user says to continue the next task:

1. Read the explicitly selected stage plan and the latest progress document.
2. Confirm that the selected plan is still open before choosing incomplete P0/P1 work.
3. If the latest plan is closed, treat a new request as maintenance unless the user explicitly opens a new stage.
4. Do not infer v1.0 implementation from the existence of its plan; require an explicit implementation task and follow the plan priority/order.
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
- Commit hash after committing, if updating the doc after commit is not required by the user.

Prefer concise tables over long prose.

## Current Baseline

The release baseline remains `v0.13.0`; migrations run through `000049`. `v0.14-dev` is the latest completed development
stage, with closure evidence in `docs/进度/v0.14-dev/v0.14.11-dev.md`; do not describe this as `v0.14.0 Final`.
Treat v0.1-v0.13 plans as historical unless a compatibility task explicitly targets them. `v1.0` is the next formally
planned major-series stage, but has no implementation completion evidence yet.
