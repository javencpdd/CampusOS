"""Exercise the migration entrypoint with a clock rollback and a strict SQL boundary stub."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import textwrap
import unittest


ROOT = Path(__file__).resolve().parents[1]

PSQL = r"""#!/usr/bin/env python3
import json, os, pathlib, re, sys
state_file = pathlib.Path(os.environ['CAMPUSOS_TIMING_TEST_STATE'])
state = json.loads(state_file.read_text()) if state_file.exists() else {'applied': 0}
args = sys.argv[1:]
if '-f' in args:
    sql = pathlib.Path(args[args.index('-f') + 1]).read_text()
    assert 'CREATE TABLE timing_probe' in sql and 'COMMIT;' in sql
    row = re.search(r"VALUES \('([0-9]+)','([^']+)','([0-9a-f]+)',0,", sql)
    assert row, sql
    state.update(version=row[1], name=row[2], checksum=row[3], execution_ms=0, applied=state['applied'] + 1)
    state_file.write_text(json.dumps(state))
    raise SystemExit(0)
sql = next((args[i + 1] for i, a in enumerate(args[:-1]) if a in ('-c', '-tAc', '-qAtc')), '')
if 'information_schema.columns' in sql:
    print('t')
elif 'INSERT INTO public.schema_migration_locks' in sql:
    print(re.search(r"VALUES \(1, '([^']+)'\)", sql)[1])
elif 'SELECT checksum FROM' in sql:
    print(state.get('checksum', ''))
elif 'SELECT name FROM' in sql:
    print(state.get('name', ''))
elif 'UPDATE public.schema_migrations SET execution_ms=' in sql:
    elapsed = int(re.search(r'execution_ms=(-?[0-9]+)', sql)[1])
    if elapsed < 0:
        sys.stderr.write('schema_migrations_execution_ms_check: negative execution_ms=' + str(elapsed) + '\n')
        raise SystemExit(1)
    state['execution_ms'] = elapsed
    state_file.write_text(json.dumps(state))
elif 'CREATE TABLE IF NOT EXISTS public.schema_migrations' not in sql and 'DELETE FROM public.schema_migration_locks' not in sql:
    raise AssertionError('Unexpected SQL boundary call: ' + sql)
"""

DATE = r"""#!/usr/bin/env python3
import os, pathlib, sys
counter = pathlib.Path(os.environ['CAMPUSOS_TIMING_TEST_DATE'])
calls = int(counter.read_text()) if counter.exists() else 0
counter.write_text(str(calls + 1))
# Reproduce the reported -336 ms without changing the machine's system clock.
if sys.argv[1:] == ['+%s%3N']:
    print(2000 - 336 * calls)
elif sys.argv[1:] == ['+%s']:
    print(2 - calls)
else:
    raise AssertionError('Unexpected date arguments')
"""


@unittest.skipUnless(os.name == "posix" and shutil.which("bash"), "requires Bash")
class MigrationTimingTests(unittest.TestCase):
    def test_clock_rollback_preserves_nonnegative_timing_and_repeat_up(self):
        with tempfile.TemporaryDirectory(prefix="campusos-migration-timing-") as directory:
            work = Path(directory)
            binaries = work / "bin"
            binaries.mkdir()
            migrations = work / "migrations"
            migrations.mkdir()
            (migrations / "000001_timing_probe.up.sql").write_text("CREATE TABLE timing_probe(id integer);\n")
            (migrations / "000001_timing_probe.down.sql").write_text("DROP TABLE timing_probe;\n")
            for name, source in (("psql", PSQL), ("date", DATE)):
                tool = binaries / name
                tool.write_text(textwrap.dedent(source))
                tool.chmod(0o755)
            state = work / "state.json"
            date = work / "date-calls"
            env = os.environ | {
                "PATH": str(binaries) + os.pathsep + os.environ["PATH"],
                "CAMPUSOS_SKIP_DOTENV": "true",
                "PSQL_MODE": "host",
                "DB_NAME": "timing_fixture",
                "MIGRATIONS_DIR": str(migrations),
                "CAMPUSOS_TIMING_TEST_STATE": str(state),
                "CAMPUSOS_TIMING_TEST_DATE": str(date),
            }
            for attempt in range(2):
                result = subprocess.run(
                    ["bash", "scripts/migrate.sh", "up"], cwd=ROOT, env=env,
                    capture_output=True, text=True, timeout=20,
                )
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                stored = json.loads(state.read_text())
                self.assertEqual(stored["applied"], 1)
                self.assertGreaterEqual(stored["execution_ms"], 0)
                self.assertIn("apply" if attempt == 0 else "skip", result.stdout)
            if Path("/proc/uptime").is_file():
                self.assertFalse(date.exists(), "Linux migration timing must not use wall-clock date")


if __name__ == "__main__":
    unittest.main()
