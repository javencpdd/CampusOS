---
name: campusos-project-onboarding
description: Quickly onboard an agent to the CampusOS repository, current version progress, implemented modules, active docs, migrations, validation commands, and known development boundaries. Use when a user asks an agent to quickly understand CampusOS, continue project work after context loss, review current progress, start a new version-stage task, or decide what to read before changing code.
---

# CampusOS Project Onboarding

> 更新时间：2026-09-01

## Purpose

Use this skill before doing CampusOS project work when the agent needs a fast, accurate picture of the repository state.

The snapshot is useful, but it is not a substitute for checking live files. Treat `references/current-project-status.md` as an orientation document and verify details against code, migrations, README, and progress docs before editing.

## Onboarding Workflow

1. Confirm the repository root from the active workspace or derive it from this Skill directory; support Windows and
   Linux paths and do not hardcode a user home directory.
2. Read `references/current-project-status.md`.
3. Run `scripts/context_snapshot.sh` when current git status, migrations, docs, or skill inventory matters.
4. Read only the task-relevant source files after the snapshot:
   - Version status: `docs/计划书总结/README.md`, `docs/计划书总结/01-当前项目规划与后续路线.md`
   - Next formal plan: `docs/项目计划书v1/项目计划v1.0/00-v1.0版本计划书.md` (planned, implementation not started)
   - Latest completed implementation plan: `docs/项目计划书v0/项目计划v0.14/00-v0.14版本计划书.md`
   - Release baseline: `docs/项目计划书v0/项目计划v0.13/02-v0.13最终专业审计与后续路线.md`
   - Current progress: the latest numerically versioned files in the active `docs/进度/v0.X-dev/` directory
   - Documentation status: `docs/help/README.md`, `docs/README.md`
   - Runtime setup: `README.md`, `.env.example`, `Makefile`, `compose.dev.yml`, `compose.deploy.yml`, `scripts/`
   - Backend implementation: `modules/`, `internal/modules/`, `internal/platform/`, `internal/plugin/`, `cmd/`, `pkg/`
   - Frontend implementation: `web/src/`, `admin/src/`, `docs-site/`
   - Plugins, resources and SDK: `data/plugins/`, `data/resources/`, `sdk/`, `examples/plugins/`, `cmd/campusosctl/`
5. Before editing, check `git status --short` and do not overwrite unrelated user changes.
6. Select a task-specific Skill when it applies:
   - Ordinary task without commit: `campusos-dev-nocommit`
   - Explicit local commit: `campusos-dev-workflow`
   - Docker development: `campusos-docker-development`
   - Web/Admin regression: `campusos-webui-regression`
   - Migration/data architecture: `campusos-data-architecture-sync`
   - README/document routing: `campusos-readme-update`
7. After edits, validate based on impact:
   - Go/backend: `GOCACHE=/tmp/campusos-go-cache go test ./... -count=1`
   - Migrations/data: `make migrate-status`, `make database-check`, and the relevant migration drill
   - User frontend: `cd web && pnpm lint && pnpm format:check && pnpm build`
   - Admin frontend: `cd admin && pnpm test:component && pnpm build`
   - Docs frontend: `cd docs-site && pnpm build`
   - Documentation: `make readme-check && make docs-links`
   - Architecture: `make architecture-check`
   - Docker development: `make docker-dev-test` and `make docker-deploy-check`
   - Cross-platform text: `python scripts/check-line-endings.py --include-untracked`
   - Shell scripts: `bash -n <script>`
   - Release/high-risk changes: run the applicable restore and browser release gates.

## Output Pattern

When using this skill to brief the user or another agent, summarize:

- Current completed stage and next recommended stage.
- Key implemented modules.
- Native and Docker run/validation commands.
- Core Module, Built-in Feature, External Plugin, and Resource Package boundaries.
- Boundaries that must not be overstated.
- Files or directories the agent should read next for the requested task.

Keep the summary concise and clearly separate verified current state from planned work.
