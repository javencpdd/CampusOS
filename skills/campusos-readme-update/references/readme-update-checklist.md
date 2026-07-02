# README Update Checklist

Use this checklist before editing `README.md`.

## Source Priority

Prefer current executable/configured state over older narrative docs:

1. `docker-compose.yml`, `.env.example`, `Makefile`, `scripts/`
2. `go.mod`, `package.json` files, `.github/workflows/`
3. `migrations/`
4. Current code directories: `internal/`, `cmd/`, `web/`, `admin/`, `sdk/`, `examples/`, `skills/`
5. Latest progress docs under `docs/进度/`
6. Project plans under `docs/项目计划v*/`
7. Help docs under `docs/help/`

If sources conflict, state the current verified behavior and avoid claiming uncertain features.

## README Sections To Check

| Section | Update Rules |
| --- | --- |
| Project summary | State current implemented capability and current active stage. Do not overstate planned v5 work. |
| Current status | Reflect backend, web, admin, plugins, AI Gateway, personal space/style, MCP/Webhook/IM status accurately. |
| Version recap | Include v0.3/v0.4 outcomes and note incomplete items when relevant. |
| Architecture | Keep ports and service names aligned with Docker Compose and `.env.example`. |
| Environment | Include Go/Node/pnpm/Docker/PostgreSQL client requirements. |
| Quick start | Keep commands copy-pasteable. Mention `POSTGRES_PORT=5433` if host `5432` is occupied. |
| Database | List current migrations and link database docs. Make clear Docker PostgreSQL is the recommended dev DB. |
| Services | Verify `web:3000`, `admin:3001`, API, Host API, pgAdmin, PostgreSQL, Redis, NATS ports. |
| Common commands | Match Makefile targets. |
| Plugins/skills | Include current project skills and plugin examples only if they exist. |
| Known limitations | Keep Windows/native deployment, MCP/Webhook/IM, plugin marketplace, and AI risks honest. |
| Docs index | Link active plan/progress/help docs that exist. |

## Wording Rules

- Use "已完成", "部分完成", "未完成", "计划中" precisely.
- Avoid phrases like "已支持" unless code or docs prove it.
- Prefer "当前推荐" for environment choices that may change.
- Mention local-only state explicitly when relevant, for example `.env` is ignored by git.
- Do not include secrets beyond documented dev defaults.

## Validation Checklist

Run at minimum:

```bash
git diff --check -- README.md
python3 skills/campusos-readme-update/scripts/check_readme_links.py README.md
```

Run when relevant:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
docker compose config -q
make migrate-status
cd web && npm run build
cd admin && npm run build
```

If a command is not run, state why in the final answer.
