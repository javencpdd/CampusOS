#!/usr/bin/env bash
# Explicit author build only. Package validation and installation never call it.
set -euo pipefail

usage() {
  cat <<'TEXT'
Usage: bash scripts/build-v5-chat-echo.sh --out <new-directory>

Builds the headless Echo provider for Linux/amd64 using only the public Go SDK
and Go standard library, offline and outside the host module. The output must
not exist. It contains image/provider, image/Dockerfile, independent SDK/author
modules, build evidence, and package-template/plugin.json.

Requires an installed Go 1.25+ toolchain; no automatic download. To select an
installed toolchain explicitly, set CAMPUSOS_AUTHOR_GO=/absolute/path/to/go.

The package template has a zero image digest and is NOT a release. This script
does not build images, publish packages, install plugins, or restart the host.
TEXT
}

if [[ "$#" == 1 && ( "$1" == --help || "$1" == -h ) ]]; then
  usage
  exit 0
fi
if [[ "$#" != 2 || "$1" != --out || -z "$2" ]]; then
  usage >&2
  exit 2
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
author_go_binary="${CAMPUSOS_AUTHOR_GO:-$(command -v go)}"
if [[ "$author_go_binary" != /* || ! -x "$author_go_binary" ]]; then
  printf '%s\n' 'CAMPUSOS_AUTHOR_GO must select an installed Go executable by absolute path' >&2
  exit 2
fi
out="$(python3 - "$2" <<'PY'
from pathlib import Path
import sys
print(Path(sys.argv[1]).expanduser().resolve())
PY
)"
# No reuse or cleanup: failed builds remain inspectable and cannot overwrite a
# caller's existing files, including an existing empty directory or symlink.
mkdir -- "$out"
mkdir "$out/sdk" "$out/author" "$out/image" "$out/package-template" "$out/home" "$out/tmp"

for source in "$root"/sdk/go/*.go; do
  case "$source" in *_test.go) continue;; esac
  cp -- "$source" "$out/sdk/"
done
cp -- "$root/plugins/campusos.chat-echo/main.go" "$out/author/main.go"
cp -- "$root/plugins/campusos.chat-echo/Dockerfile" "$out/image/Dockerfile"
cp -- "$root/plugins/campusos.chat-echo/plugin.json" "$out/package-template/plugin.json"

cat >"$out/sdk/go.mod" <<'TEXT'
module github.com/campusos/CampusOS/sdk/go

go 1.25.0
TEXT
cat >"$out/author/go.mod" <<'TEXT'
module example.invalid/campusos-chat-echo

go 1.25.0

require github.com/campusos/CampusOS/sdk/go v0.0.0

replace github.com/campusos/CampusOS/sdk/go => ../sdk
TEXT

author_go() {
  env -i PATH="$PATH" HOME="$out/home" TMPDIR="$out/tmp" \
    GOENV=off GOWORK=off GOFLAGS= GOTOOLCHAIN=local \
    GOPROXY=off GOSUMDB=off GOPATH="$out/gopath" GOMODCACHE="$out/gomodcache" \
    GOCACHE="$out/go-cache" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    "$author_go_binary" "$@"
}

(
  cd "$out/author"
  author_go version >"$out/go-version.txt"
  author_go list -mod=readonly -m -json all >"$out/modules.jsonl"
  author_go list -mod=readonly -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' . \
    | sed '/^$/d' >"$out/nonstandard-imports.txt"
  python3 - "$out/nonstandard-imports.txt" <<'PY'
from pathlib import Path
import sys
actual = set(Path(sys.argv[1]).read_text().splitlines())
expected = {'example.invalid/campusos-chat-echo', 'github.com/campusos/CampusOS/sdk/go'}
if actual != expected:
    raise SystemExit('author build requires only the public SDK and standard library')
PY
  author_go build -mod=readonly -trimpath -buildvcs=false -o "$out/image/provider" .
  author_go version -m "$out/image/provider" >"$out/provider-build-info.txt"
)

python3 - "$out" <<'PY'
from hashlib import sha256
from pathlib import Path
import json
import sys

out = Path(sys.argv[1])
template = json.loads((out / 'package-template/plugin.json').read_text())
if template['backend']['artifact']['image'] != 'localhost/campusos-chat-echo@sha256:' + '0' * 64:
    raise SystemExit('expected the unpublished zero-digest author template')
binary = (out / 'image/provider').read_bytes()
if binary[:6] != b'\x7fELF\x02\x01' or binary[18:20] != b'\x3e\x00':
    raise SystemExit('expected a Linux/amd64 ELF executable')
files = [*sorted((out / 'sdk').glob('*.go')), out / 'sdk/go.mod',
         out / 'author/main.go', out / 'author/go.mod', out / 'image/provider',
         out / 'image/Dockerfile', out / 'package-template/plugin.json',
         out / 'go-version.txt', out / 'modules.jsonl',
         out / 'nonstandard-imports.txt', out / 'provider-build-info.txt']
report = {'schema': 'campusos.v12-08a-author-build/v1',
          'target': 'linux/amd64', 'cgo_enabled': False,
          'offline': True, 'host_module_used': False, 'image_built': False,
          'package_published': False, 'plugin_executed': False,
          'package_template_only': True,
          'files_sha256': {str(p.relative_to(out)): sha256(p.read_bytes()).hexdigest() for p in files}}
(out / 'author-build.json').write_text(json.dumps(report, indent=2) + '\n')
PY

cat >"$out/AUTHOR-BUILD.md" <<'TEXT'
# Echo author build

This directory is an explicit offline author build, not a published or executed
plugin. `author/` and `sdk/` are independent modules. Neither uses the host's
go.mod, go.sum, workspace, internal packages, or third-party modules.
The explicit local Go toolchain is recorded in `go-version.txt`; automatic
toolchain downloads are disabled. Set `CAMPUSOS_AUTHOR_GO` to an installed Go
1.25+ executable when the default `go` is older.

The Container author template uses version 1.1.0. Previously published version
1.0.0 is immutable and must not be replaced with these new bytes.

`image/provider` is a static Linux/amd64 program for the fixed Container HTTP
stdio protocol. `image/Dockerfile` starts it as non-root from scratch. Image
creation must be requested explicitly by the author, for example:

```bash
docker build --pull=false --network=none --tag localhost/campusos-chat-echo:author ./image
docker image inspect --format '{{json .RepoDigests}}' localhost/campusos-chat-echo:author
```

`package-template/plugin.json` contains a 64-zero image digest. Replace that
placeholder with the image's actual local RepoDigest (the exact
localhost/campusos-chat-echo@sha256:... value) before packing. Never use a tag,
an image ID, a guessed digest, or the zero placeholder as release evidence.
If the local Docker engine reports no RepoDigest, resolve image publication
for that engine; package validation cannot manufacture one.

Using an already-built campusosctl, the explicit package steps are:

```bash
campusosctl plugin validate ./package-template
campusosctl plugin pack ./package-template --out ./chat-echo.campusos
campusosctl plugin sign ./chat-echo.campusos --key-id <publisher-key-id> --key-file <private-key-file> --out ./release.json
campusosctl plugin verify ./chat-echo.campusos --release ./release.json --trust <trust-file>
```

Those commands are examples and were not executed by the build script. Sign
only the package containing the actual image digest. Installation, version
approval, current-user consent, configuration and enabling are separate host
operations. Use independent system/user `prefix` values to verify isolation.
No image is pulled or built by package checking or installation. This provider
does not call a model or claim integration with built-in AI/RAG functionality.
TEXT

printf 'Author build: %s\n' "$out"
printf '%s\n' 'Static provider built; package-template still has a zero image digest. No image/package installation or execution occurred.'
