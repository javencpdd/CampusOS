#!/usr/bin/env bash
set -euo pipefail

drop_to_developer() {
  [[ "$(id -u)" == "0" ]] || return 0
  local uid="${CAMPUSOS_DEV_UID:-1000}"
  local gid="${CAMPUSOS_DEV_GID:-1000}"
  local home_dir="/tmp/campusos-dev-home"
  mkdir -p node_modules "$home_dir"
  chown -R "$uid:$gid" node_modules "$home_dir"
  if [[ -d .vitepress ]]; then
    mkdir -p .vitepress/cache .vitepress/.temp
    chown -R "$uid:$gid" .vitepress/cache .vitepress/.temp
  fi
  export HOME="$home_dir"
  exec setpriv --reuid="$uid" --regid="$gid" --clear-groups "$0" "$@"
}

drop_to_developer "$@"

seed_dir="${CAMPUSOS_FRONTEND_SEED:?CAMPUSOS_FRONTEND_SEED is required}"
if [[ ! -f "$seed_dir/.campusos-lock-sha" || ! -f pnpm-lock.yaml ]]; then
  echo "frontend dependency seed or source lockfile is missing" >&2
  exit 1
fi

expected_hash="$(cat "$seed_dir/.campusos-lock-sha")"
source_hash="$(sha256sum pnpm-lock.yaml | cut -d' ' -f1)"
if [[ "$source_hash" != "$expected_hash" ]]; then
  echo "pnpm-lock.yaml changed after the image was built; rerun Docker Compose with --build" >&2
  exit 1
fi

installed_hash=''
if [[ -f node_modules/.campusos-lock-sha ]]; then
  installed_hash="$(cat node_modules/.campusos-lock-sha)"
fi
if [[ "$installed_hash" != "$expected_hash" ]]; then
  echo "==> seeding locked frontend dependencies"
  find node_modules -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
  cp -a "$seed_dir/node_modules/." node_modules/
  printf '%s\n' "$expected_hash" >node_modules/.campusos-lock-sha
fi

exec "$@"
