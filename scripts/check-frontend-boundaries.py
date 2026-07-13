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
LEGACY_API_IMPORT_RE = re.compile(r"(?:from\s+['\"]@/api['\"]|import\s*\(\s*['\"]@/api['\"]\s*\))")
LEGACY_VIEW_IMPORT_RE = re.compile(r"(?:from\s+['\"]@/views/|import\s*\(\s*['\"]@/views/)")
MAX_FACADE_LINES = 24
MAX_ROUTER_LINES = 72
MAX_STORE_FACADE_LINES = 4


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
            text = path.read_text(encoding="utf-8")
            if source not in {f"{app}/src/api/index.ts", f"{app}/src/api/index.tsx"} and LEGACY_API_IMPORT_RE.search(text):
                violations.append(f"{source}: must import the owning module API instead of @/api")
            if LEGACY_VIEW_IMPORT_RE.search(text):
                violations.append(f"{source}: must import a page from its owning module instead of @/views")
            owner = module_owner(path, source_root)
            for target in INTERNAL_IMPORT_RE.findall(text):
                if owner == target:
                    continue
                if target in set(allowed.get(source, [])):
                    continue
                violations.append(f"{source}: cross-module internal import -> {target}")
        for relative, maximum in (("api/index.ts", MAX_FACADE_LINES), ("router/index.ts", MAX_ROUTER_LINES)):
            root_file = source_root / relative
            if root_file.exists() and len(root_file.read_text(encoding="utf-8").splitlines()) > maximum:
                violations.append(f"{app}/src/{relative}: compatibility facade exceeds {maximum} lines")
        legacy_views = source_root / "views"
        if legacy_views.exists():
            for path in sorted(legacy_views.rglob("*.vue")):
                violations.append(f"{path.relative_to(root).as_posix()}: page implementation must live in an owning module")
        compatibility_stores = source_root / "stores"
        if compatibility_stores.exists():
            for path in sorted(compatibility_stores.glob("*.ts")):
                line_count = len(path.read_text(encoding="utf-8").splitlines())
                if line_count > MAX_STORE_FACADE_LINES:
                    violations.append(
                        f"{path.relative_to(root).as_posix()}: compatibility store exceeds {MAX_STORE_FACADE_LINES} lines"
                    )
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
