#!/usr/bin/env bash
# Current Linux restore gate. Every run owns a fresh target and cleans it up.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
[[ "$(uname -s)" == Linux ]] || { echo 'native Windows recovery acceptance is deferred; use the Linux target for current evidence' >&2; exit 1; }
work="${CAMPUSOS_V12_08B_OUTPUT:-.cache/v12-08b-exit/acceptance-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
exec python3 scripts/v12-08b-exit-drill.py --work "$work"
