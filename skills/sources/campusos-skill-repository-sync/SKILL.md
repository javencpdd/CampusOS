---
name: campusos-skill-repository-sync
description: Maintain CampusOS repository-local Codex Skills, their portable `.agents/skills` discovery bridges, source folders, and user guides. Use when adding, renaming, moving, auditing, validating, or publishing project Skills; when making a cloned repository expose Skills without a user-level installation; or when Skill source, bridge metadata, and documentation may have drifted.
---

# CampusOS Skill Repository Sync

> 更新时间：2026-08-03

Keep the Git-tracked Skill source, Codex discovery entry, and developer guide synchronized without copying anything into a user's system directory.

## Repository contract

- Treat `skills/sources/<skill-name>/` as the canonical source, including `SKILL.md`, `agents/`, `scripts/`, and `references/`.
- Treat `skills/guides/` as human-facing usage and maintenance documentation, not as discoverable Skill source.
- Treat `.agents/skills/<skill-name>/` as a generated, Git-tracked discovery bridge. Codex scans this official repository-level location after clone.
- Do not use repository symlinks as the default bridge: Git symlink checkout is not reliable on every Windows installation.
- Do not install or overwrite `$HOME/.agents/skills`, `$HOME/.codex/skills`, or another user-level directory unless the user explicitly requests a separate personal installation.

Read [repository-discovery-contract.md](references/repository-discovery-contract.md) before changing layout or discovery behavior.

## Workflow

1. Resolve the repository root and inspect `.gitignore` before treating a missing bridge or source as deleted.
2. Add or update the canonical Skill under `skills/sources/<skill-name>/`. Keep frontmatter limited to `name` and `description`, and include an update date in the body.
3. Add or update its usage guide under `skills/guides/` and link it from `skills/guides/README.md`.
4. Generate portable discovery bridges:

   ```text
   python skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --write
   ```

5. Verify that no source/bridge drift remains:

   ```text
   python skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --check
   ```

6. Run the official `skill-creator` `quick_validate.py` against every directory in `skills/sources/` and `.agents/skills/`.
7. Update current documentation, `docs-site/contributing/workflow.md`, and the active `docs/进度/v0.13-dev/` evidence. Do not rewrite historical progress records merely because their old commands reflect the layout at that time.
8. Run repository link checks and `git diff --check`. Do not commit or push unless the user explicitly requests it.

## Bridge rules

The generated bridge must retain the canonical frontmatter so implicit and explicit activation behave consistently. Its body must tell the agent to read the canonical `SKILL.md` completely and resolve bundled resources from the canonical source directory.

Keep bridges small. Do not duplicate full instructions, references, or scripts under `.agents/skills`; duplication would create two independently editable sources of truth.

If a bridge is stale, regenerate it. If the checker reports an unexpected stale bridge directory, inspect it before removal and never delete it automatically.

## Completion report

Report:

- canonical Skills added, moved, or updated;
- discovery bridges generated and checked;
- usage guides and current docs updated;
- canonical and bridge validation results;
- whether a Codex restart is needed for an already-open session to refresh the visible Skill list;
- explicitly that no user-level Skill installation was performed, unless the user requested one.
