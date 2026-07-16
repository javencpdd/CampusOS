#!/usr/bin/env python3
"""Validate the v10 module, plugin, resource, and user-data boundaries."""

from __future__ import annotations

import hashlib
import json
import os
import re
from pathlib import Path
import sys


ROOT = Path(os.environ.get("CAMPUSOS_GOVERNANCE_ROOT", Path(__file__).resolve().parents[1])).resolve()
DATA = ROOT / "data"
MODULES = ROOT / "modules"
RESOURCE_TYPES = {
    "themes": "theme",
    "homepage-packs": "homepage-pack",
    "space-style-packs": "space-style-pack",
    "skills": "skill",
    "prompts": "prompt",
    "personas": "persona",
    "knowledge-metadata": "knowledge-metadata",
}
RESERVED_MODULE_NAMES = {
    "moderation",
    "category-moderation",
    "personal-space",
    "controlled-richtext-article",
    "personal-schedule",
    "appearance",
    "homepage-customizer",
    "web-theme",
    "ai-gateway",
    "webhook",
    "mcp",
    "message",
    "platform-log",
    "integration-overview",
}
PLUGIN_MARKERS = {"plugin.yaml", "plugin.yml", "go.mod", "Cargo.toml"}
MODULE_MARKERS = {"module.yaml", "module.yml"}
RUNTIME_PATTERN = re.compile(r"^runtime:\s*([^\s#]+)", re.MULTILINE)


def fail(errors: list[str], message: str) -> None:
    errors.append(message)


def directory_checksum(directory: Path) -> str:
    digest = hashlib.sha256()
    files = sorted(
        path
        for path in directory.rglob("*")
        if path.is_file() and path.name != "resource.json"
    )
    for path in files:
        relative = path.relative_to(directory).as_posix().encode()
        digest.update(relative)
        digest.update(b"\0")
        digest.update(path.read_bytes())
        digest.update(b"\0")
    return digest.hexdigest()


def validate_resources(errors: list[str]) -> None:
    resources = DATA / "resources"
    actual_kinds = {path.name for path in resources.iterdir() if path.is_dir()}
    missing = sorted(set(RESOURCE_TYPES) - actual_kinds)
    unexpected = sorted(actual_kinds - set(RESOURCE_TYPES))
    if missing:
        fail(errors, f"data/resources is missing resource kinds: {', '.join(missing)}")
    if unexpected:
        fail(errors, f"data/resources has undeclared resource kinds: {', '.join(unexpected)}")

    for kind_dir, expected_type in RESOURCE_TYPES.items():
        root = resources / kind_dir
        if not root.is_dir():
            continue
        for package in sorted(path for path in root.iterdir() if path.is_dir()):
            manifest_path = package / "resource.json"
            if not manifest_path.is_file():
                fail(errors, f"resource package has no resource.json: {package.relative_to(ROOT)}")
                continue
            try:
                manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
            except (OSError, json.JSONDecodeError) as error:
                fail(errors, f"invalid resource manifest {manifest_path.relative_to(ROOT)}: {error}")
                continue
            if manifest.get("schema") != "campusos.resource/v1":
                fail(errors, f"unsupported resource schema: {manifest_path.relative_to(ROOT)}")
            if manifest.get("id") != package.name:
                fail(errors, f"resource ID must match directory: {package.relative_to(ROOT)}")
            if manifest.get("type") != expected_type:
                fail(errors, f"resource type does not match {kind_dir}: {package.relative_to(ROOT)}")
            entry = manifest.get("entry", "")
            if not entry or Path(entry).is_absolute() or ".." in Path(entry).parts or not (package / entry).is_file():
                fail(errors, f"resource entry is missing or unsafe: {package.relative_to(ROOT)}")
            if manifest.get("checksum") != directory_checksum(package):
                fail(errors, f"resource checksum mismatch: {package.relative_to(ROOT)}")
            for path in package.rglob("*"):
                if path.is_file() and path.name in PLUGIN_MARKERS:
                    fail(errors, f"resource contains plugin runtime marker: {path.relative_to(ROOT)}")


