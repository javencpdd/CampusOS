# CampusOS Docker Development Contract

> 更新时间：2026-08-03

## Configuration ownership

| File | Role |
| --- | --- |
| `deploy/docker/.env.dev.example` | Versioned template for the Docker development profile. |
| `deploy/docker/.env.dev.local` | Ignored, effective local development configuration; never commit secrets. |
| `.env.example` | Native/general environment template, not the effective Docker development file. |
| `deploy/docker/.env.example` | Aggregate production/single-host deployment template. |

Changing project environment values requires `docker-dev.* up` so Compose recreates affected containers. `docker restart`
does not re-read an env file.

## Command decision table

| Situation | Windows PowerShell | Linux/WSL2/Git Bash |
| --- | --- | --- |
| First configuration | `.\scripts\docker-dev.ps1 setup` | `./scripts/docker-dev.sh setup` |
| First build/start | `.\scripts\docker-dev.ps1 setup -Start` | `./scripts/docker-dev.sh setup --start` |
| Start/apply runtime config, no build | `.\scripts\docker-dev.ps1 up` | `./scripts/docker-dev.sh up` |
| Rebuild application images and start | `.\scripts\docker-dev.ps1 rebuild` | `./scripts/docker-dev.sh rebuild` |
| Status/logs | `ps`, `logs <service>` | `ps`, `logs <service>` |
| LAN diagnosis | `lan-check` | `lan-check` |
| Stop and keep data | `down` | `down` |

Do not run `down` before `up`/`rebuild`; it adds downtime without helping Compose update containers. Never add `-v` to
shutdown unless the user explicitly requests removal of development data and the exact volumes have been confirmed.

## Hot reload and build inputs

- API watches Go source, `go.mod`/`go.sum`, migrations, and module YAML, builds a candidate, migrates forward, and switches
  only after success.
- Web/Admin use Vite HMR; Docs uses VitePress reload.
- Package manifests/lockfiles, `Dockerfile.dev`, Compose build sections, and `deploy/docker/dev-*` entrypoints require
  `rebuild`.
- `up` must use existing images without reaching Docker Hub; first setup/rebuild may need registries and dependency sources.

## Network and proxy boundary

- Default UI and all sensitive services are loopback-only.
- LAN opt-in may bind only 3000, 3001, and 3002 to `0.0.0.0`; API/data remain loopback-only.
- Windows uses `. .\sh\proxy.ps1 on -SystemProxy` when Docker Desktop must read the current-user System proxy, followed by
  `docker desktop restart`. Linux/WSL/Git Bash uses `source sh/proxy.sh on`, noting that it also manages Git/SSH.
- A project `EMAIL_PROVIDER=smtp` setting is unrelated to Docker Hub proxy reachability.

## Validation evidence

Static checks prove launcher/Compose contracts; runtime checks prove the current machine. Record which one was run. A
healthy result on Windows Docker Desktop `amd64` does not certify Linux `arm64` or production HA behavior.
