# CampusOS Versioned Development Reference

## Stage Detection

Use this order:

1. Explicit user request, for example `v0.4-dev`, `v0.5-dev`, or `v0.6-dev`.
2. Existing progress directory under `docs/进度/`, sorted by version.
3. Existing project plan directory under `docs/项目计划v*`, sorted by plan version.
4. Ask a concise clarification only if no defensible stage can be inferred.

Stage names use:

```text
v0.3-dev
v0.4-dev
v0.5-dev
```

Progress documents use:

```text
docs/进度/<stage>/v0.X.Y-dev.md
```

Examples:

```text
docs/进度/v0.4-dev/v0.4.2-dev.md
docs/进度/v0.5-dev/v0.5.0-dev.md
```

## Plan Mapping

| Implementation stage | Preferred plan path |
| --- | --- |
| `v0.3-dev` | `docs/项目计划v3/02-v0.3-dev计划书.md` |
| `v0.4-dev` | `docs/项目计划v4/00-v4版本计划书.md` |
| `v0.5-dev` | `docs/项目计划v5/` if present |
| Future stages | Latest matching `docs/项目计划vN/` or user-provided plan |

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
find docs/进度/v0.4-dev -maxdepth 1 -type f -name 'v0.4.*-dev.md' | sort -V
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
| Migration | `make migrate-up`; `make migrate-status` |
| Frontend | `cd web && pnpm build`; `cd admin && pnpm build` when relevant |
| Workflow | Python YAML parse; `git diff --check` |
| Docs only | `git diff --check` and inspect links/paths |
| Skill updates | `quick_validate.py <skill-dir>`; run relevant bundled scripts |

Always run:

```bash
git diff --check
GOCACHE="${GOCACHE:-/tmp/campusos-go-cache}" go test ./...
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

## Current Baselines

Treat these as already complete unless the current plan says otherwise:

- v0.3-dev pre-stabilization and plugin runtime foundation.
- v0.3-dev Wasm runtime, Host API permissions, plugin logs, SDK/CLI baseline.
- v0.4-dev preflight items recorded in `docs/进度/v0.4-dev/v0.4.0-dev.md`.
- v0.4-dev plugin configuration schema recorded in `docs/进度/v0.4-dev/v0.4.1-dev.md`.

For v0.4-dev, the active plan currently prioritizes plugin delivery, AI Gateway, MCP, personal homepage/style plugins, Webhook, IM/robot integration, admin observability, and release/deployment stabilization.
