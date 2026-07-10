#!/usr/bin/env python3
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path
from urllib.parse import unquote


LINK_RE = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")
SCHEME_RE = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.-]*:")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Check local Markdown links in README and documentation indexes."
    )
    parser.add_argument(
        "files",
        nargs="+",
        type=Path,
        help="Markdown files to check (legacy usage with only README.md remains valid)",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=Path.cwd(),
        help="Repository root; links resolving outside this directory are rejected",
    )
    return parser.parse_args()


def normalize_target(raw: str) -> str:
    target = raw.strip()
    if not target:
        return target
    if target.startswith("<"):
        closing = target.find(">")
        if closing != -1:
            target = target[1:closing]
    else:
        target = target.split(maxsplit=1)[0]
    return unquote(target.split("#", 1)[0])


def should_skip(target: str) -> bool:
    return not target or target.startswith("#") or bool(SCHEME_RE.match(target))


def resolve_input(path: Path, root: Path) -> Path:
    return path.resolve() if path.is_absolute() else (root / path).resolve()


def main() -> int:
    args = parse_args()
    root = args.root.resolve()
    missing: list[tuple[Path, int, str, str]] = []

    for raw_file in args.files:
        markdown_file = resolve_input(raw_file, root)
        if not markdown_file.is_file():
            missing.append((raw_file, 0, str(raw_file), "input file does not exist"))
            continue

        for line_no, line in enumerate(
            markdown_file.read_text(encoding="utf-8").splitlines(), start=1
        ):
            for match in LINK_RE.finditer(line):
                target = normalize_target(match.group(1))
                if should_skip(target):
                    continue

                resolved = (markdown_file.parent / target).resolve()
                try:
                    resolved.relative_to(root)
                except ValueError:
                    missing.append(
                        (markdown_file, line_no, target, "target escapes repository root")
                    )
                    continue

                if not resolved.exists():
                    missing.append((markdown_file, line_no, target, "target is missing"))

    if missing:
        print("Markdown link check failed:")
        for markdown_file, line_no, target, reason in missing:
            location = f"{markdown_file}:{line_no}" if line_no else str(markdown_file)
            print(f"  {location}: {target} ({reason})")
        return 1

    checked = ", ".join(str(path) for path in args.files)
    print(f"Markdown link check passed: {checked}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
