#!/usr/bin/env python3
from __future__ import annotations

import re
import sys
from pathlib import Path
from urllib.parse import unquote


LINK_RE = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")
SKIP_PREFIXES = (
    "http://",
    "https://",
    "mailto:",
    "#",
)


def normalize_target(raw: str) -> str:
    target = raw.strip()
    if not target:
        return target
    if target.startswith("<") and target.endswith(">"):
        target = target[1:-1].strip()
    target = target.split("#", 1)[0]
    return unquote(target)


def should_skip(target: str) -> bool:
    return not target or target.startswith(SKIP_PREFIXES)


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: check_readme_links.py README.md", file=sys.stderr)
        return 2

    readme = Path(sys.argv[1])
    if not readme.exists():
        print(f"missing README file: {readme}", file=sys.stderr)
        return 2

    root = readme.resolve().parent
    missing: list[tuple[int, str]] = []

    for line_no, line in enumerate(readme.read_text(encoding="utf-8").splitlines(), start=1):
        for match in LINK_RE.finditer(line):
            target = normalize_target(match.group(1))
            if should_skip(target):
                continue
            path = (root / target).resolve()
            try:
                path.relative_to(root)
            except ValueError:
                missing.append((line_no, target))
                continue
            if not path.exists():
                missing.append((line_no, target))

    if missing:
        print("README link check failed:")
        for line_no, target in missing:
            print(f"  line {line_no}: {target}")
        return 1

    print(f"README link check passed: {readme}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
