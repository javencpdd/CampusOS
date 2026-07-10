# README Update Checklist

## 1. Source Priority

Use current executable state before narrative history:

1. `Makefile`, `docker-compose.yml`, `.env.example`, `scripts/`, package manifests, CI workflows.
2. Current code, migration, SDK, example, plugin, and Skill directories.
3. Latest `docs/进度/` record for completed scope.
4. Current architecture, API, help, and Skill documents.
5. Version plans for planned, deferred, and acceptance scope.

When sources conflict, document verified behavior and mark uncertainty instead of choosing the most optimistic source.

## 2. README Core Check

| Item | Rule |
| --- | --- |
| Summary | One short project description. |
| Current status | Current stage plus a compact implemented/limited summary. |
| Quick start | Minimal prerequisites and shortest working command sequence. |
| Addresses | Primary Web/Admin/API addresses only; operational tools belong in help docs. |
| Repository map | Only top-level ownership boundaries needed for orientation. |
| Commands | Keep the first-run or primary validation command; route the full catalog to help docs. |
| Documentation | Link `docs/README.md` prominently and keep only a small set of role/topic shortcuts. |
| License | Link the repository license. |

Default policy targets enforced by `audit_readme_structure.py`:

- No more than 180 lines.
- No more than 8 level-two sections.
- Must include current status, quick start, and documentation sections.
- Must link `docs/README.md`.
- Must not add detailed-reference headings such as full API lists, migration history, troubleshooting, plugin development tutorials, or configuration reference.

These are guardrails, not a target size. Prefer a shorter README when navigation remains clear.

## 3. Move-or-Link Check

Before removing a detail from README:

1. Identify its canonical owner using `documentation-routing.md`.
2. Search for an existing document with `rg` before creating a file.
3. Update or create the canonical document.
4. Ensure it is reachable from `docs/README.md` directly or through a category index.
5. Replace README detail with a concise link only when it is still first-visit information.
6. Preserve an old path with a relocation stub when progress/history documents link to it.

## 4. Wording Check

- Use “已完成”“基础闭环”“部分完成”“计划中”“未完成” precisely.
- Do not use “已支持” without code, migration, documentation, or a validation result.
- Keep local-only defaults explicit and separate from production recommendations.
- Never copy tokens, private keys, real credentials, or unredacted logs into README.
- Use current paths and `pnpm`, not stale flat `docs/help/` paths or `npm` commands.

## 5. Validation

Always run:

```bash
python3 skills/campusos-readme-update/scripts/audit_readme_structure.py --root .
python3 skills/campusos-readme-update/scripts/check_readme_links.py --root . README.md docs/README.md
git diff --check
```

For each moved/created document, add `--require-doc <path>` to the structure audit.

Run Go, Compose, migration, or frontend checks only when the changed claims depend on them. Record commands not run and the reason.
