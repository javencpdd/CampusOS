#!/usr/bin/env python3
"""Exercise the actual shell entries using owned files and fake PostgreSQL CLIs.

These checks cover backup custody and the v3 apply fence, not a completed
database restore or authentication-generation recovery acceptance.
"""

import hashlib
import io
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tarfile
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
BACKUP = ROOT / "scripts/backup.sh"
RESTORE = ROOT / "scripts/restore.sh"
CONTAINER_RESTORE = ROOT / "deploy/docker/restore-container.sh"


class BackupSecretBoundary(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tool_temp = tempfile.TemporaryDirectory(prefix="campusos-recovery-cli-")
        cls.ctl = Path(cls.tool_temp.name) / "campusosctl"
        env = os.environ | {"GOCACHE": str(ROOT / ".cache/v12-g0/go-cache")}
        subprocess.run(["go", "build", "-o", str(cls.ctl), "./cmd/campusosctl"], cwd=ROOT, env=env, check=True)

    @classmethod
    def tearDownClass(cls):
        cls.tool_temp.cleanup()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="campusos-backup-boundary-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.fixture = self.root / "checkout"
        self.fixture.mkdir()
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.marker = self.root / "dotenv-loaded"
        self.psql_calls = self.root / "psql-calls"
        self.env = os.environ.copy()
        self.env.update(
            PATH=f"{self.bin}{os.pathsep}{os.environ['PATH']}",
            CAMPUSOS_SKIP_DOTENV="false",
            CAMPUSOSCTL=str(self.ctl),
            DB_HOST="unused-fixture-host",
            DB_NAME="process_database",
            DB_PASSWORD="process-fixture-password",
            DOTENV_MARKER=str(self.marker),
            PSQL_CALLS=str(self.psql_calls),
            FAIL_PG_DUMP="false",
            # Never inherit a real deployment's custom persistent directories.
            MODULES_DIR="modules",
            PLUGINS_DIR="data/plugins",
            PLUGIN_DATA_DIR="data/plugin_data",
            MODULE_DATA_DIR="data/module_data",
            RESOURCE_DIR="data/resources",
        )
        self.command(
            "pg_dump",
            'if [[ "${FAIL_PG_DUMP:-false}" == true ]]; then\n'
            '  printf "partial dump must never be published\\n"\n'
            '  exit 9\n'
            'fi\n'
            'printf -- "-- fixture dump for %s\\n" "$DB_NAME"\n'
            'printf "CREATE TABLE fixture_keep (id integer);\\n"\n',
        )
        self.command("psql", 'printf "%s\\n" "$@" >"$PSQL_CALLS"\n')
        self.command("date", 'printf "20261021T010203Z\\n"\n')
        (self.fixture / ".env").write_text(
            "DB_NAME=dotenv_database\n"
            "DB_PASSWORD=dotenv-password-must-not-be-archived\n"
            "PLUGIN_SECRET_KEY=deployment-key-must-not-be-archived\n"
            'printf loaded >"$DOTENV_MARKER"\n'
        )
        self.contents = {
            "migrations/000001_fixture.up.sql": b"-- fixture migration\n",
            "modules/fixture/manifest.json": b'{"name":"fixture"}\n',
            "data/resources/object.bin": b"object bytes\x00\xff\n",
            "data/config/ordinary.json": b'{"theme":"fixture","revision":7}\n',
            "data/plugin_data/v5/packages/fixture.tar": b"immutable package\x00",
            "data/plugin_data/v5/prepared/candidate.bin": b"prepared candidate\xff",
            "data/plugin_data/v5/data/user-1/private.bin": b"owner ordinary bytes\x00",
            "data/plugin_data/v5/checkpoints/point.json": b'{"revision":2}\n',
            "data/plugin_data/v5/restored-data/point/private.bin": b"rollback bytes\x00",
        }
        for name, contents in self.contents.items():
            path = self.fixture / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(contents)
            path.chmod(0o644)

    def command(self, name, body):
        path = self.bin / name
        path.write_text("#!/usr/bin/env bash\nset -euo pipefail\n" + body)
        path.chmod(0o700)

    def run_entry(self, entry, *args, env=None, success=True):
        result = subprocess.run(
            ["bash", str(entry), *map(str, args)],
            cwd=self.fixture,
            env=self.env if env is None else env,
            text=True,
            capture_output=True,
            timeout=30,
            check=False,
        )
        if success:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def backup(self, *, env=None, output=None):
        output = output or self.root / "backups"
        result = self.run_entry(BACKUP, output, env=env)
        archive = Path(result.stdout.strip())
        self.assertTrue(archive.is_file())
        return archive

    def outer_bytes(self, archive):
        with tarfile.open(archive, "r:gz") as bundle:
            return {
                member.name: bundle.extractfile(member).read()
                for member in bundle.getmembers()
                if member.isfile()
            }

    def legacy(self, archive, version):
        files = self.outer_bytes(archive)
        files["metadata.txt"] = files["metadata.txt"].replace(
            b"campusos-single-node-v3", f"campusos-single-node-v{version}".encode()
        )
        files["SHA256SUMS"] = "".join(
            f"{hashlib.sha256(files[name]).hexdigest()}  {name}\n"
            for name in ["database.sql", "files.tar.gz", "metadata.txt"]
        ).encode()
        destination = self.root / f"legacy-v{version}.tar.gz"
        with tarfile.open(destination, "w:gz") as bundle:
            for name, contents in files.items():
                info = tarfile.TarInfo(name)
                info.mode = 0o600
                info.size = len(contents)
                bundle.addfile(info, io.BytesIO(contents))
        return destination

    def test_backup_excludes_root_dotenv_and_preserves_all_v5_bytes(self):
        archive = self.backup()
        self.assertTrue(self.marker.exists(), "default trusted dotenv loading retained")
        self.assertEqual(stat.S_IMODE(archive.stat().st_mode), 0o600)
        outer = self.outer_bytes(archive)
        self.assertIn(b"format=campusos-single-node-v3\n", outer["metadata.txt"])
        self.assertIn(b"deployment_keys=separate_custody_required", outer["metadata.txt"])
        self.assertIn(b"fixture dump for dotenv_database", outer["database.sql"])
        with tarfile.open(fileobj=io.BytesIO(outer["files.tar.gz"]), mode="r:gz") as files:
            members = {
                member.name.removeprefix("./"): files.extractfile(member).read()
                for member in files.getmembers()
                if member.isfile()
            }
        self.assertNotIn(".env", members)
        self.assertEqual(members, self.contents)
        for payload in [*outer.values(), *members.values()]:
            self.assertNotIn(b"deployment-key-must-not-be-archived", payload)
            self.assertNotIn(b"dotenv-password-must-not-be-archived", payload)

    def test_same_second_backups_have_distinct_names_and_do_not_overwrite(self):
        first = self.backup()
        before = first.read_bytes()
        second = self.backup()
        self.assertNotEqual(first, second)
        self.assertIn("20261021T010203Z", first.name)
        self.assertIn("20261021T010203Z", second.name)
        self.assertEqual(first.read_bytes(), before)
        self.assertEqual(len(list(first.parent.glob("*.tar.gz"))), 2)
        self.assertFalse(list(first.parent.glob(".campusos-backup-*")))

    def test_failed_dump_never_publishes_partial_archive(self):
        output = self.root / "failed-backups"
        env = self.env | {"FAIL_PG_DUMP": "true"}
        self.run_entry(BACKUP, output, env=env, success=False)
        self.assertEqual(list(output.iterdir()), [])

    def test_link_in_data_never_publishes_an_unrestorable_archive(self):
        (self.fixture / "data/config/link").symlink_to("ordinary.json")
        output = self.root / "unsafe-backups"
        result = self.run_entry(BACKUP, output, success=False)
        self.assertIn("recovery.invalid_archive", result.stderr)
        self.assertEqual(list(output.iterdir()), [])

    def test_explicit_skip_dotenv_uses_process_configuration(self):
        archive = self.backup(env=self.env | {"CAMPUSOS_SKIP_DOTENV": "true"})
        self.assertFalse(self.marker.exists())
        self.assertIn(b"fixture dump for process_database", self.outer_bytes(archive)["database.sql"])

    def test_v3_verify_extract_have_private_files_and_unchanged_bytes(self):
        archive = self.backup()
        self.run_entry(RESTORE, "verify", archive)
        destination = self.root / "inspection"
        self.run_entry(RESTORE, "extract", archive, destination)
        self.assertEqual(stat.S_IMODE(destination.stat().st_mode), 0o700)
        for name in ["database.sql", "files.tar.gz", "metadata.txt", "SHA256SUMS"]:
            self.assertEqual(stat.S_IMODE((destination / name).stat().st_mode), 0o600, name)
        for name, contents in self.contents.items():
            path = destination / "files" / name
            self.assertEqual(path.read_bytes(), contents, name)
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600, name)
        self.assertFalse((destination / "files/.env").exists())

    def test_apply_requires_confirmation_before_dotenv_or_target_writes(self):
        archive = self.backup()
        self.marker.unlink()
        watched = self.fixture / "data/config/ordinary.json"
        watched.write_bytes(b"live data must remain unchanged")
        self.run_entry(RESTORE, "apply", archive, success=False)
        self.assertFalse(self.marker.exists())
        self.assertFalse(self.psql_calls.exists())
        self.assertEqual(watched.read_bytes(), b"live data must remain unchanged")

    def test_legacy_archives_cannot_reinstate_deployment_keys(self):
        archive = self.backup()
        for version in [1, 2]:
            with self.subTest(version=version):
                result = self.run_entry(RESTORE, "verify", self.legacy(archive, version), success=False)
                self.assertIn("recovery.invalid_archive", result.stderr)

    def test_container_entry_requires_offline_confirmation(self):
        sandbox = self.root / "restore-container-owned.sh"
        sandbox.write_text(CONTAINER_RESTORE.read_text().replace("/app", str(self.root / "owned-app")))
        self.run_entry(sandbox, self.backup(), success=False)
        self.assertFalse(self.psql_calls.exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
