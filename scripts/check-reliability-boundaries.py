#!/usr/bin/env python3
"""Reject new v11 reliability-boundary regressions without rewriting legacy code."""

from __future__ import annotations

import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SERVICE_ROOT = ROOT / "internal" / "modules"
DIRECT_BEGIN = re.compile(r"\b(?:\w+\.)?pool\.Begin(?:Tx)?\s*\(")
SLEEP = re.compile(r"\btime\.Sleep\s*\(")
UNTRACKED_GOROUTINE = re.compile(r"\bgo\s+func\s*\(")


def main() -> int:
    errors: list[str] = []
    for path in sorted(SERVICE_ROOT.rglob("*.go")):
        if path.name.endswith("_test.go") or "/repository/" in path.as_posix():
            continue
        text = path.read_text(encoding="utf-8")
        if DIRECT_BEGIN.search(text):
            errors.append(f"{path.relative_to(ROOT)}: application code must use TxKernel, not pool.Begin")

    for directory in (ROOT / "internal" / "platform" / "reliability", ROOT / "internal" / "modules" / "features" / "webhook"):
        for path in sorted(directory.rglob("*.go")):
            if path.name.endswith("_test.go"):
                continue
            text = path.read_text(encoding="utf-8")
            if SLEEP.search(text):
                errors.append(f"{path.relative_to(ROOT)}: durable retry logic must not use time.Sleep")
            if directory.name == "webhook" and UNTRACKED_GOROUTINE.search(text):
                errors.append(f"{path.relative_to(ROOT)}: webhook delivery must use the durable worker, not a goroutine")

    resource_paths = [
        ROOT / "internal" / "modules" / "features" / "richtext",
        ROOT / "internal" / "modules" / "features" / "schedule",
    ]
    for directory in resource_paths:
        for path in sorted(directory.rglob("*.go")):
            if path.name.endswith("_test.go"):
                continue
            if "internal/modules/features/personalspace" in path.read_text(encoding="utf-8"):
                errors.append(f"{path.relative_to(ROOT)}: must use User Storage port, not Personal Space implementation")

    if errors:
        print("reliability boundary check failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print("reliability boundary check passed: TxKernel, durable worker, and User Storage boundaries are intact")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
