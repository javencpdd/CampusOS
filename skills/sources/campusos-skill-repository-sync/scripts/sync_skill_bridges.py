#!/usr/bin/env python3
"""Generate or verify portable Codex repository Skill discovery bridges."""

from __future__ import annotations

import argparse
import shutil
import sys
from pathlib import Path


BRIDGE_BODY = """# Repository Skill bridge

> 更新时间：2026-08-28

Immediately read [{name} canonical instructions]({canonical}) completely before taking task actions, then follow them faithfully.

Resolve every relative script, reference, or asset path against the canonical directory `skills/sources/{folder}/`, not this bridge directory. This file is only the portable repository discovery entry; do not duplicate or rewrite the canonical workflow here.
"""


def frontmatter(text: str, path: Path) -> tuple[str, str]:
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        raise ValueError(f"{path}: missing YAML frontmatter")
    try:
        closing = next(i for i in range(1, len(lines)) if lines[i].strip() == "---")
    except StopIteration as exc:
        raise ValueError(f"{path}: unterminated YAML frontmatter") from exc
    block = lines[: closing + 1]
    name = ""
    for line in block[1:-1]:
        if line.startswith("name:"):
            name = line.split(":", 1)[1].strip().strip("'\"")
            break
    if not name:
        raise ValueError(f"{path}: frontmatter name is missing")
    return "\n".join(block) + "\n", name


def expected_bridge(source: Path, bridge: Path) -> tuple[str, str]:
    header, name = frontmatter(source.read_text(encoding="utf-8"), source)
    canonical = Path("../../../skills/sources") / source.parent.name / "SKILL.md"
    body = BRIDGE_BODY.format(name=name, canonical=canonical.as_posix(), folder=source.parent.name)
    return header + "\n" + body, name


def synchronize(root: Path, write: bool) -> list[str]:
    source_root = root / "skills" / "sources"
    bridge_root = root / ".agents" / "skills"
    if not source_root.is_dir():
        return [f"missing canonical Skill directory: {source_root}"]

    problems: list[str] = []
    expected_names: set[str] = set()
    for source in sorted(source_root.glob("*/SKILL.md")):
        folder = source.parent.name
        try:
            content, name = expected_bridge(source, bridge_root / folder)
        except ValueError as error:
            problems.append(str(error))
            continue
        if name != folder:
            problems.append(f"{source}: frontmatter name {name!r} does not match folder {folder!r}")
            continue
        expected_names.add(folder)
        bridge_dir = bridge_root / folder
        bridge_file = bridge_dir / "SKILL.md"
        metadata_source = source.parent / "agents" / "openai.yaml"
        metadata_target = bridge_dir / "agents" / "openai.yaml"

        if write:
            bridge_dir.mkdir(parents=True, exist_ok=True)
            bridge_file.write_text(content, encoding="utf-8", newline="\n")
            if metadata_source.is_file():
                metadata_target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(metadata_source, metadata_target)
        else:
            if not bridge_file.is_file():
                problems.append(f"missing bridge: {bridge_file}")
            elif bridge_file.read_text(encoding="utf-8") != content:
                problems.append(f"stale bridge: {bridge_file}")
            if metadata_source.is_file():
                if not metadata_target.is_file():
                    problems.append(f"missing bridge metadata: {metadata_target}")
                elif metadata_target.read_bytes() != metadata_source.read_bytes():
                    problems.append(f"stale bridge metadata: {metadata_target}")

    if bridge_root.is_dir():
        stale = sorted(path.name for path in bridge_root.iterdir() if path.is_dir() and path.name not in expected_names)
        for name in stale:
            problems.append(f"unexpected bridge directory (inspect before removing): {bridge_root / name}")
    return problems


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=".", help="CampusOS repository root")
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true", help="verify bridges without writing")
    mode.add_argument("--write", action="store_true", help="write or refresh expected bridge files")
    args = parser.parse_args()

    root = Path(args.root).resolve()
    problems = synchronize(root, args.write)
    if problems:
        for problem in problems:
            print(f"ERROR: {problem}", file=sys.stderr)
        return 1
    action = "synchronized" if args.write else "verified"
    count = len(list((root / "skills" / "sources").glob("*/SKILL.md")))
    print(f"CampusOS repository Skills {action}: {count} canonical source(s), no bridge drift.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
