#!/usr/bin/env bash
set -euo pipefail

CHROME_BIN="${CHROME_BIN:-$(command -v google-chrome || command -v chromium || command -v chromium-browser || true)}"
[[ -n "$CHROME_BIN" ]] || { echo "Chrome/Chromium is required for browser smoke" >&2; exit 127; }

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

check_page() {
  local name="$1" url="$2" marker="$3"
  local dom="$work_dir/$name.html" screenshot="$work_dir/$name.png"
  curl --fail --silent --show-error "$url" >"$dom"
  if ! grep -q "$marker" "$dom"; then
    echo "$name did not serve expected marker '$marker'" >&2
    return 1
  fi
  timeout 30 env CHROME_BIN="$CHROME_BIN" node web/tests/browser-render-smoke.mjs "$url" "$screenshot" "$name" \
    >"$work_dir/$name.stdout" 2>"$work_dir/$name.stderr"
  if [[ ! -s "$screenshot" ]]; then
    echo "$name did not produce a non-empty Chrome screenshot" >&2
    sed -n '1,80p' "$work_dir/$name.stderr" >&2
    return 1
  fi
  echo "browser smoke passed: $name $url"
}

curl --fail --silent --show-error "${API_URL:-http://localhost:8080/api/v1/health}" >/dev/null
check_page web "${WEB_URL:-http://localhost:3000}" 'id="app"'
check_page admin "${ADMIN_URL:-http://localhost:3001}" 'id="app"'
check_page docs "${DOCS_URL:-http://localhost:3002}" 'CampusOS'

if [[ "${RUN_BROWSER_WORKFLOW:-true}" == "true" ]]; then
  echo "running authenticated browser workflow"
  (
    cd web
    CHROME_BIN="$CHROME_BIN" \
      WEB_URL="${WEB_URL:-http://localhost:3000}" \
      ADMIN_URL="${ADMIN_URL:-http://localhost:3001}" \
      DOCS_URL="${DOCS_URL:-http://localhost:3002}" \
      API_BASE_URL="${API_BASE_URL:-http://localhost:8080/api/v1}" \
      node tests/browser-workflow.mjs
  )
fi

if [[ "${RUN_STYLE_PACK_SMOKE:-true}" == "true" ]]; then
  echo "running style-pack desktop/mobile workflow"
  (
    cd web
    CHROME_BIN="$CHROME_BIN" \
      WEB_URL="${WEB_URL:-http://localhost:3000}" \
      STYLE_PACK_SCREENSHOT_DIR="${STYLE_PACK_SCREENSHOT_DIR:-$work_dir/style-pack}" \
      node tests/style-pack-responsive.mjs
  )
fi

if [[ "${RUN_RESPONSIVE_SMOKE:-true}" == "true" ]]; then
  echo "running five-viewport responsive workflow"
  (
    cd web
    CHROME_BIN="$CHROME_BIN" \
      WEB_URL="${WEB_URL:-http://localhost:3000}" \
      ADMIN_URL="${ADMIN_URL:-http://localhost:3001}" \
      node tests/responsive-workflow.mjs
  )
fi
