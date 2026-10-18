#!/usr/bin/env bash
set -euo pipefail
# The gateway serves only current managed v5 assets through the dedicated API
# listener. Plugin builds/signatures/imports are explicit author operations.
exec node /workspace/deploy/docker/plugin-ui-server.mjs
