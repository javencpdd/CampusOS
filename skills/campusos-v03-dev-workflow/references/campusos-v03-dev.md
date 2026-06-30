# CampusOS v0.3-dev Reference

## Task Priority

Use this order unless the user gives a more specific task:

1. Runtime interface review.
2. Wasm Runtime skeleton.
3. Wasm event dispatch.
4. Runtime timeout and error isolation.
5. Host API permission checks.
6. Plugin logs persisted to `plugin_logs`.
7. `examples/plugins/hello-wasm`.
8. SDK/CLI and admin UI follow-up tasks.

## Progress Document Numbering

Progress docs must live in:

```text
docs/进度/v0.3-dev/
```

Use:

```text
v0.3.0-dev.md
v0.3.1-dev.md
v0.3.2-dev.md
```

To find the next file:

```bash
find docs/进度/v0.3-dev -maxdepth 1 -type f -name 'v0.3.*-dev.md' | sort
```

Do not rename existing progress docs unless explicitly requested.

## Current Baseline

v0.3-dev-pre is complete. Treat these as done:

- Application-side BIGINT IDs for core objects, plugin records, and API keys.
- `schema_migrations` migration tracking.
- Migrations `000001` through `000005`.
- Default admin and default category seed.
- `.env.example`.
- `scripts/migrate.*`, `scripts/docker-up.*`, `scripts/dev.*`.
- Core OpenAPI baseline.
- JWT thread creation regression test.
- CI/CD workflows and help docs.

## Validation Matrix

| Change type | Required validation |
| --- | --- |
| Go backend | `go test ./...` |
| Plugin runtime | `go test ./...`; add runtime-specific tests where possible |
| Migration | `make migrate-up`; `make migrate-status` |
| Frontend | `cd web && pnpm build`; `cd admin && pnpm build` |
| Workflow | Python YAML parse; `git diff --check` |
| Docs only | `git diff --check` and inspect links/paths |

## Documentation Standard

Each `v0.3.X-dev.md` should record:

- Objective.
- Actual changes.
- Files changed.
- Validation commands and pass/fail result.
- Risks or follow-up tasks.
- Commit hash after committing, if updating the doc after commit is not required by the user.

Prefer concise tables over long prose.
