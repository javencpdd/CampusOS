#!/usr/bin/env python3
"""Owned Linux offline backup/restore drill. Never starts an application server."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import tarfile
import time

REPO = Path(__file__).resolve().parent.parent
PREDECESSOR = "docs/项目计划书v1/项目计划v1.2/evidence/v12-08a-ecosystem-linux.json"
PREDECESSOR_SHA = "d76dc2bae5ecd1cd82fe8bbd08f2d8f411bd2622d01d3b9e63a30afd7670db0c"
SEED = "campusos_v12_08b_backup_seed"
RESTORED = "campusos_v12_08b_backup_restored"


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def source_snapshot():
    names = subprocess.check_output(
        ["git", "ls-files", "-co", "--exclude-standard", "-z"], cwd=REPO
    ).decode().split("\0")
    # Documents have a separate, final seal. Include every executable input,
    # SQL, manifest, module/lock file and static source; no cached binaries.
    return {n: digest(REPO / n) for n in sorted(set(names)) if n
            and (REPO / n).is_file() and not n.endswith(".md")
            and not n.startswith("docs/项目计划书v1/项目计划v1.2/evidence/")}


def run_target(work):
    assert os.uname().sysname == "Linux", "Linux target required"
    assert digest(REPO / PREDECESSOR) == PREDECESSOR_SHA, "dependency changed"
    work.mkdir(parents=True, exist_ok=False)
    os.chmod(work, 0o700)
    env = {k: os.environ[k] for k in ("PATH", "HOME") if k in os.environ}
    env.update(GOCACHE=str(REPO / ".cache/v12-g0/go-cache"),
               GOTOOLCHAIN="auto", GOFLAGS="-buildvcs=false",
               CAMPUSOS_SKIP_DOTENV="true", DOCKER_HOST="unix:///var/run/docker.sock")
    container = "campusos-v12-08b-backup-" + secrets.token_hex(6)
    password, key = secrets.token_hex(24), secrets.token_hex(32)
    source, extracted = work / "source", work / "extracted"
    source.mkdir(mode=0o700)
    # Real credentials intentionally live OUTSIDE the backup root, in process
    # memory. This .env is a canary for the standard accidental export path.
    canary = "v12-backup-env-" + secrets.token_hex(24)
    commands = []

    def run(name, args, *, extra=None, cwd=REPO, stdin=None, required=True):
        settings = dict(env)
        settings.update(extra or {})
        started = time.monotonic()
        with (work / (name + ".log")).open("wb") as output:
            result = subprocess.run(args, cwd=cwd, env=settings, input=stdin,
                                    stdout=output, stderr=subprocess.STDOUT)
        # Never persist connection strings, deployment keys or full envs.
        commands.append({"name": name, "exit_code": result.returncode,
                         "elapsed_seconds": round(time.monotonic() - started, 3)})
        assert not required or result.returncode == 0, name + " failed; inspect private log"
        return (work / (name + ".log")).read_text()

    def sql(name, database, text):
        return run(name, ["docker", "exec", "-i", container, "psql", "-U", "campusos",
                          "-d", database, "-XAtv", "ON_ERROR_STOP=1"], stdin=text.encode())

    def migrate(name, database, action):
        return run(name, ["bash", "scripts/migrate.sh", action], extra={
            "PSQL_MODE": "docker", "POSTGRES_CONTAINER": container,
            "DB_USER": "campusos", "DB_NAME": database, "DB_PASSWORD": password})

    started = False
    proof = {}
    try:
        (source / ".env").write_text("CAMPUSOS_SECRET_ENCRYPTION_KEYS=offline:" + key
                                     + "\nJWT_SECRET=" + canary + "\n")
        os.chmod(source / ".env", 0o600)
        shutil.copytree(REPO / "migrations", source / "migrations")
        run("build-test", ["go", "test", "-race", "-c", "-o", str(work / "plugin.test"), "./internal/plugin"])
        run("build-cli", ["go", "build", "-race", "-o", str(work / "campusosctl"), "./cmd/campusosctl"])
        frozen = source_snapshot()
        (work / "source-sha256.json").write_text(json.dumps(frozen, ensure_ascii=False, indent=2) + "\n")
        run("pg-start", ["docker", "run", "--pull=never", "--rm", "-d", "--name", container,
                         "--tmpfs", "/var/lib/postgresql/data:rw,size=512m",
                         "-e", "POSTGRES_DB=" + SEED, "-e", "POSTGRES_USER=campusos",
                         "-e", "POSTGRES_PASSWORD", "-p", "127.0.0.1::5432", "postgres:16-alpine"],
            extra={"POSTGRES_PASSWORD": password})
        started = True
        image_id = run("pg-image", ["docker", "image", "inspect", "postgres:16-alpine", "--format", "{{.Id}}"]).strip()
        for attempt in range(120):
            ready = subprocess.run(["docker", "exec", container, "pg_isready", "-h", "127.0.0.1",
                                    "-U", "campusos", "-d", SEED], env=env, capture_output=True)
            if ready.returncode == 0:
                break
            time.sleep(0.25)
        else:
            raise AssertionError("owned PostgreSQL did not become ready")
        port = run("pg-port", ["docker", "port", container, "5432/tcp"]).strip()
        assert port.startswith("127.0.0.1:")
        port = port.rsplit(":", 1)[1]
        pg_version = sql("pg-version", SEED, "SHOW server_version_num;").strip()
        assert 160000 <= int(pg_version) < 170000
        migrate("migrate-up", SEED, "up")
        migrate("migrate-seed-check", SEED, "check")

        native_proofs = {}

        def native(phase, database, root):
            result = run("native-" + phase, [str(work / "plugin.test"), "-test.v", "-test.run=^TestV12BackupRestoreOffline$"], cwd=REPO / "internal/plugin", extra={
                "CAMPUSOS_V12_BACKUP_DSN": f"postgres://campusos:{password}@127.0.0.1:{port}/{database}?sslmode=disable",
                "CAMPUSOS_V12_BACKUP_ROOT": str(root), "CAMPUSOS_V12_BACKUP_PHASE": phase,
                "CAMPUSOS_V12_BACKUP_KEY": key})
            assert "--- PASS: TestV12BackupRestoreOffline " in result
            assert "--- SKIP" not in result and "--- FAIL" not in result and "DATA RACE" not in result
            records = [json.loads(line.removeprefix("V12_BACKUP_PROOF "))
                       for line in result.splitlines() if line.startswith("V12_BACKUP_PROOF ")]
            assert len(records) == 1
            record = records[0]
            for name, expected in {"phase": phase, "database": database, "status": "passed",
                                   "objects": 2, "secret_rows": 5, "installations": 3,
                                   "configurations": 3, "checkpoints": 3, "prepared_files": 3,
                                   "original_and_independent_views": 6,
                                   "correct_external_key": "passed", "missing_external_key": "denied",
                                   "wrong_external_key": "denied", "cross_owner_object": "denied",
                                   "cross_owner_secret": "denied", "server_started": False,
                                   "runtime_started": False, "observe_called": False,
                                   "authentication_generation_rotated": False, "safe_to_reopen_traffic": False}.items():
                assert record[name] == expected, "incomplete native proof: " + name
            native_proofs[phase] = record

        native("seed", SEED, source)
        before = {str(p.relative_to(source)): digest(p) for p in sorted(source.rglob("*")) if p.is_file()}
        backup = run("backup", ["bash", str(REPO / "scripts/backup.sh"), str(work / "archives")], cwd=source,
                     extra={"POSTGRES_CONTAINER": container, "DB_USER": "campusos", "DB_NAME": SEED,
                            "DB_PASSWORD": password, "DB_HOST": "127.0.0.1", "DB_PORT": port})
        archive = Path(backup.strip().splitlines()[-1])
        assert archive.is_file() and archive.stat().st_mode & 0o777 == 0o600
        run("verify", ["bash", str(REPO / "scripts/restore.sh"), "verify", str(archive)], cwd=source)
        run("extract", ["bash", str(REPO / "scripts/restore.sh"), "extract", str(archive), str(extracted)], cwd=source)
        assert not (extracted / "files/.env").exists()
        assert "format=campusos-single-node-v3" in (extracted / "metadata.txt").read_text()
        # Search uncompressed members, including the encrypted SQL dump and
        # nested file archive; a compressed-byte search would prove nothing.
        for p in [extracted / "database.sql", extracted / "metadata.txt"]:
            data = p.read_bytes()
            assert key.encode() not in data and canary.encode() not in data
        with tarfile.open(extracted / "files.tar.gz", "r:gz") as nested:
            for item in nested:
                if item.isfile():
                    data = nested.extractfile(item).read()
                    assert key.encode() not in data and canary.encode() not in data
        restored_files = {str(p.relative_to(extracted / "files")): digest(p)
                          for p in sorted((extracted / "files").rglob("*")) if p.is_file()}
        assert restored_files == {n: sha for n, sha in before.items() if n != ".env"}, "file loss/change"
        assert before == {str(p.relative_to(source)): digest(p) for p in sorted(source.rglob("*")) if p.is_file()}
        sql("create-restored", "postgres", "CREATE DATABASE " + RESTORED + ";")
        run("restore-sql", ["docker", "exec", "-i", container, "psql", "-U", "campusos", "-d", RESTORED,
                            "-Xv", "ON_ERROR_STOP=1"], stdin=(extracted / "database.sql").read_bytes())
        migrate("migrate-restored-check", RESTORED, "check")
        versions = sql("migration-version", RESTORED, "SELECT max(version::integer),count(*) FROM schema_migrations;").strip()
        assert versions == "28|28", versions
        native("restore", RESTORED, extracted / "files")
        reconcile = run("storage-reconcile", [str(work / "campusosctl"), "storage", "reconcile", "--root",
                                               str(extracted / "files/data/personal-space"), "--dry-run"], extra={
            "DATABASE_DSN": f"postgres://campusos:{password}@127.0.0.1:{port}/{RESTORED}?sslmode=disable"})
        reconciliation = json.loads(reconcile)
        assert reconciliation["dry_run"] is True
        assert reconciliation.get("differences_truncated", False) is False
        assert reconciliation["report"]["differences"] in ([], None), "restored Object reconciliation differs"
        assert isinstance(reconciliation["report"]["counts"], dict)
        assert all(value == 0 for value in reconciliation["report"]["counts"].values())
        assert sql("restored-object-count", RESTORED, "SELECT count(*) FROM storage_objects;").strip() == "2"
        assert frozen == source_snapshot(), "source changed during target"
        proof = {"implementation_status": "passed", "automation_status": "passed", "target_status": "passed",
                 "stage": "V12-08b", "stage_status": "in_progress", "slice": "offline_backup_deployment_key_separation",
                 "environment": {"os": "Linux", "architecture": os.uname().machine, "postgres_version_num": pg_version,
                                 "postgres_storage": "owned tmpfs 512m", "postgres_image_id": image_id, "loopback_only": True,
                                 "source_database": SEED, "restored_database": RESTORED, "server_started": False},
                 "source_sha256": frozen, "dependencies": {PREDECESSOR: PREDECESSOR_SHA},
                 "binary_sha256": {str(p.relative_to(REPO)): digest(p) for p in [work / "plugin.test", work / "campusosctl"]},
                 "checks": {"native_seed": "passed", "native_restored": "passed", "archive_mode_0600": True,
                            "no_deployment_key_or_env_canary_in_uncompressed_members": True,
                            "source_files_unchanged": True, "restored_files_exact": len(restored_files),
                            "migration_checks_seed_restored": "passed", "migration_current": 28, "migration_next": 29,
                            "object_reconciliation": reconciliation, "native_proofs": native_proofs},
                 "archive_sha256": digest(archive), "commands": commands,
                 "windows": "deferred_by_user_not_run", "shared_instance_reset": False, "commit_or_push": False,
                 "remaining_for_stage": ["whole-restore authentication generation rotation and credential invalidation",
                     "current authorization review and plugin/egress quarantine before reopening traffic",
                     "archive link/type/duplicate/manifest hardening and full apply recovery protocol",
                     "Linux Docker/browser recovery scenarios and frozen capacity/recovery thresholds",
                     "Windows target acceptance (explicitly deferred)"]}
    finally:
        try:
            if started:
                run("pg-server", ["docker", "logs", container], required=False)
                run("pg-cleanup", ["docker", "stop", container])
        finally:
            # Do not retain the independent key even if Docker cleanup fails.
            (source / ".env").unlink(missing_ok=True)
    assert proof, "target incomplete"
    proof["cleanup"] = {"owned_postgres_stopped": True, "deployment_key_canary_removed": True}
    proof["commands"] = commands
    (work / "target.json").write_text(json.dumps(proof, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps({"work": str(work.relative_to(REPO)), "status": "passed",
                      "restored_files": proof["checks"]["restored_files_exact"]}))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work", required=True, help="new owned output directory below .cache/v12-08b-backup")
    args = parser.parse_args()
    selected = Path(args.work).resolve()
    assert selected.is_relative_to(REPO / ".cache/v12-08b-backup") and selected != REPO / ".cache/v12-08b-backup"
    run_target(selected)
