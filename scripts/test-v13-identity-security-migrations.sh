#!/usr/bin/env bash
# 用法：保留的历史命令入口；其原迁移链已被 v1 干净基线取代。
# 执行时统一转发到当前的零建库、checksum、reset 与 up/down/up 验证。
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
echo "==> 历史 migration drill 已合并为 test-v1-database-baseline.sh"
exec bash "$script_dir/test-v1-database-baseline.sh"