def validate_plugins(errors: list[str]) -> None:
    plugins = DATA / "plugins"
    for plugin in sorted(path for path in plugins.iterdir() if path.is_dir()):
        manifest = plugin / "plugin.yaml"
        if not manifest.is_file():
            fail(errors, f"external plugin has no plugin.yaml: {plugin.relative_to(ROOT)}")
            continue
        if plugin.name in RESERVED_MODULE_NAMES:
            fail(errors, f"compiled module name appears in data/plugins: {plugin.relative_to(ROOT)}")
        text = manifest.read_text(encoding="utf-8")
        match = RUNTIME_PATTERN.search(text)
        if not match or match.group(1) not in {"grpc", "wasm", "http", "remote-http", "mcp"}:
            fail(errors, f"external plugin has unsupported or built-in runtime: {manifest.relative_to(ROOT)}")
        for marker in MODULE_MARKERS:
            if (plugin / marker).exists():
                fail(errors, f"external plugin contains module descriptor: {(plugin / marker).relative_to(ROOT)}")
        for style_dir in plugin.rglob("style-packs"):
            if style_dir.is_dir():
                fail(errors, f"external plugin implementation contains resource data: {style_dir.relative_to(ROOT)}")


def validate_modules(errors: list[str]) -> None:
    descriptor_paths = sorted(MODULES.glob("core/*/module.yaml")) + sorted(MODULES.glob("features/*/module.yaml"))
    if not descriptor_paths:
        fail(errors, "modules has no compiled module descriptors")
    for descriptor in descriptor_paths:
        text = descriptor.read_text(encoding="utf-8")
        if "schema: campusos.module/v1" not in text:
            fail(errors, f"invalid module schema: {descriptor.relative_to(ROOT)}")
        implementation = next(
            (line.split(":", 1)[1].strip() for line in text.splitlines() if line.startswith("implementation:")),
            "",
        )
        platform_kernel_locations = {
            "internal/server/platform_modules.go",
            "internal/platform/feature",
            "internal/plugin",
        }
        if not implementation.startswith("internal/modules/") and implementation not in platform_kernel_locations:
            fail(errors, f"module implementation has no governed module/platform owner: {descriptor.relative_to(ROOT)}")
        if (descriptor.parent / "plugin.yaml").exists():
            fail(errors, f"compiled module contains plugin manifest: {descriptor.parent.relative_to(ROOT)}")


def validate_mutable_data(errors: list[str]) -> None:
    plugin_data = DATA / "plugin_data"
    for name in RESERVED_MODULE_NAMES:
        if (plugin_data / name).exists():
            fail(errors, f"built-in module data remains under data/plugin_data: data/plugin_data/{name}")
    for marker in MODULE_MARKERS:
        for path in plugin_data.rglob(marker):
            fail(errors, f"plugin data contains module descriptor: {path.relative_to(ROOT)}")

    module_data = DATA / "module_data"
    for marker in PLUGIN_MARKERS:
        for path in module_data.rglob(marker):
            fail(errors, f"module data contains plugin implementation marker: {path.relative_to(ROOT)}")


def main() -> int:
    errors: list[str] = []
    required_directories = {
        "modules": MODULES,
        "internal/modules": ROOT / "internal" / "modules",
        "data/plugins": DATA / "plugins",
        "data/plugin_data": DATA / "plugin_data",
        "data/module_data": DATA / "module_data",
        "data/resources": DATA / "resources",
        "data/personal-space": DATA / "personal-space",
    }
    for label, directory in required_directories.items():
        if not directory.is_dir():
            fail(errors, f"missing required directory: {label}")

    if not errors:
        validate_modules(errors)
        validate_plugins(errors)
        validate_resources(errors)
        validate_mutable_data(errors)

    if errors:
        print("data governance check failed:", file=sys.stderr)
        for message in errors:
            print(f"- {message}", file=sys.stderr)
        return 1

    print("data governance check passed: compiled modules, external plugins, module data, plugin data, resources, and user storage are isolated")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
