#!/usr/bin/env bash
# 用法：在 CampusOS 仓库根目录执行 bash migrations/tools/generate_er.sh
# 可选：传入 --check 只检查已生成产物是否与 migrations 一致。
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

python_command=()
if [[ -n "${PYTHON_BIN:-}" ]]; then
  python_command=("$PYTHON_BIN")
else
  for candidate in python3 python; do
    if command -v "$candidate" >/dev/null 2>&1 &&
      "$candidate" -c 'import sys; raise SystemExit(0 if sys.version_info >= (3, 10) else 1)' >/dev/null 2>&1; then
      python_command=("$candidate")
      break
    fi
  done
  if [[ ${#python_command[@]} -eq 0 ]] && command -v py >/dev/null 2>&1 &&
    py -3 -c 'import sys; raise SystemExit(0 if sys.version_info >= (3, 10) else 1)' >/dev/null 2>&1; then
    python_command=(py -3)
  fi
fi

if [[ ${#python_command[@]} -eq 0 ]]; then
  echo "未找到可执行的 Python 3.10+。可通过 PYTHON_BIN 显式指定解释器。" >&2
  exit 2
fi

exec "${python_command[@]}" migrations/tools/generate_er.py "$@"
