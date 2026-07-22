#!/usr/bin/env python3
"""Guard the first v13 error-contract migration against raw error leaks."""

from __future__ import annotations

from pathlib import Path
import re
import sys


ROOT = Path(__file__).resolve().parents[1]
MIGRATED_HANDLERS = (
    "internal/modules/core/identity/handler/user_handler.go",
    "internal/modules/features/mutualaid/handler.go",
    "internal/modules/features/secondhand/handler.go",
    "internal/platform/reliability/handler.go",
)
TRANSLATORS = (
    "internal/modules/core/identity/handler/error_contract.go",
    "internal/modules/features/mutualaid/error_contract.go",
    "internal/modules/features/secondhand/error_contract.go",
    "internal/platform/reliability/error_contract.go",
)
RAW_RESPONSE = re.compile(
    r"response\.(?:Error|ErrorWithDetails)\([^\n]*\b(?:err|error)\.Error\(\)"
)


def main() -> int:
    failures: list[str] = []
    for relative in MIGRATED_HANDLERS:
        path = ROOT / relative
        source = path.read_text(encoding="utf-8")
        for match in RAW_RESPONSE.finditer(source):
            line = source.count("\n", 0, match.start()) + 1
            failures.append(f"{relative}:{line}: raw error text reaches a public response")

    for relative in TRANSLATORS:
        path = ROOT / relative
        if not path.is_file() or "apperror.MustTranslator" not in path.read_text(encoding="utf-8"):
            failures.append(f"{relative}: module Translator is missing")

    frontend_contracts = (
        ROOT / "web/src/shared/api/error.ts",
        ROOT / "admin/src/shared/api/error.ts",
    )
    if all(path.is_file() for path in frontend_contracts):
        if frontend_contracts[0].read_bytes() != frontend_contracts[1].read_bytes():
            failures.append("Web and Admin API error parsers have drifted")

    if failures:
        print("error contract check failed:", file=sys.stderr)
        for failure in failures:
            print(f"- {failure}", file=sys.stderr)
        return 1
    print("error contract check passed: catalog translators and raw-error guards are aligned")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
