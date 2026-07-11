#!/usr/bin/env bash
set -euo pipefail

CHROME_BIN="${CHROME_BIN:-$(command -v google-chrome || command -v chromium || command -v chromium-browser || true)}"
[[ -n "$CHROME_BIN" ]] || { echo "Chrome/Chromium is required for browser smoke" >&2; exit 127; }

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

check_page() {
  local name="$1" url="$2" marker="$3"
  local dom="$work_dir/$name.html" profile="$work_dir/profile-$name"
  timeout 30 "$CHROME_BIN" --headless --no-sandbox --disable-gpu --disable-dev-shm-usage \
    --user-data-dir="$profile" --virtual-time-budget=5000 --dump-dom "$url" >"$dom" 2>"$work_dir/$name.stderr"
  if ! grep -q "$marker" "$dom"; then
    echo "$name did not render expected marker '$marker'" >&2
    sed -n '1,80p' "$work_dir/$name.stderr" >&2
    return 1
  fi
  echo "browser smoke passed: $name $url"
}

curl --fail --silent --show-error "${API_URL:-http://localhost:8080/api/v1/health}" >/dev/null
check_page web "${WEB_URL:-http://localhost:3000}" 'id="app"'
check_page admin "${ADMIN_URL:-http://localhost:3001}" 'id="app"'
check_page docs "${DOCS_URL:-http://localhost:3002}" 'CampusOS'
