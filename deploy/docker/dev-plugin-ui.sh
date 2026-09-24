#!/usr/bin/env bash
set -euo pipefail

# This development-only process builds reviewed plugin source into `dist/` and
# then serves only immutable releases below plugins/.installed. It never serves
# frontend source, node_modules, staging files, or a user configuration path.
cd /workspace/plugins/campusos.pdf-viewer/frontend
pnpm build
exec node /workspace/deploy/docker/plugin-ui-server.mjs
