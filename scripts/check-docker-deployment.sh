#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

required=(
  .dockerignore
  compose.deploy.yml
  deploy/docker/Dockerfile
  deploy/docker/entrypoint.sh
  deploy/docker/nginx-spa.conf
  deploy/docker/nginx-docs.conf
  deploy/docker/.env.example
  scripts/docker-deploy.sh
)

for path in "${required[@]}"; do
  if [[ ! -f "$path" ]]; then
    echo "missing Docker deployment file: $path" >&2
    exit 1
  fi
done

if rg -n '/var/run/docker\.sock|privileged:[[:space:]]*true' compose.deploy.yml deploy/docker >/dev/null; then
  echo "Docker deployment must not mount the daemon socket or use privileged containers." >&2
  exit 1
fi

rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT
docker compose --env-file deploy/docker/.env.example -f compose.deploy.yml config >"$rendered"

for service in postgres redis nats api web admin docs; do
  if ! grep -q "^  ${service}:" "$rendered"; then
    echo "rendered Docker deployment is missing service: $service" >&2
    exit 1
  fi
done

grep -q 'target: api' "$rendered"
grep -q 'target: web' "$rendered"
grep -q 'target: admin' "$rendered"
grep -q 'target: docs' "$rendered"
grep -q 'source: campusos-data' "$rendered"
grep -q 'target: /app/data' "$rendered"

bash -n deploy/docker/entrypoint.sh scripts/docker-deploy.sh
echo "Docker deployment contract passed."

