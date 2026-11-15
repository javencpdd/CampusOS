import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("version_sync", Path(__file__).with_name("check-version-sync.py"))
VERSION_SYNC = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VERSION_SYNC)


class VersionSyncTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.patch = patch.object(VERSION_SYNC, "ROOT", self.root)
        self.patch.start()
        self.addCleanup(self.patch.stop)
        self.files = {
            "internal/platform/version/version.go": 'const (\n Number = "1.1.0-dev"\n)',
            "docs-site/package.json": '{"version":"1.1.0-dev"}',
            "sdk/typescript/package.json": '{"version":"1.1.0-dev"}',
            "sdk/go/hostapi.go": 'const (\n SDKVersion = "v1.1"\n)',
            "docs/api/openapi-v0.6-current.yaml": "info:\n  version: 1.1.0-dev-experimental\n",
            "deploy/docker/.env.example": "CAMPUSOS_IMAGE_TAG=1.1.0-dev\n",
            "deploy/docker/Dockerfile": "ARG CAMPUSOS_VERSION=1.1.0-dev\n",
            "docs/plan.md": "# CampusOS v1.2 版本计划\n",
            "config/project-status.json": json.dumps({"schema": "campusos.project-status/v1", "development_stage": "v1.2-dev", "plan": "docs/plan.md"}),
        }
        for name in ["compose.deploy.yml", *[f"deploy/docker/components/compose.{app}.yml" for app in ["api", "web", "admin", "docs"]]]:
            self.files[name] = "image: campusos:${CAMPUSOS_IMAGE_TAG:-1.1.0-dev}\n"
        for name in ["README.md", "docs/README.md", "docs-site/index.md", "docs-site/guide/introduction.md"]:
            self.files[name] = "运行标识 v1.1.0-dev；当前开发 v1.2-dev\n"
        for name, value in self.files.items():
            self.write(name, value)

    def write(self, name, value):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(value, encoding="utf-8")

    def check(self):
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            return VERSION_SYNC.main()

    def test_runtime_can_differ_from_current_development_plan(self):
        self.assertEqual(self.check(), 0)

    def test_old_development_stage_on_any_entry_surface_is_rejected(self):
        for name in ["README.md", "docs/README.md", "docs-site/index.md", "docs-site/guide/introduction.md"]:
            with self.subTest(path=name):
                self.write(name, "v1.1.0-dev / v1.1-dev")
                self.assertEqual(self.check(), 1)
                self.write(name, self.files[name])

    def test_runtime_package_sdk_openapi_and_deployment_drift_still_fail(self):
        for name in ["docs-site/package.json", "sdk/typescript/package.json", "sdk/go/hostapi.go", "docs/api/openapi-v0.6-current.yaml", "deploy/docker/.env.example", "deploy/docker/Dockerfile", "compose.deploy.yml", "deploy/docker/components/compose.api.yml"]:
            with self.subTest(path=name):
                self.write(name, self.files[name].replace("1.1", "9.9"))
                self.assertEqual(self.check(), 1)
                self.write(name, self.files[name])

    def test_wrong_runtime_on_documentation_is_rejected(self):
        self.write("docs-site/index.md", "v9.9.0-dev / v1.2-dev")
        self.assertEqual(self.check(), 1)

    def test_development_stage_must_match_selected_plan_heading(self):
        self.write("docs/plan.md", "# CampusOS v1.3 版本计划\n")
        self.assertEqual(self.check(), 1)

    def test_missing_invalid_or_foreign_plan_is_rejected(self):
        for plan in ["docs/missing.md", "../foreign.md"]:
            with self.subTest(plan=plan):
                status = json.loads(self.files["config/project-status.json"])
                status["plan"] = plan
                self.write("config/project-status.json", json.dumps(status))
                self.assertEqual(self.check(), 1)

    def test_invalid_project_status_is_rejected(self):
        for status in [{}, [], {"schema": "campusos.project-status/v1", "development_stage": "v1.2.0-dev"}]:
            with self.subTest(status=status):
                self.write("config/project-status.json", json.dumps(status))
                self.assertEqual(self.check(), 1)


if __name__ == "__main__":
    unittest.main()
