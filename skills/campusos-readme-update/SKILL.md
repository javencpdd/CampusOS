---
name: campusos-readme-update
description: CampusOS README maintenance workflow for /home/jack/bbs/bbs01/CampusOS. Use when the user asks to update, refresh, audit, rewrite, synchronize, or review README.md based on current project state, version plans, progress docs, help docs, migrations, Docker setup, CI/CD, frontend/admin/backend capabilities, or recent commits.
---

# CampusOS README Update

## Core Rule

Update `README.md` so it reflects the current repository reality. Do not describe planned, partial, or unverified features as complete.

Default repository path:

```text
/home/jack/bbs/bbs01/CampusOS
```

Before editing, read `references/readme-update-checklist.md`.

## Workflow

1. Run preflight:

```bash
git status -sb
git branch --show-current
```

2. Read the current README and relevant source-of-truth files:

```bash
sed -n '1,260p' README.md
find docs/项目计划v* -maxdepth 2 -type f | sort -V
find docs/进度 -maxdepth 2 -type f | sort -V | tail -n 20
find docs/help -maxdepth 1 -type f | sort
find migrations -maxdepth 1 -type f | sort
```

3. Inspect code/config only where README content depends on it:

```bash
sed -n '1,180p' docker-compose.yml
sed -n '1,220p' Makefile
find .github/workflows -maxdepth 1 -type f | sort
find web/src admin/src internal cmd sdk examples skills -maxdepth 2 -type f 2>/dev/null | sort | sed -n '1,220p'
```

4. Update README conservatively:

- Keep it useful for a new developer.
- Prefer exact commands, paths, ports, accounts, and current limitations.
- Separate completed features from planned work.
- Preserve important local caveats, such as Docker PostgreSQL using `POSTGRES_PORT=5433` when host `5432` is unavailable.
- Update migrations, service addresses, documentation links, skill links, and version-stage summaries together.
- Do not remove user-authored sections unless they are obsolete or replaced with equivalent current information.

5. Validate:

```bash
git diff --check -- README.md
python3 skills/campusos-readme-update/scripts/check_readme_links.py README.md
```

Run extra validation when README claims commands work:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
docker compose config -q
make migrate-status
```

6. If the user asks to commit, commit only README and directly related docs/scripts.

## Documentation

If the README update changes documented setup rules, also update the matching help document under `docs/help/`.

Examples:

| README topic | Help doc to consider |
| --- | --- |
| Database, PostgreSQL, migrations, pgAdmin | `docs/help/数据库管理指南.md`, `docs/help/make-migrate-up教程.md` |
| CI/CD | `docs/help/GitHub Actions CI-CD使用说明.md` |
| PR process | `docs/help/PR模板与CI自测说明.md` |
| Skill usage | `docs/help/CampusOS-dev流程Skill使用说明.md` or the specific skill help doc |
| Plugins | `docs/help/插件保存位置与当前插件作用汇总.md`, `docs/help/标准插件包导入导出要求.md` |

## Output

Summarize:

- What README sections changed.
- Which source documents/code paths were used.
- Validation commands and results.
- Any content intentionally left as planned/partial.
