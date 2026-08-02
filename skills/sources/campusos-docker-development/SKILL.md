---
name: campusos-docker-development
description: Diagnose, change, and verify the cross-platform CampusOS Docker development workflow on Windows PowerShell and Linux/WSL2/Git Bash. Use for first setup, `deploy/docker/.env.dev.local`, `up` versus `rebuild`, source hot reload, Docker Hub proxy failures, LAN exposure of ports 3000-3002, container/platform logs, Compose health, shutdown, or changes to `scripts/docker-dev.*`, `compose.dev.yml`, and `deploy/docker/`.
---

# CampusOS Docker Development

> 更新时间：2026-08-03

## Outcome

Keep one Windows/Linux development contract without exposing sensitive services, rebuilding unnecessarily, losing data, or
mistaking Docker/registry failures for application bugs.

Read [Docker development contract](references/docker-development-contract.md) before changing launchers, Compose, proxy,
LAN, reload, log, or configuration behavior.

## Workflow

1. Check `git status --short` and preserve unrelated changes. Check `.gitignore` before treating
   `deploy/docker/.env.dev.local`, `.campusos/`, logs, caches, or volumes as missing source files.
2. Establish runtime facts:
   - Read `README.md`, `compose.dev.yml`, `scripts/docker-dev.ps1`, `scripts/docker-dev.sh`, and relevant
     `deploy/docker/dev-*` entrypoints.
   - Inspect the effective Compose configuration without printing secrets.
   - When a stack is running, inspect `docker-dev.* ps`, service logs, health endpoints, and the exact failed request.
3. Classify the task before choosing a command:
   - Ordinary Go/Vue/VitePress source: let hot reload handle it.
   - `.env.dev.local`, ports, runtime environment, or mounts: run `up`.
   - Dockerfile, package/lockfile, Compose build inputs, or development entrypoints: run `rebuild`.
   - First machine with no application images: run `setup`, edit `.env.dev.local`, then `setup -Start`/`--start`.
4. Keep API, PostgreSQL, Redis, and NATS loopback-only. Expose only Web/Admin/Docs to the LAN through the explicit
   `CAMPUSOS_DEV_ALLOW_LAN` and per-UI bind contract, then run `lan-check`.
5. Treat `failed to fetch oauth token` as a Docker Hub/DNS/proxy path failure. Verify the local proxy and Docker Desktop
   System proxy separately from project SMTP or application environment. Restart Docker Desktop only after its own
   proxy/DNS/engine settings change.
6. Make the smallest cross-platform change. Update PowerShell and Bash launchers together when their public command contract
   changes. Preserve LF/CRLF rules and do not put Windows paths inside container configuration.
7. Validate the affected contract and runtime. Prefer existing repository checks over one-off scripts.
8. Synchronize the root quick-start summary, canonical Help guide, `docs-site/deployment/docker-development.md`, and current
   progress evidence when developer-visible behavior changes.

## Required validation

Run the applicable subset:

```bash
make docker-dev-test
make docker-deploy-check
python scripts/test_lan_access.py
python scripts/check-line-endings.py --include-untracked
python scripts/check-doc-links.py
git diff --check
```

When Docker is available, also run `docker-dev.* config`, `ps`, the relevant health/HTTP checks, and a real `up` or
`rebuild` only when the task authorizes that lifecycle action. Never delete volumes or stop unrelated containers as a
diagnostic shortcut.

## Report

State the failure layer, platform, exact command contract, whether images were rebuilt, whether Docker Desktop itself was
restarted, data/port exposure impact, runtime evidence, and skipped checks.
