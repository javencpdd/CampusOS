#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${CAMPUSOS_DOCKER_DEV_ENV:-$ROOT_DIR/deploy/docker/.env.dev.example}"
COMPOSE_FILE="$ROOT_DIR/compose.dev.yml"

set_developer_identity() {
  if [[ -n "${CAMPUSOS_DEV_UID:-}" && -n "${CAMPUSOS_DEV_GID:-}" ]]; then
    return
  fi
  case "$(uname -s 2>/dev/null || true)" in
    MINGW*|MSYS*|CYGWIN*)
      export CAMPUSOS_DEV_UID="${CAMPUSOS_DEV_UID:-1000}"
      export CAMPUSOS_DEV_GID="${CAMPUSOS_DEV_GID:-1000}"
      ;;
    *)
      export CAMPUSOS_DEV_UID="${CAMPUSOS_DEV_UID:-$(id -u)}"
      export CAMPUSOS_DEV_GID="${CAMPUSOS_DEV_GID:-$(id -g)}"
      ;;
  esac
}

set_developer_identity

compose() {
  local args=(docker compose --env-file "$ENV_FILE")
  if [[ -n "${CAMPUSOS_DOCKER_PROJECT:-}" ]]; then
    args+=(--project-name "$CAMPUSOS_DOCKER_PROJECT")
  fi
  args+=(-f "$COMPOSE_FILE")
  "${args[@]}" "$@"
}

command="${1:-up}"
case "$command" in
  config)
    compose config --quiet
    echo "Docker development configuration is valid."
    ;;
  build)
    compose build api web admin docs
    ;;
  up)
    compose up -d --build --wait --wait-timeout "${CAMPUSOS_DOCKER_WAIT_TIMEOUT:-600}"
    compose ps
    ;;
  ps)
    compose ps
    ;;
  logs)
    shift || true
    compose logs -f --tail=200 "$@"
    ;;
  test)
    compose run --rm --no-deps \
      -e PLUGINS_DIR=/workspace/data/plugins \
      -e PLUGIN_DATA_DIR=/tmp/campusos-go-test/plugin_data \
      -e MODULE_DATA_DIR=/tmp/campusos-go-test/module_data \
      -e RESOURCE_DIR=/workspace/data/resources \
      api bash -c \
      'GOCACHE=/go-cache/build GOMODCACHE=/go-cache/modules GOFLAGS=-buildvcs=false go test ./... -count=1'
    ;;
  shell)
    compose exec api bash
    ;;
  down)
    compose down
    ;;
  reset)
    [[ "${2:-}" == "--confirm" ]] || {
      echo "reset deletes only the campusos-dev containers and named volumes; pass --confirm" >&2
      exit 2
    }
    compose down --volumes --remove-orphans
    ;;
  *)
    echo "Usage: $0 {config|build|up|ps|logs [service]|test|shell|down|reset --confirm}" >&2
    exit 2
    ;;
esac
