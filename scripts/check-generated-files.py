#!/usr/bin/env python3
"""Keep local frontend/doc build output out of the repository index."""

from __future__ import annotations

from pathlib import Path
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]
IGNORED_OUTPUTS = (
    "web/dist/",
    "admin/dist/",
    "docs-site/.vitepress/cache/",
    "docs-site/.vitepress/dist/",
    "docs-site/.vitepress/.temp/",
)


def tracked_files() -> list[str]:
    result = subprocess.run(
        ["git", "ls-files"],
        cwd=ROOT,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if result.returncode:
        raise RuntimeError(result.stderr.strip() or "git ls-files failed")
    return [line for line in result.stdout.splitlines() if line]


def main() -> int:
    gitignore = (ROOT / ".gitignore").read_text(encoding="utf-8")
    missing_rules = [directory for directory in IGNORED_OUTPUTS if directory not in gitignore]
    if missing_rules:
        print("generated file policy is missing .gitignore rules:", file=sys.stderr)
        for directory in missing_rules:
            print(f"- {directory}", file=sys.stderr)
        return 1

    offenders = [
        path
        for path in tracked_files()
        if any(path.startswith(directory) for directory in IGNORED_OUTPUTS)
    ]
    if offenders:
        print("generated build output is still tracked:", file=sys.stderr)
        for path in offenders:
            print(f"- {path}", file=sys.stderr)
        return 1

    print("generated file policy check passed: frontend and docs build output is ignored and untracked")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except RuntimeError as error:
        print(f"generated file policy check failed: {error}", file=sys.stderr)
        raise SystemExit(1)
