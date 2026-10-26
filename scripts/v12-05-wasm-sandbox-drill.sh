#!/usr/bin/env bash
# First05 slice: signed archive -> real bounded Wasm JSON kernel, no production Runner.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mode="${1:-target}"
work="${CAMPUSOS_V5_WASM_DIR:?set absolute owned evidence directory}"
case "$work" in "$root"/.cache/v12-05-wasm-sandbox/acceptance.*) ;; *) echo 'requires owned05 sandbox evidence directory' >&2; exit 2;; esac
export GOCACHE="$root/.cache/v12-g0/go-cache" TMPDIR="$work/tmp"
mkdir -p "$TMPDIR" "$work/native" "$work/home"
check(){
 local name="$1"; shift
 if "$@" >"$work/common-$name.log" 2>&1; then echo "$name" >>"$work/common-passed.txt"
 else tail -n 35 "$work/common-$name.log" >&2; return 1; fi
}
native(){
 local name="$1" package="$2" pattern="$3"
 (cd "$package"; env -i PATH="$PATH" HOME="$work/home" TMPDIR="$TMPDIR" GORACE=halt_on_error=1 "$work/native/$name.test" -test.run "$pattern" -test.v -test.count=1 -test.timeout=30s) >"$work/native/$name.log" 2>"$work/native/$name.stderr"
 go tool test2json -t <"$work/native/$name.log" >"$work/native/$name.jsonl"
}
if [[ "$mode" == target ]]; then
 python3 scripts/v12-05-wasm-sandbox-report.py freeze "$work"
 uname -srm >"$work/environment.txt"
 go version >>"$work/environment.txt"
 go test -c -race -o "$work/native/capture.test" ./internal/plugin >"$work/native/capture-build.log" 2>&1
 go test -c -race -o "$work/native/sandbox.test" ./internal/plugin/wasm >"$work/native/sandbox-build.log" 2>&1
 file "$work/native/capture.test" "$work/native/sandbox.test" >"$work/native/binaries.txt"
 go version -m "$work/native/capture.test" >"$work/native/capture-build-info.txt"
 go version -m "$work/native/sandbox.test" >"$work/native/sandbox-build-info.txt"
 native capture internal/plugin '^TestV5WasmProgram'
 native sandbox internal/plugin/wasm '^TestV5Sandbox'
 python3 scripts/v12-05-wasm-sandbox-report.py verify "$work"
elif [[ "$mode" == common ]]; then
 python3 scripts/v12-05-wasm-sandbox-report.py verify "$work"
 : >"$work/common-passed.txt"
 check full-go go test ./... -count=1
 check affected-race go test -race ./internal/plugin/... ./internal/server -count=1
 check contracts make contracts-check
 check architecture make architecture-check
 check readme make readme-check
 check docs-links make docs-links
 check docs-build make docs-build
 check line-endings python3 scripts/check-line-endings.py --include-untracked
 check diff git diff --check
elif [[ "$mode" == documents ]]; then
 python3 scripts/v12-05-wasm-sandbox-report.py verify "$work"
 : >"$work/final-document-passed.txt"
 document(){
  local name="$1"; shift
  if "$@" >"$work/final-document-$name.log" 2>&1; then echo "$name" >>"$work/final-document-passed.txt"
  else tail -n 35 "$work/final-document-$name.log" >&2; return 1; fi
 }
 document docs-links make docs-links
 document docs-build make docs-build
 document architecture make architecture-check
 document line-endings python3 scripts/check-line-endings.py --include-untracked
 document diff git diff --check
elif [[ "$mode" == final ]]; then
 python3 scripts/v12-05-wasm-sandbox-report.py seal "$work"
else
 echo 'usage: v12-05-wasm-sandbox-drill.sh target|common|documents|final' >&2; exit 2
fi
