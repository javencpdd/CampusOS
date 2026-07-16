---
name: campusos-readme-update
description: Maintain and reorganize CampusOS repository documentation with a concise root README and discoverable detailed docs. Use when asked to update, audit, trim, rewrite, synchronize, or split README.md; move secondary setup, plugin, Skill, API, architecture, troubleshooting, contribution, or version details into docs/help, docs/skills, docs/api, docs/architecture, plans, or progress docs; or verify that README and the docs portal stay aligned with the current repository.
---

# CampusOS README Update

## Outcome

Keep `README.md` as the repository front door, not the complete manual. Move secondary details to the correct `docs/` owner, make them reachable from `docs/README.md`, and keep claims aligned with executable repository state.

Default root:

```text
/home/jack/bbs/bbs01/CampusOS
```

Read `references/readme-update-checklist.md` before editing. Read `references/documentation-routing.md` when moving, creating, or consolidating documentation.

## Core README Contract

Keep only information needed before a reader enters the documentation portal:

1. Project name and one-sentence purpose.
2. Current development baseline, core implemented areas, and major limitations.
3. Minimal prerequisites and copy-paste quick start.
4. Primary Web/Admin/API addresses.
5. Small repository map and one essential validation command when useful.
6. A prominent `docs/README.md` entry plus a few high-value role/topic links.
7. License link.

Move detailed environment variables, credentials, pgAdmin/database instructions, migration history, API inventories, plugin/Skill tutorials, troubleshooting, CI/CD, contribution scripts, architecture internals, release history, and long command catalogs out of the root README.

## Workflow

### 1. Preflight

```bash
git status -sb
git branch --show-current
```

Do not overwrite unrelated worktree changes.

### 2. Establish Current Truth

Read the current entry points and inventory actual documentation categories:

```bash
sed -n '1,220p' README.md
sed -n '1,220p' docs/README.md
find docs -maxdepth 2 -type d | sort
find docs/help docs/skills docs/api docs/architecture -maxdepth 2 -type f 2>/dev/null | sort
find docs/进度 -maxdepth 2 -type f | sort -V | tail -n 20
find docs/项目计划v* -maxdepth 2 -type f | sort -V
```

Verify claims against source files only as needed:

```bash
sed -n '1,220p' Makefile
sed -n '1,220p' docker-compose.yml
find migrations -maxdepth 1 -type f | sort
find internal cmd web/src admin/src sdk examples skills -maxdepth 2 -type f 2>/dev/null | sort | sed -n '1,240p'
```

Prefer executable/configured state, then current progress records, then plans. Never promote planned or partial work to completed status.

### 3. Classify Before Editing

For each README block, choose exactly one action:

| Action | Use when |
| --- | --- |
| Keep | A new reader needs it to identify, start, or navigate the project. |
| Shorten and link | The summary is useful, but details belong in a maintained document. |
| Move | The content is operational, reference-oriented, historical, or role-specific. |
| Remove | It is obsolete, duplicated, unverifiable, or replaced by a canonical document. |

Do not delete useful details until their target document exists and is linked from the docs portal.

### 4. Route Details to Their Owner

Use `references/documentation-routing.md`. Important defaults:

- `docs/help/`: setup, operation, contribution, plugin administration, troubleshooting, and task guides.
- `docs/skills/`: Skill usage, maintenance, validation, and discovery.
- `docs/api/`: HTTP/Host API contracts, route groups, errors, permissions, and examples.
- `docs/architecture/`: boundaries, topology, data ownership, storage, and security design.
- `docs/进度/`: completed task evidence and validation results.
- `docs/项目计划v*/`: planned scope, sequencing, acceptance, and deferred work.

Prefer updating an existing canonical document over creating another overlapping guide. Preserve old paths with a short relocation document when historical links would otherwise break.

### 5. Edit in Safe Order

1. Create or update the target detailed document.
2. Add it to the nearest category index, such as `docs/skills/README.md`, when one exists.
3. Add or update its role/topic entry in `docs/README.md`.
4. Replace the README detail with a concise statement and link, or remove it when the docs portal already provides the needed route.
5. Synchronize current baseline and progress links only when the active stage actually advanced.

The structure audit compares the server startup version with `README.md`, `docs/README.md`, the docs-site home page, and the docs-site introduction. Update those entry surfaces together; historical compatibility filenames and versioned reference pages do not need renaming.

Avoid copying the same table or command catalog into README and docs. The detailed document is canonical; README is a summary and route.

### 6. Validate Documentation Integrity

Run the structural policy check and link checker:

```bash
python3 skills/campusos-readme-update/scripts/audit_readme_structure.py --root .
python3 skills/campusos-readme-update/scripts/check_readme_links.py --root . README.md docs/README.md
git diff --check
```

When creating or moving a detailed document, prove it is reachable from the docs portal:

```bash
python3 skills/campusos-readme-update/scripts/audit_readme_structure.py \
  --root . \
  --require-doc docs/path/to/document.md
```

Run repository validation when README claims commands or implemented capabilities changed:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
docker compose config -q
make migrate-status
(cd web && pnpm build)
(cd admin && pnpm build)
```

State any skipped validation and reason. Do not run expensive or stateful commands merely because a link label changed.

## Guardrails

- Do not turn README into a changelog, API reference, deployment manual, plugin tutorial, or version plan.
- Do not create `docs/` duplicates differentiated only by wording.
- Do not leave moved documents unreachable from `docs/README.md` or a reachable category index.
- Do not expose production secrets. Document local development defaults only in the relevant security/setup guide.
- Do not rewrite historical progress records to match current paths; preserve compatibility links instead.
- If the repository layout conflicts with this Skill, follow the live layout and update the routing reference in the same task.

## Output

Report:

- README content kept, shortened, moved, or removed.
- Canonical docs created or updated and their portal route.
- Source files used to verify claims.
- Structural, link, and repository validation results.
- Any planned or uncertain content intentionally excluded from README.
