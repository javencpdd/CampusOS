#!/usr/bin/env bash
set -euo pipefail

if [[ $# -gt 0 ]]; then
  go run ./scripts/hash-password.go "$1"
  exit 0
fi

read -r -s -p "Password: " password
echo
printf '%s' "$password" | go run ./scripts/hash-password.go
