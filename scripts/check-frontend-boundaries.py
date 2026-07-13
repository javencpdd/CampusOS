#!/usr/bin/env python3
"""Keep future Web/Admin domain modules from importing another module's internals."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any


DEFAULT_ROOT = Path(__file__).resolve().parents[1]
INTERNAL_IMPORT_RE = re.compile(r"@/modules/([^/'\"]+)/internal(?:/|['\"])")


def load_config(root: Path, config_path: str) -> dict[str, Any]:
    return json.loads((root / config_path).read_text(encoding="utf-8"))


def module_owner(path: Path, source_root: Path) -> str:
    relative = path.relative_to(source_root)
    parts = relative.parts
    if len(parts) >= 3 and parts[0] == "modules":
        return parts[1]
    return ""


def collect_violations(root: Path, config: dict[str, Any]) -> list[str]:
    allowed = config.get("internal_import_exceptions", {})
    if len(allowed) > config.get("frozen_internal_import_exception_count", 0):
        return ["frontend internal-import allowlist expanded beyond its frozen maximum"]
    violations: list[str] = []
    for app in ("web", "admin"):
        source_root = root / app / "src"
        if not source_root.exists():
            continue
        for path in sorted(source_root.rglob("*")):
            if path.suffix not in {".ts", ".tsx", ".vue"}:
                continue
            source = path.relative_to(root).as_posix()
            owner = module_owner(path, source_root)
            for target in INTERNAL_IMPORT_RE.findall(path.read_text(encoding="utf-8")):
                if owner == target:
                    continue
                if target in set(allowed.get(source, [])):
                    continue
                violations.append(f"{source}: cross-module internal import -> {target}")
    return sorted(set(violations))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=DEFAULT_ROOT)
    parser.add_argument("--config", default="config/frontend-boundary-allowlist.json")
    args = parser.parse_args()
    root = args.root.resolve()
    violations = collect_violations(root, load_config(root, args.config))
    if violations:
        print("CampusOS frontend boundary check failed:", file=sys.stderr)
        for item in violations:
            print(f"- {item}", file=sys.stderr)
        return 1
    print("CampusOS frontend boundary check passed: no cross-module internal imports")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
