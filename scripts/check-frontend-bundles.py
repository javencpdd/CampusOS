#!/usr/bin/env python3
"""Verify v0.9 initial-entry JavaScript/CSS budgets after route/component splitting."""

from __future__ import annotations

import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BUDGETS = {
    "web": {"js": 500_000, "css": 400_000},
    "admin": {"js": 250_000, "css": 400_000},
}


def largest_entry(directory: Path, suffix: str) -> Path | None:
    matches = list((directory / "dist" / "assets").glob(f"index-*.{suffix}"))
    return max(matches, key=lambda path: path.stat().st_size) if matches else None


def main() -> int:
    failed = False
    for app, limits in BUDGETS.items():
        for suffix, limit in limits.items():
            asset = largest_entry(ROOT / app, suffix)
            if asset is None:
                print(f"{app}: missing built index asset (*.{suffix})", file=sys.stderr)
                failed = True
                continue
            size = asset.stat().st_size
            print(f"{app}: {asset.name} {size} bytes (budget {limit})")
            if size > limit:
                print(f"{app}: {suffix} entry exceeds budget", file=sys.stderr)
                failed = True
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
