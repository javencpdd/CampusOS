# Repository Skill discovery contract

> 更新时间：2026-08-03

## Current official behavior

The current Codex manual states that repository Skills are scanned from `.agents/skills` in the current working directory and each parent directory up to the repository root. A root-level `.agents/skills` therefore applies to Codex launched anywhere inside the CampusOS repository.

Codex supports symlinked Skill folders, but CampusOS uses checked-in bridge folders instead. On Windows, a Git clone may materialize a symbolic link as an ordinary text file unless developer mode, permissions, and `core.symlinks` are aligned. A small bridge `SKILL.md` is portable across Windows and Linux clones.

Official source consulted on 2026-08-03:

- <https://learn.chatgpt.com/docs/build-skills.md>

## CampusOS layout

```text
.agents/skills/<skill-name>/       # Codex discovery bridge, generated and committed
skills/sources/<skill-name>/       # canonical Skill source
skills/guides/                     # human-facing usage documentation
```

The bridge is intentionally not a second implementation. It carries matching frontmatter and directs the agent to load the canonical source before acting.

## Clone behavior

After cloning the repository:

1. Launch Codex in the repository root or any subdirectory.
2. Invoke a Skill with `$campusos-project-onboarding` or another listed name.
3. If an already-open Codex session predates the clone or Skill update and the list does not refresh automatically, restart Codex once.

No copy into `~/.agents/skills`, `~/.codex/skills`, `%USERPROFILE%\.agents\skills`, or `%USERPROFILE%\.codex\skills` is required for repository-local use.
