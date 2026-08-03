# CampusOS Documentation Routing

Use this reference to select one canonical owner for content removed from `README.md`.

## Routing Table

| Content | Canonical location | README treatment |
| --- | --- | --- |
| Project purpose and current baseline | `README.md` | Keep concise. |
| Minimal first run and primary addresses | `README.md` | Keep concise and link the full guide. |
| Environment variables, ports, local accounts, pgAdmin, Docker, migrations | `docs/help/系统设计相关/` | Keep only prerequisites and shortest start command. |
| Contribution scripts, PR, CI/CD, GitHub workflow | `docs/help/github使用相关/` or development guide | Keep at most one contribution-guide link. |
| Plugin installation, lifecycle, package rules, configuration, style packs | `docs/help/插件相关/` | Keep one plugin developer/admin route. |
| Skill invocation, structure, validation, synchronization | `skills/guides/` | Link the Skills index from `docs/README.md`; do not list every Skill in root README. |
| Current public developer/user workflow | `docs-site/` plus a canonical Help/API owner | Keep public docs concise and link deeper repository evidence instead of duplicating it. |
| HTTP API, Host API, errors, auth, permissions, request/response examples | `docs/api/` | Keep one API index link. |
| Module boundaries, topology, database/file ownership, security design | `docs/architecture/` | Keep one architecture overview link. |
| Completed task evidence and exact verification output | `docs/进度/<stage>/` | Root README links only the latest relevant progress record when useful. |
| Planned scope, priorities, deferred work, acceptance | `docs/项目计划v*/` | Keep only current plan/next-stage link. |
| Cross-version evolution, plan validity, current candidate roadmap | `docs/计划书总结/` | Link the summary, not a long list of historical plans. |
| Release notes and upgrade notes | `docs/releases/` | Do not duplicate a release history in README. |
| Unscheduled ideas and raw samples | `docs/Todo/` | Do not present as current product capability. |

## Category Index Rules

`docs/README.md` is the documentation portal. A category may also provide its own `README.md`, for example `skills/guides/README.md`.

When adding a document:

1. Link it from its category index when the category has multiple documents.
2. Link the category index from `docs/README.md` by reader role or documentation type.
3. Link directly from root README only when the document is essential before or immediately after first run.

## Existing-Path Compatibility

Some historical progress documents point to `docs/help/skills相关/` or `docs/skills/`. New Skill documentation belongs in `skills/guides/`. When migrating an existing Skill guide, leave a short Markdown relocation file at the old path instead of rewriting historical progress records.

Do not create duplicate full documents at old and new paths. The relocation file must name and link the canonical document.

Raw files named `计划思路`, prompt transcripts, and planning discussion exports are inputs, not current plans. Preserve them
for traceability, add an explicit non-authoritative status when needed, and record their replacement in
`docs/计划书总结/README.md`.
