---
name: campusos-project-onboarding
description: Quickly onboard an agent to the CampusOS repository, current version progress, implemented modules, active docs, migrations, validation commands, and known development boundaries. Use when a user asks an agent to quickly understand CampusOS, continue project work after context loss, review current progress, start a new version-stage task, or decide what to read before changing code.
---

# CampusOS Project Onboarding

## Purpose

Use this skill before doing CampusOS project work when the agent needs a fast, accurate picture of the repository state.

The snapshot is useful, but it is not a substitute for checking live files. Treat `references/current-project-status.md` as an orientation document and verify details against code, migrations, README, and progress docs before editing.

## Onboarding Workflow

1. Confirm the repository root is `/home/jack/bbs/bbs01/CampusOS`.
2. Read `references/current-project-status.md`.
3. Run `scripts/context_snapshot.sh` when current git status, migrations, docs, or skill inventory matters.
4. Read only the task-relevant source files after the snapshot:
   - Version planning: `docs/项目计划v5/00-v5版本计划书.md`, `docs/项目计划v4/02-v4实现状态与后续规划总结.md`
   - Current progress: `docs/进度/v0.5-dev/`
   - Runtime setup: `README.md`, `.env.example`, `Makefile`, `docker-compose.yml`, `scripts/`
   - Backend implementation: `internal/`, `cmd/server/`, `pkg/`
   - Frontend implementation: `web/src/`, `admin/src/`
   - Plugins and SDK: `internal/plugin/`, `examples/plugins/`, `sdk/go/`, `cmd/campusosctl/`
5. Before editing, check `git status --short` and do not overwrite unrelated user changes.
6. After edits, validate based on impact:
   - Go/backend: `GOCACHE=/tmp/campusos-go-cache go test ./... -count=1`
   - Migrations: `make migrate-up && make migrate-status`
   - User frontend: `cd web && pnpm build`
   - Admin frontend: `cd admin && pnpm build`
   - Shell scripts: `bash -n <script>`
   - Markdown links when relevant: use the relevant local checker if one exists.

## Output Pattern

When using this skill to brief the user or another agent, summarize:

- Current completed stage and next recommended stage.
- Key implemented modules.
- Current run and validation commands.
- Boundaries that must not be overstated.
- Files or directories the agent should read next for the requested task.

Keep the summary concise and clearly separate verified current state from planned work.
