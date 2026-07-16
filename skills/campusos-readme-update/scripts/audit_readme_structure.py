#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import re
from collections import deque
from pathlib import Path
from urllib.parse import unquote


LINK_RE = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")
SCHEME_RE = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.-]*:")
DETAIL_HEADING_MARKERS = (
    "完整 API",
    "API 明细",
    "接口清单",
    "环境变量详解",
    "完整环境变量",
    "配置参考",
    "数据库表",
    "迁移历史",
    "故障排查",
    "常见问题",
    "插件开发教程",
    "Skill 使用说明",
    "CI/CD 详解",
    "版本历史",
    "更新日志",
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Audit CampusOS README scope and docs-portal reachability."
    )
    parser.add_argument("--root", type=Path, default=Path.cwd())
    parser.add_argument("--readme", type=Path, default=Path("README.md"))
    parser.add_argument("--docs-index", type=Path, default=Path("docs/README.md"))
    parser.add_argument("--max-lines", type=int, default=180)
    parser.add_argument("--max-sections", type=int, default=8)
    parser.add_argument(
        "--require-doc",
        action="append",
        default=[],
        type=Path,
        help="Document that must be reachable from docs/README.md; repeat as needed",
    )
    parser.add_argument("--json", action="store_true")
    return parser.parse_args()


def resolve(root: Path, path: Path) -> Path:
    return path.resolve() if path.is_absolute() else (root / path).resolve()


def normalize_target(raw: str) -> str:
    target = raw.strip()
    if target.startswith("<"):
        closing = target.find(">")
        if closing != -1:
            target = target[1:closing]
    else:
        target = target.split(maxsplit=1)[0] if target else target
    return unquote(target.split("#", 1)[0])


def local_markdown_targets(markdown_file: Path, root: Path) -> list[Path]:
    targets: list[Path] = []
    for match in LINK_RE.finditer(markdown_file.read_text(encoding="utf-8")):
        target = normalize_target(match.group(1))
        if not target or target.startswith("#") or SCHEME_RE.match(target):
            continue
        resolved = (markdown_file.parent / target).resolve()
        try:
            resolved.relative_to(root)
        except ValueError:
            continue
        if resolved.is_dir():
            resolved = resolved / "README.md"
        if resolved.suffix.lower() == ".md" and resolved.is_file():
            targets.append(resolved)
    return targets


def reachable_markdown(start: Path, root: Path) -> set[Path]:
    visited: set[Path] = set()
    queue: deque[Path] = deque([start])
    docs_root = start.parent.resolve()
    while queue:
        current = queue.popleft()
        if current in visited or not current.is_file():
            continue
        visited.add(current)
        for target in local_markdown_targets(current, root):
            try:
                target.relative_to(docs_root)
            except ValueError:
                continue
            queue.append(target)
    return visited


def has_link(source: Path, target: Path, root: Path) -> bool:
    return target in local_markdown_targets(source, root)


def main() -> int:
    args = parse_args()
    root = args.root.resolve()
    readme = resolve(root, args.readme)
    docs_index = resolve(root, args.docs_index)
    errors: list[str] = []
    warnings: list[str] = []

    if not readme.is_file():
        errors.append(f"missing root README: {readme}")
    if not docs_index.is_file():
        errors.append(f"missing docs portal: {docs_index}")
    if errors:
        return emit(args.json, errors, warnings, {})

    readme_text = readme.read_text(encoding="utf-8")
    readme_lines = readme_text.splitlines()
    docs_text = docs_index.read_text(encoding="utf-8")
    headings = [line[3:].strip() for line in readme_lines if line.startswith("## ")]

    if len(readme_lines) > args.max_lines:
        errors.append(
            f"README has {len(readme_lines)} lines; maximum is {args.max_lines}. "
            "Move secondary details into docs/."
        )
    elif len(readme_lines) > 120:
        warnings.append(
            f"README has {len(readme_lines)} lines; review whether details can move to docs/."
        )

    if len(headings) > args.max_sections:
        errors.append(
            f"README has {len(headings)} level-two sections; maximum is {args.max_sections}."
        )

    for required in ("当前状态", "快速开始", "文档"):
        if required not in headings:
            errors.append(f"README is missing required section: {required}")

    current_version = ""
    version_source = root / "internal/platform/version/version.go"
    if version_source.is_file():
        match = re.search(
            r'\bNumber\s*=\s*"(\d+\.\d+\.\d+)"',
            version_source.read_text(encoding="utf-8"),
        )
        if match:
            current_version = "v" + match.group(1)
    if not current_version:
        server_main = root / "cmd/server/main.go"
        if server_main.is_file():
            match = re.search(
                r"CampusOS (v\d+\.\d+\.\d+) starting",
                server_main.read_text(encoding="utf-8"),
            )
            if match:
                current_version = match.group(1)
    if current_version:
        version_surfaces = {
            "README baseline": readme,
            "docs portal baseline": docs_index,
            "docs-site home baseline": root / "docs-site/index.md",
            "docs-site introduction baseline": root / "docs-site/guide/introduction.md",
        }
        for label, surface in version_surfaces.items():
            if surface.is_file() and current_version not in surface.read_text(
                encoding="utf-8"
            ):
                errors.append(
                    f"{label} does not match server startup version: {current_version}"
                )

    for heading in headings:
        if any(marker.lower() in heading.lower() for marker in DETAIL_HEADING_MARKERS):
            errors.append(
                f"detailed reference section belongs under docs/, not README: {heading}"
            )

    if not has_link(readme, docs_index, root):
        errors.append("README must link directly to docs/README.md")
    if not has_link(docs_index, readme, root):
        errors.append("docs/README.md must link back to the root README")

    category_markers = {
        "docs/help": "help/",
        "docs/api": "api/",
        "docs/architecture": "architecture/",
        "docs/skills": "skills/",
        "docs/进度": "进度/",
    }
    for directory, marker in category_markers.items():
        if (root / directory).is_dir() and marker not in docs_text:
            errors.append(f"docs portal does not expose existing category: {directory}")

    reachable = reachable_markdown(docs_index, root)
    for required_doc in args.require_doc:
        target = resolve(root, required_doc)
        try:
            target.relative_to(root / "docs")
        except ValueError:
            errors.append(f"--require-doc must be under docs/: {required_doc}")
            continue
        if not target.is_file():
            errors.append(f"required document does not exist: {required_doc}")
        elif target not in reachable:
            errors.append(f"document is not reachable from docs/README.md: {required_doc}")

    direct_doc_links = sum(
        1
        for target in local_markdown_targets(readme, root)
        if target != docs_index and (root / "docs") in target.parents
    )
    if direct_doc_links > 14:
        warnings.append(
            f"README has {direct_doc_links} direct documentation links; prefer routing through docs/README.md."
        )

    metrics = {
        "readme_lines": len(readme_lines),
        "level_two_sections": len(headings),
        "direct_doc_links": direct_doc_links,
        "reachable_docs": len(reachable),
    }
    return emit(args.json, errors, warnings, metrics)


def emit(
    as_json: bool, errors: list[str], warnings: list[str], metrics: dict[str, int]
) -> int:
    if as_json:
        payload = {"ok": not errors, "errors": errors, "warnings": warnings, **metrics}
        print(json.dumps(payload, ensure_ascii=False, indent=2))
    else:
        print("CampusOS README structure audit")
        for key, value in metrics.items():
            print(f"{key.replace('_', ' ')}: {value}")
        for warning in warnings:
            print(f"warning: {warning}")
        for error in errors:
            print(f"error: {error}")
        print("result: passed" if not errors else "result: failed")
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
