#!/usr/bin/env python3
"""Validate the v10 module, plugin, resource, and user-data boundaries."""

from __future__ import annotations

import hashlib
import json
import os
import re
import struct
from pathlib import Path
import sys
import zlib


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
# Built-in static assets are not installable campusos.resource/v1 packages.
STATIC_RESOURCE_FILES = {"branding": {"default-logo.png"}}
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
    unexpected = sorted(actual_kinds - set(RESOURCE_TYPES) - set(STATIC_RESOURCE_FILES))
    if missing:
        fail(errors, f"data/resources is missing resource kinds: {', '.join(missing)}")
    if unexpected:
        fail(errors, f"data/resources has undeclared resource kinds: {', '.join(unexpected)}")

    validate_static_resources(errors)

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


def validate_png_pixels(compressed: bytes, width: int, height: int, depth: int, color: int, interlace: int) -> None:
    # Each PNG pass is scanlines of one filter byte plus packed pixel bits.
    # IHDR has already bounded dimensions and validated this color/depth pair.
    bits_per_pixel = depth * {0: 1, 2: 3, 3: 1, 4: 2, 6: 4}[color]
    passes = ((0, 0, 1, 1),) if not interlace else (
        (0, 0, 8, 8), (4, 0, 8, 8), (0, 4, 4, 8),
        (2, 0, 4, 4), (0, 2, 2, 4), (1, 0, 2, 2), (0, 1, 1, 2),
    )
    scanlines = []
    for x, y, step_x, step_y in passes:
        pass_width = max(0, (width - x + step_x - 1) // step_x)
        pass_height = max(0, (height - y + step_y - 1) // step_y)
        if pass_width and pass_height:
            scanlines.append((1 + (pass_width * bits_per_pixel + 7) // 8, pass_height))
    expected = sum(stride * rows for stride, rows in scanlines)
    decoder = zlib.decompressobj()
    # One extra byte detects an expansion beyond the IHDR-derived bound. Do not
    # call flush(): it would remove the output cap on an attacker-controlled PNG.
    pixels = decoder.decompress(compressed, expected + 1)
    if len(pixels) != expected or not decoder.eof or decoder.unused_data or decoder.unconsumed_tail:
        raise ValueError("PNG pixel stream length or termination")
    offset = 0
    for stride, rows in scanlines:
        for _ in range(rows):
            if pixels[offset] > 4:
                raise ValueError("PNG scanline filter")
            offset += stride


def validate_static_resources(errors: list[str]) -> None:
    for directory, allowed in STATIC_RESOURCE_FILES.items():
        root = DATA / "resources" / directory
        # Older layouts may omit optional built-in branding entirely.
        if not root.exists() and not root.is_symlink():
            continue
        if root.is_symlink() or not root.is_dir():
            fail(errors, f"static resource group must be a real directory: {root.relative_to(ROOT)}")
            continue
        if {path.name for path in root.iterdir()} != allowed:
            fail(errors, f"static resource group has missing or undeclared files: {root.relative_to(ROOT)}")
        for name in allowed:
            path = root / name
            if path.is_symlink() or not path.is_file():
                fail(errors, f"static resource must be a regular file: {path.relative_to(ROOT)}")
                continue
            if not 0 < path.stat().st_size <= 2 * 1024 * 1024:
                fail(errors, f"static logo exceeds the 2 MiB image limit: {path.relative_to(ROOT)}")
                continue
            data = path.read_bytes()
            # Match homepage's existing logo bounds and validate PNG framing,
            # CRCs and bounded scanlines without an image-library dependency.
            try:
                if data[:8] != b"\x89PNG\r\n\x1a\n":
                    raise ValueError("PNG signature")
                offset, chunks, image_data = 8, [], []
                while offset < len(data):
                    length = struct.unpack_from(">I", data, offset)[0]
                    end = offset + 12 + length
                    if end > len(data):
                        raise ValueError("truncated chunk")
                    kind = data[offset + 4:offset + 8]
                    payload = data[offset + 8:end - 4]
                    crc = struct.unpack_from(">I", data, end - 4)[0]
                    if zlib.crc32(kind + payload) != crc:
                        raise ValueError("chunk CRC")
                    if not chunks:
                        if kind != b"IHDR" or length != 13:
                            raise ValueError("IHDR")
                        width, height, depth, color, compression, filtering, interlace = struct.unpack(">IIBBBBB", payload)
                        depths = {0: {1, 2, 4, 8, 16}, 2: {8, 16}, 3: {1, 2, 4, 8}, 4: {8, 16}, 6: {8, 16}}
                        if not (0 < width <= 1024 and 0 < height <= 1024) or depth not in depths.get(color, set()) or compression or filtering or interlace not in (0, 1):
                            raise ValueError("image bounds or format")
                    elif kind == b"IHDR":
                        raise ValueError("duplicate IHDR")
                    if kind == b"IDAT":
                        image_data.append(payload)
                    chunks.append(kind)
                    offset = end
                    if kind == b"IEND":
                        if length or end != len(data):
                            raise ValueError("IEND")
                        break
                if chunks[-1:] != [b"IEND"] or b"IDAT" not in chunks:
                    raise ValueError("incomplete PNG")
                validate_png_pixels(b"".join(image_data), width, height, depth, color, interlace)
            except (ValueError, struct.error, zlib.error):
                fail(errors, f"static logo must be a bounded PNG with valid chunks: {path.relative_to(ROOT)}")


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
            "internal/platform/observability",
            "internal/platform/reliability",
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
