#!/usr/bin/env python3
"""Focused regression tests for the architecture boundary checkers."""

from __future__ import annotations

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


SCRIPTS = Path(__file__).resolve().parent


def load_module(name: str, filename: str):
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / filename)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


BACKEND = load_module("architecture_boundary_checker", "check-architecture-boundaries.py")
FRONTEND = load_module("frontend_boundary_checker", "check-frontend-boundaries.py")


class ArchitectureCheckTests(unittest.TestCase):
    def write(self, root: Path, relative: str, content: str) -> None:
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def backend_config(self) -> dict:
        return {
            "frozen_source_exception_count": 0,
            "frozen_target_edge_count": 0,
            "legacy_concrete_imports": {},
            "legacy_server_business_composition": [],
            "legacy_server_business_routes": [],
            "legacy_plugin_manager_backreferences": [],
        }

    def test_backend_allows_same_domain_and_rejects_cross_domain_repository(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.write(root, "internal/community/service/service.go", 'package service\nimport "github.com/campusos/CampusOS/internal/community/repository"\n')
            self.assertEqual([], BACKEND.collect_violations(root, self.backend_config()))
            self.write(root, "internal/mcp/mcp.go", 'package mcp\nimport "github.com/campusos/CampusOS/internal/community/repository"\n')
            violations = BACKEND.collect_violations(root, self.backend_config())
            self.assertTrue(any("cross-domain concrete import" in item for item in violations), violations)

    def test_backend_rejects_allowlist_expansion(self) -> None:
        config = self.backend_config()
        config["legacy_concrete_imports"] = {"internal/a/a.go": {"targets": ["internal/b/repository"], "reason": "fixture"}}
        with tempfile.TemporaryDirectory() as directory:
            violations = BACKEND.collect_violations(Path(directory), config)
        self.assertTrue(any("allowlist source exceptions expanded" in item for item in violations), violations)

    def test_frontend_rejects_another_module_internal_import(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = {"frozen_internal_import_exception_count": 0, "internal_import_exceptions": {}}
            self.write(root, "web/src/modules/community/page.ts", "import value from '@/modules/identity/internal/session'\nexport default value\n")
            violations = FRONTEND.collect_violations(root, config)
            self.assertEqual(["web/src/modules/community/page.ts: cross-module internal import -> identity"], violations)
            self.write(root, "web/src/modules/community/page.ts", "import value from '@/modules/identity/public'\nexport default value\n")
            self.assertEqual([], FRONTEND.collect_violations(root, config))


if __name__ == "__main__":
    unittest.main()
