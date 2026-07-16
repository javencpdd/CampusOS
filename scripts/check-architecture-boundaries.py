#!/usr/bin/env python3
"""Reject new CampusOS backend module-boundary violations.

The legacy allowlist is intentionally finite. It documents only the pre-v0.8
debt captured in A0 and can shrink but cannot grow through a normal change.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any


DEFAULT_ROOT = Path(__file__).resolve().parents[1]
IMPORT_RE = re.compile(r'"github\.com/campusos/CampusOS/(internal/[^"\s]+)"')
CONCRETE_SEGMENTS = {"repository", "service", "handler", "adapter"}
FEATURE_ROOTS = {"features/personalspace", "features/richtext", "features/schedule", "features/appearance"}
ROUTE_CALL_RE = re.compile(r"\.(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s*\(")
NEW_CALL_RE = re.compile(r"\b[A-Za-z_][A-Za-z0-9_]*\.New[A-Za-z0-9_]*\s*\(")
MANAGER_STRUCT_RE = re.compile(r"type\s+Manager\s+struct\s*\{(?P<body>.*?)\}", re.DOTALL)
MANAGER_METHOD_RE = re.compile(r"func\s+\(m\s+\*Manager\).*?(?=\nfunc\s|\Z)", re.DOTALL)
MANAGER_INTERNAL_STATE_RE = re.compile(
    r"m\.(?:catalog|audit|ui|events|lifecycle|configs|packages|snapshots|host)\."
    r"(?:mu|operationMu|plugins|repo|revision|listeners|subscriptions)\b"
)


def load_config(root: Path, config_path: str) -> dict[str, Any]:
    return json.loads((root / config_path).read_text(encoding="utf-8"))


def relative_domain(path: str) -> str:
    parts = Path(path).parts
    if len(parts) < 2 or parts[0] != "internal":
        return ""
    if parts[1] == "modules" and len(parts) > 3 and parts[2] in {"core", "features"}:
        return "/".join(parts[2:4])
    if parts[1] == "platform" and len(parts) > 2:
        return "/".join(parts[1:3])
    return parts[1]


def is_concrete(target: str) -> bool:
    return bool(CONCRETE_SEGMENTS.intersection(Path(target).parts))


def config_violations(config: dict[str, Any]) -> list[str]:
    legacy = config.get("legacy_concrete_imports", {})
    source_count = len(legacy)
    edge_count = sum(len(item.get("targets", [])) for item in legacy.values())
    violations: list[str] = []
    if source_count > config.get("frozen_source_exception_count", 0):
        violations.append(
            f"allowlist source exceptions expanded to {source_count}; frozen maximum is {config.get('frozen_source_exception_count', 0)}"
        )
    if edge_count > config.get("frozen_target_edge_count", 0):
        violations.append(
            f"allowlist target edges expanded to {edge_count}; frozen maximum is {config.get('frozen_target_edge_count', 0)}"
        )
    for source, item in legacy.items():
        targets = item.get("targets", [])
        if not item.get("reason"):
            violations.append(f"allowlist entry {source} has no removal reason")
        if len(targets) != len(set(targets)):
            violations.append(f"allowlist entry {source} contains duplicate targets")
    return violations


def allowed_targets(config: dict[str, Any], source: str) -> set[str]:
    entry = config.get("legacy_concrete_imports", {}).get(source, {})
    return set(entry.get("targets", []))


def collect_violations(root: Path, config: dict[str, Any]) -> list[str]:
    violations = config_violations(config)
    server_composition = set(config.get("legacy_server_business_composition", []))
    server_routes = set(config.get("legacy_server_business_routes", []))
    manager_backrefs = set(config.get("legacy_plugin_manager_backreferences", []))

    for path in sorted((root / "internal").rglob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        source = path.relative_to(root).as_posix()
        text = path.read_text(encoding="utf-8")
        owner = relative_domain(source)
        concrete_imported = False
        for target in IMPORT_RE.findall(text):
            target_owner = relative_domain(target)
            if target_owner == owner or not is_concrete(target):
                continue
            concrete_imported = True
            if target not in allowed_targets(config, source):
                violations.append(f"{source}: cross-domain concrete import -> {target}")

        if source.startswith("internal/server/") and concrete_imported and NEW_CALL_RE.search(text) and source not in server_composition:
            violations.append(f"{source}: server constructs business objects; move construction to a domain module")
        if source.startswith("internal/server/") and ROUTE_CALL_RE.search(text) and source not in server_routes:
            violations.append(f"{source}: server registers business routes; use a Route Contributor")

        if owner in FEATURE_ROOTS and re.search(r"\*plugin\.Manager\b", text):
            violations.append(f"{source}: built-in feature directly depends on Plugin Manager")

        if source.startswith("internal/plugin/") and path.name.endswith("services.go"):
            if re.search(r"\bmanager\s+\*Manager\b|\.manager\b", text) and source not in manager_backrefs:
                violations.append(f"{source}: Plugin Platform subservice depends on Manager")

    for plugin_root in (root / "data" / "plugins", root / "examples" / "plugins"):
        if plugin_root.exists():
            for path in sorted(plugin_root.rglob("*.go")):
                if "github.com/campusos/CampusOS/internal/" in path.read_text(encoding="utf-8"):
                    violations.append(f"{path.relative_to(root)}: external plugin imports internal/*")

    manager_path = root / "internal" / "plugin" / "manager.go"
    if manager_path.exists():
        manager_text = manager_path.read_text(encoding="utf-8")
        match = MANAGER_STRUCT_RE.search(manager_text)
        if not match:
            violations.append("internal/plugin/manager.go: Manager facade declaration is missing")
        else:
            body = match.group("body")
            if "sync.Mutex" in body or "sync.RWMutex" in body or "map[" in body:
                violations.append("internal/plugin/manager.go: Manager facade owns mutable core state")
        if re.search(r"&(?:LifecycleService|ConfigService|UIRegistry|EventRegistry|AuditLogService)\s*\{[^}]*\bm\.[A-Za-z_]", manager_text, re.DOTALL):
            violations.append("internal/plugin/manager.go: Plugin Platform subservice calls back into Manager")
        for method in MANAGER_METHOD_RE.findall(manager_text):
            if MANAGER_INTERNAL_STATE_RE.search(method):
                violations.append("internal/plugin/manager.go: Manager facade bypasses a subservice public operation")
                break
        for service_type in ("PackageService", "SnapshotService", "HostAccessService"):
            if f"*{service_type}" not in manager_text:
                violations.append(f"internal/plugin/manager.go: Manager facade is missing {service_type}")

    server_text = "\n".join(path.read_text(encoding="utf-8") for path in sorted((root / "internal" / "server").glob("*.go")))
    if re.search(r"NewRegistryWithStore(?:AndConfig)?\([^\n]*manager", server_text):
        violations.append("internal/server: Feature Registry is wired to Plugin Manager at runtime")

    routes_path = root / "internal" / "projectaudit" / "routes.go"
    if routes_path.exists():
        routes_text = routes_path.read_text(encoding="utf-8")
        if 'Owner:      "transport.httpapi"' in routes_text:
            violations.append("internal/projectaudit/routes.go: HTTP routes use transport as their owner")

    agent_root = root / "internal" / "agentcontract"
    if agent_root.exists():
        forbidden = (
            "pkg/database",
            "pkg/auth",
            "internal/modules/core/identity/repository",
            "internal/modules/core/userstorage",
        )
        for path in sorted(agent_root.rglob("*.go")):
            if path.name.endswith("_test.go"):
                continue
            text = path.read_text(encoding="utf-8")
            for target in forbidden:
                if target in text:
                    violations.append(f"{path.relative_to(root)}: Agent contract imports {target}")
    return sorted(set(violations))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=DEFAULT_ROOT)
    parser.add_argument("--config", default="config/architecture-boundary-allowlist.json")
    args = parser.parse_args()
    root = args.root.resolve()
    config = load_config(root, args.config)
    violations = collect_violations(root, config)
    if violations:
        print("CampusOS architecture boundary check failed:", file=sys.stderr)
        for item in violations:
            print(f"- {item}", file=sys.stderr)
        return 1
    legacy = config.get("legacy_concrete_imports", {})
    edges = sum(len(item.get("targets", [])) for item in legacy.values())
    print(f"CampusOS architecture boundary check passed: {len(legacy)} legacy sources, {edges} legacy concrete edges")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
