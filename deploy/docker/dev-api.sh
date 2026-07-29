#!/usr/bin/env bash
set -euo pipefail

drop_to_developer() {
  [[ "$(id -u)" == "0" ]] || return 0
  local uid="${CAMPUSOS_DEV_UID:-1000}"
  local gid="${CAMPUSOS_DEV_GID:-1000}"
  local home_dir="/tmp/campusos-dev-home"
  mkdir -p /go-cache/build /go-cache/modules /tmp/campusos-dev "$home_dir"
  chown -R "$uid:$gid" /go-cache/build /go-cache/modules /tmp/campusos-dev "$home_dir"
  export HOME="$home_dir"
  exec setpriv --reuid="$uid" --regid="$gid" --clear-groups "$0" "$@"
}

drop_to_developer "$@"

cd /workspace

wait_for_postgres() {
  local attempts="${CAMPUSOS_DATABASE_WAIT_ATTEMPTS:-60}"
  local delay="${CAMPUSOS_DATABASE_WAIT_DELAY:-2}"
  local attempt

  for ((attempt = 1; attempt <= attempts; attempt++)); do
    if PGPASSWORD="${DB_PASSWORD:-}" pg_isready \
      -h "${DB_HOST:-postgres}" \
      -p "${DB_PORT:-5432}" \
      -U "${DB_USER:-campusos}" \
      -d "${DB_NAME:-campusos}" >/dev/null 2>&1; then
      return 0
    fi
    echo "==> waiting for PostgreSQL (${attempt}/${attempts})"
    sleep "$delay"
  done

  echo "PostgreSQL did not become ready in time" >&2
  return 1
}

source_fingerprint() {
  {
    find cmd internal pkg sdk -type f -name '*.go' -print0
    find migrations modules -type f \( -name '*.sql' -o -name '*.yaml' -o -name '*.yml' \) -print0
    printf '%s\0' go.mod go.sum
  } | sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1
}

server_pid=''
stop_server() {
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" >/dev/null 2>&1; then
    kill -TERM "$server_pid"
    wait "$server_pid" || true
  fi
  server_pid=''
}

start_server() {
  /tmp/campusos-dev/server &
  server_pid="$!"
}

rebuild_server() {
  local candidate="/tmp/campusos-dev/server.new"
  echo "==> building CampusOS API"
  if ! go build -buildvcs=false -trimpath -o "$candidate" ./cmd/server; then
    echo "==> build failed; the last successful API process remains available" >&2
    return 1
  fi
  if ! PSQL_MODE=host ./scripts/migrate.sh up; then
    echo "==> migration failed; the last successful API process remains available" >&2
    return 1
  fi
  stop_server
  mv "$candidate" /tmp/campusos-dev/server
  start_server
  echo "==> CampusOS API restarted"
}

shutdown() {
  stop_server
  exit 0
}

trap 'stop_server' EXIT
trap 'shutdown' INT TERM

mkdir -p /tmp/campusos-dev data/logs
wait_for_postgres
rebuild_server

fingerprint="$(source_fingerprint)"
poll_interval="${CAMPUSOS_DEV_POLL_INTERVAL:-1}"

while sleep "$poll_interval"; do
  next_fingerprint="$(source_fingerprint)"
  if [[ "$next_fingerprint" != "$fingerprint" ]]; then
    if rebuild_server; then
      fingerprint="$next_fingerprint"
    fi
    continue
  fi

  if ! kill -0 "$server_pid" >/dev/null 2>&1; then
    echo "==> API process exited; restarting the last successful build" >&2
    wait "$server_pid" || true
    start_server
  fi
done
