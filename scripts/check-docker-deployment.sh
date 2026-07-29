#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

required=(
  .gitattributes
  .dockerignore
  compose.dev.yml
  compose.deploy.yml
  deploy/docker/Dockerfile.dev
  deploy/docker/Dockerfile
  deploy/docker/dev-api.sh
  deploy/docker/dev-frontend.sh
  deploy/docker/entrypoint.sh
  deploy/docker/nginx-spa.conf
  deploy/docker/nginx-docs.conf
  deploy/docker/restore-container.sh
  deploy/docker/.env.dev.example
  deploy/docker/.env.example
  deploy/docker/components/compose.infra.yml
  deploy/docker/components/compose.api.yml
  deploy/docker/components/compose.web.yml
  deploy/docker/components/compose.admin.yml
  deploy/docker/components/compose.docs.yml
  deploy/docker/components/.env.infra.example
  deploy/docker/components/.env.api.example
  deploy/docker/components/.env.web.example
  deploy/docker/components/.env.admin.example
  deploy/docker/components/.env.docs.example
  scripts/docker-component.sh
  scripts/docker-component.ps1
  scripts/docker-dev.sh
  scripts/docker-dev.ps1
  scripts/docker-deploy.sh
  scripts/docker-deploy.ps1
)

for path in "${required[@]}"; do
  if [[ ! -f "$path" ]]; then
    echo "missing Docker deployment file: $path" >&2
    exit 1
  fi
done

if rg -n '/var/run/docker\.sock|privileged:[[:space:]]*true' compose.dev.yml compose.deploy.yml deploy/docker >/dev/null; then
  echo "Docker deployment must not mount the daemon socket or use privileged containers." >&2
  exit 1
fi

database_major="$(sed -nE 's/^[[:space:]]*image:[[:space:]]*postgres:([0-9]+)-.*/\1/p' deploy/docker/components/compose.infra.yml)"
backup_client_major="$(sed -nE 's/^ARG POSTGRES_CLIENT_MAJOR=([0-9]+)$/\1/p' deploy/docker/Dockerfile)"
if [[ -z "$database_major" || -z "$backup_client_major" || "$database_major" != "$backup_client_major" ]]; then
  echo "API pg_dump major version must match the PostgreSQL container major version." >&2
  exit 1
fi

deploy_rendered="$(mktemp)"
dev_rendered="$(mktemp)"
component_rendered="$(mktemp)"
trap 'rm -f "$deploy_rendered" "$dev_rendered" "$component_rendered"' EXIT
docker compose --env-file deploy/docker/.env.example -f compose.deploy.yml --profile maintenance config >"$deploy_rendered"
docker compose --env-file deploy/docker/.env.dev.example -f compose.dev.yml config >"$dev_rendered"

for service in postgres redis nats api web admin docs; do
  if ! grep -q "^  ${service}:" "$deploy_rendered"; then
    echo "rendered Docker deployment is missing service: $service" >&2
    exit 1
  fi
  if ! grep -q "^  ${service}:" "$dev_rendered"; then
    echo "rendered Docker development stack is missing service: $service" >&2
    exit 1
  fi
done

grep -q '^  maintenance:' "$deploy_rendered"
grep -q 'target: api' "$deploy_rendered"
grep -q 'target: web' "$deploy_rendered"
grep -q 'target: admin' "$deploy_rendered"
grep -q 'target: docs' "$deploy_rendered"
grep -q 'source: campusos-data' "$deploy_rendered"
grep -q 'target: /app/data' "$deploy_rendered"
grep -q '/app/data/.backup-staging' scripts/docker-deploy.sh
grep -q '/app/data/.backup-staging' scripts/docker-deploy.ps1
grep -q 'source: go-build-cache' "$dev_rendered"
grep -q 'source: web-node-modules' "$dev_rendered"
grep -q 'source: admin-node-modules' "$dev_rendered"
grep -q 'source: docs-node-modules' "$dev_rendered"
grep -q 'target: web-dev' "$dev_rendered"
grep -q 'target: admin-dev' "$dev_rendered"
grep -q 'target: docs-dev' "$dev_rendered"
grep -q 'CAMPUSOS_DEV_UID' "$dev_rendered"
grep -q 'setpriv --reuid' deploy/docker/dev-api.sh
grep -q 'setpriv --reuid' deploy/docker/dev-frontend.sh
grep -q 'COREPACK_HOME=/opt/corepack' deploy/docker/Dockerfile.dev
grep -q 'COREPACK_DEFAULT_TO_LATEST=0' deploy/docker/Dockerfile.dev
grep -q 'corepack prepare pnpm@8.15.9 --activate' deploy/docker/Dockerfile.dev

for component in infra api web admin docs; do
  docker compose \
    --env-file deploy/docker/.env.example \
    --env-file "deploy/docker/components/.env.$component.example" \
    -f "deploy/docker/components/compose.$component.yml" \
    config >"$component_rendered"
  if [[ "$component" == "infra" ]]; then
    for service in postgres redis nats; do
      grep -q "^  ${service}:" "$component_rendered"
    done
  else
    grep -q "^  ${component}:" "$component_rendered"
    grep -q "target: ${component}" "$component_rendered"
  fi
done

grep -q 'name: campusos_backend' "$deploy_rendered"
grep -q 'name: campusos_edge' "$deploy_rendered"
grep -q 'name: campusos_campusos-data' "$deploy_rendered"
grep -q 'docker network create --internal' scripts/docker-deploy.sh
grep -q 'docker network create --internal' scripts/docker-component.sh
grep -q -- '--internal' scripts/docker-deploy.ps1
grep -q -- '--internal' scripts/docker-component.ps1

bash -n \
  deploy/docker/dev-api.sh \
  deploy/docker/dev-frontend.sh \
  deploy/docker/entrypoint.sh \
  deploy/docker/restore-container.sh \
  scripts/docker-component.sh \
  scripts/docker-dev.sh \
  scripts/docker-deploy.sh
echo "Docker deployment and cross-platform development contracts passed."
