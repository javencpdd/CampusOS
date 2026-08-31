#!/usr/bin/env python3
"""Validate repository-local links in current CampusOS documentation."""

from __future__ import annotations

import re
import sys
from pathlib import Path
from urllib.parse import unquote


ROOT = Path(__file__).resolve().parents[1]
SCAN_ROOTS = (
    ROOT / "README.md",
    ROOT / "docs" / "README.md",
    ROOT / "docs" / "api",
    ROOT / "docs" / "architecture",
    ROOT / "docs" / "help",
    ROOT / "docs" / "skills",
    ROOT / "docs" / "项目计划v0.6",
    ROOT / "docs" / "项目计划v0.7",
    ROOT / "docs" / "项目计划v0.8",
    ROOT / "docs" / "进度" / "v0.6-dev",
    ROOT / "docs" / "进度" / "v0.7-dev",
    ROOT / "docs" / "进度" / "v0.8-dev",
    ROOT / "docs-site",
    ROOT / "data" / "plugins",
    ROOT / "sdk",
    ROOT / "skills",
)
EXCLUDED_PARTS = {"node_modules", "dist", ".vitepress", ".git"}
INLINE_LINK = re.compile(r"!?\[[^\]]*\]\(([^)]+)\)")
REFERENCE_LINK = re.compile(r"^\s*\[[^\]]+\]:\s*(\S+)", re.MULTILINE)
FENCED_BLOCK = re.compile(r"```.*?```|~~~.*?~~~", re.DOTALL)


def markdown_files() -> list[Path]:
    files: set[Path] = set()
    for candidate in SCAN_ROOTS:
        if candidate.is_file() and candidate.suffix.lower() == ".md":
            files.add(candidate)
        elif candidate.is_dir():
            for path in candidate.rglob("*.md"):
                if not EXCLUDED_PARTS.intersection(path.relative_to(ROOT).parts):
                    files.add(path)
    return sorted(files)


def normalize_target(raw: str) -> str:
    target = raw.strip()
    if target.startswith("<") and ">" in target:
        target = target[1 : target.index(">")]
    elif " " in target:
        target = target.split(None, 1)[0]
    return unquote(target.strip())


def should_skip(target: str) -> bool:
    lower = target.lower()
    return (
        not target
        or target.startswith("#")
        or target.startswith("/")
        or lower.startswith(("http://", "https://", "mailto:", "tel:", "data:"))
        or "{{" in target
        or "${" in target
    )


def resolve(source: Path, target: str) -> Path:
    without_anchor = target.split("#", 1)[0].split("?", 1)[0]
    return (source.parent / without_anchor).resolve()


def main() -> int:
    errors: list[str] = []
    checked = 0
    for source in markdown_files():
        text = FENCED_BLOCK.sub("", source.read_text(encoding="utf-8"))
        links = [*INLINE_LINK.findall(text), *REFERENCE_LINK.findall(text)]
        for raw in links:
            target = normalize_target(raw)
            if should_skip(target):
                continue
            checked += 1
            resolved = resolve(source, target)
            try:
                resolved.relative_to(ROOT)
            except ValueError:
                errors.append(f"{source.relative_to(ROOT)}: link escapes repository: {target}")
                continue
            if not resolved.exists():
                errors.append(f"{source.relative_to(ROOT)}: missing target: {target}")

    if errors:
        print("CampusOS documentation link check failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print(f"CampusOS documentation link check passed: {checked} local links")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
