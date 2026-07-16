#!/usr/bin/env python3
"""Validate the checked-in CampusOS data ownership boundaries.

The checker deliberately validates only stable, repository-level facts. It
does not inspect user-owned files below data/personal-space, and it leaves
legacy style-pack sources readable during the migration to data/resources.
"""

from __future__ import annotations

from pathlib import Path
import sys


ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "data"
RESOURCE_KINDS = {
    "themes",
    "homepage-packs",
    "space-style-packs",
    "skills",
    "prompts",
    "personas",
    "knowledge-metadata",
}
PLUGIN_IMPLEMENTATION_MARKERS = {"plugin.yaml", "plugin.yml", "go.mod", "Cargo.toml"}


def fail(errors: list[str], message: str) -> None:
    errors.append(message)


def find_markers(directory: Path) -> list[Path]:
    if not directory.exists():
        return []
    return [path for path in directory.rglob("*") if path.is_file() and path.name in PLUGIN_IMPLEMENTATION_MARKERS]


def main() -> int:
    errors: list[str] = []
    warnings: list[str] = []

    required_directories = {
        "data/plugins": DATA / "plugins",
        "data/plugin_data": DATA / "plugin_data",
        "data/personal-space": DATA / "personal-space",
        "data/resources": DATA / "resources",
    }
    for label, directory in required_directories.items():
        if not directory.is_dir():
            fail(errors, f"missing required directory: {label}")

    resources = DATA / "resources"
    if resources.is_dir():
        actual_kinds = {path.name for path in resources.iterdir() if path.is_dir()}
        missing = sorted(RESOURCE_KINDS - actual_kinds)
        unexpected = sorted(actual_kinds - RESOURCE_KINDS)
        if missing:
            fail(errors, f"data/resources is missing resource kinds: {', '.join(missing)}")
        if unexpected:
            fail(errors, f"data/resources has undeclared resource kinds: {', '.join(unexpected)}")
        for marker in find_markers(resources):
            fail(errors, f"resource package contains plugin implementation marker: {marker.relative_to(ROOT)}")

    plugin_data = DATA / "plugin_data"
    for marker in find_markers(plugin_data):
        fail(errors, f"plugin data contains plugin implementation marker: {marker.relative_to(ROOT)}")

    plugins = DATA / "plugins"
    if plugins.is_dir():
        style_pack_directories = sorted(path for path in plugins.rglob("style-packs") if path.is_dir())
        for directory in style_pack_directories:
            fail(errors, f"plugin implementation directory contains a style pack: {directory.relative_to(ROOT)}")

    legacy_style_sources = [
        plugin_data / "personal-space" / "style-packs",
        plugin_data / "homepage-customizer" / "style-packs",
        plugin_data / "web-theme" / "style-packs",
    ]
    for directory in legacy_style_sources:
        if not directory.is_dir():
            warnings.append(f"legacy resource source absent: {directory.relative_to(ROOT)}")

    if errors:
        print("data governance check failed:", file=sys.stderr)
        for message in errors:
            print(f"- {message}", file=sys.stderr)
        return 1

    print(
        "data governance check passed: plugin implementations, plugin data, user storage, "
        "resource packages, and legacy style sources have valid repository boundaries"
    )
    for message in warnings:
        print(f"warning: {message}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
