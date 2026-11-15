import contextlib
import http.client
import importlib.util
import io
import json
import os
from pathlib import Path
import select
import socket
import subprocess
import sys
import tempfile
import threading
import time
from types import SimpleNamespace
import unittest
import urllib.error
import warnings
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

SCRIPT = Path(__file__).with_name("bootstrap-admin.py")
SPEC = importlib.util.spec_from_file_location("bootstrap_admin", SCRIPT)
client = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(client)
PROOF = "synthetic-admin-proof-for-tests-only"
PASSWORD = "Synthetic$()'\"中文-password"


@contextlib.contextmanager
def api(status=200, body=None, redirect=False, health_status=200, health_body=None):
    requests = []

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            requests.append({"method": "POST", "path": self.path, "proof": self.headers.get("X-CampusOS-Bootstrap-Secret"),
                             "body": json.loads(self.rfile.read(int(self.headers["Content-Length"])))})
            self.send_response(status)
            if redirect:
                self.send_header("Location", f"http://127.0.0.1:{self.server.server_port}/redirected")
            self.end_headers()
            self.wfile.write(body if body is not None else b'{"code":0,"data":{"id":"synthetic"}}')

        def do_GET(self):
            requests.append({"method": "GET", "path": self.path, "proof": self.headers.get("X-CampusOS-Bootstrap-Secret")})
            self.send_response(health_status)
            if redirect:
                self.send_header("Location", f"http://127.0.0.1:{self.server.server_port}/redirected")
            self.end_headers()
            self.wfile.write(health_body if health_body is not None else b'{"code":0,"data":{"status":"ok"}}')

        def log_message(self, *args):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield server.server_port, requests
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


class BootstrapTests(unittest.TestCase):
    def test_proof_length_boundaries(self):
        for size in [8, 15, 16, 4096]:
            with self.subTest(size=size), api() as (port, calls):
                client.create_admin(port, "x" * size, "operator", "operator@example.test", PASSWORD)
                self.assertEqual(len(calls), 1)
                self.assertEqual(calls[0]["proof"], "x" * size)
        for proof in ["x" * 7, "x" * 4097, "1234 5678", "初始化中文凭证八字"]:
            with self.subTest(size=len(proof)), self.assertRaises(client.BootstrapError):
                client.validate_proof(proof)

    def test_health_check_bypasses_proxy_and_sends_no_proof(self):
        with api() as (port, calls), patch.dict(os.environ, {"HTTP_PROXY": "http://127.0.0.1:1", "http_proxy": "http://127.0.0.1:1"}):
            client.check_api(port)
        self.assertEqual(calls, [{"method": "GET", "path": "/api/v1/health", "proof": None}])

    def test_health_check_failure_stops_before_prompts_or_post(self):
        cases = [(503, None, False), (302, None, True), (200, b"not json", False),
                 (200, b'{"code":1}', False), (200, b'{"code":false}', False), (200, b"x" * 65537, False)]
        for status, body, redirect in cases:
            with self.subTest(status=status, body=body[:20] if body else None), \
                    api(health_status=status, health_body=body, redirect=redirect) as (port, calls), \
                    patch.object(sys, "argv", [str(SCRIPT), "--http", "--port", str(port)]), \
                    patch.object(sys.stdin, "isatty", return_value=True), \
                    patch.object(client.getpass, "getpass") as hidden, patch("builtins.input") as visible, \
                    contextlib.redirect_stderr(io.StringIO()) as stderr:
                self.assertEqual(client.main(), 1)
                hidden.assert_not_called()
                visible.assert_not_called()
                self.assertEqual(len(calls), 1)
                self.assertEqual(calls[0]["method"], "GET")
                self.assertIn("尚未发送初始化请求", stderr.getvalue())

    def test_connection_refused_is_reported_without_uncertain_result(self):
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
            with self.assertRaises(client.BootstrapError) as error:
                client.check_api(port)
            self.assertIn(str(port), str(error.exception))
            self.assertIn("尚未发送初始化请求", str(error.exception))
            with self.assertRaises(client.BootstrapError) as error:
                client.create_admin(port, PROOF, "operator", "operator@example.test", PASSWORD)
            self.assertIn("初始化请求未送达", str(error.exception))

    def test_timeout_and_disconnect_after_post_keep_uncertainty_without_retry(self):
        for failure in [urllib.error.URLError(TimeoutError("private detail")), TimeoutError("private detail"),
                        http.client.RemoteDisconnected("private detail"), http.client.IncompleteRead(b"private detail", 20)]:
            with self.subTest(failure=type(failure).__name__), patch.object(client, "local_opener") as opener:
                opener.return_value.open.side_effect = failure
                with self.assertRaises(client.BootstrapError) as error:
                    client.create_admin(8080, PROOF, "operator", "operator@example.test", PASSWORD)
                self.assertIn("不确定", str(error.exception))
                self.assertNotIn("private detail", str(error.exception))
                opener.return_value.open.assert_called_once()

    def test_loopback_post_exact_header_utf8_and_proxy_bypass(self):
        with api() as (port, calls), patch.dict(os.environ, {"HTTP_PROXY": "http://127.0.0.1:1", "http_proxy": "http://127.0.0.1:1"}):
            client.create_admin(port, PROOF, "operator", "operator@example.test", PASSWORD)
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["path"], "/api/v1/admin/identity/bootstrap")
        self.assertEqual(calls[0]["proof"], PROOF)
        self.assertEqual(calls[0]["body"]["password"], PASSWORD)
        self.assertEqual(calls[0]["body"]["reason"], "部署者首次初始化独立管理域")

    def test_denial_and_redirect_never_retry_or_echo_server_body(self):
        for status, redirect in [(403, False), (302, True)]:
            with self.subTest(status=status), api(status, (PROOF + PASSWORD).encode(), redirect) as (port, calls):
                with self.assertRaises(client.BootstrapError) as error:
                    client.create_admin(port, PROOF, "operator", "operator@example.test", PASSWORD)
                self.assertEqual(len(calls), 1)
                self.assertNotIn(PROOF, str(error.exception))
                self.assertNotIn(PASSWORD, str(error.exception))

    def test_invalid_and_oversized_success_responses_are_rejected(self):
        for body in [b"not json", b'{"code":false}', b'{"code":1}', b"x" * 65537]:
            with self.subTest(body_kind=body[:20]), api(body=body) as (port, calls):
                with self.assertRaises(client.BootstrapError):
                    client.create_admin(port, PROOF, "operator", "operator@example.test", PASSWORD)
                self.assertEqual(len(calls), 1)

    def test_validation_stops_before_network(self):
        with api() as (port, calls):
            for proof, username, password in [("short", "operator", PASSWORD), (PROOF + "\r\nX: injected", "operator", PASSWORD),
                                               (PROOF, "INVALID NAME", PASSWORD), (PROOF, "operator", "x" * 4096)]:
                with self.assertRaises(client.BootstrapError):
                    client.create_admin(port, proof, username, "operator@example.test", password)
            self.assertEqual(calls, [])

    @unittest.skipUnless(os.name == "posix", "POSIX file permission contract")
    def test_runtime_profile_is_private_and_uses_independent_field(self):
        with tempfile.TemporaryDirectory() as directory:
            profile = Path(directory) / "profile.json"
            profile.write_text(json.dumps({"environment": {"AUTH_ADMIN_BOOTSTRAP_SECRET": PROOF}}))
            profile.chmod(0o600)
            self.assertEqual(client.runtime_proof(profile), PROOF)
            link = Path(directory) / "link.json"
            link.symlink_to(profile)
            with self.assertRaises(client.BootstrapError):
                client.runtime_proof(link)
            profile.chmod(0o644)
            with self.assertRaises(client.BootstrapError):
                client.runtime_proof(profile)
            profile.chmod(0o600)
            for data in [{"environment": {"AUTH_BOOTSTRAP_ADMIN_SECRET": PROOF}}, {"environment": {"AUTH_ADMIN_BOOTSTRAP_SECRET": ""}}, []]:
                profile.write_text(json.dumps(data))
                with self.assertRaises(client.BootstrapError):
                    client.runtime_proof(profile)

    def test_password_mismatch_does_not_send(self):
        with api() as (port, calls), patch.object(sys, "argv", [str(SCRIPT), "--http", "--port", str(port)]), \
                patch.object(sys.stdin, "isatty", return_value=True), \
                patch("builtins.input", side_effect=["operator", "operator@example.test"]), \
                patch.object(client.getpass, "getpass", side_effect=[PROOF, PASSWORD, "different"]), \
                contextlib.redirect_stderr(io.StringIO()) as stderr:
            self.assertEqual(client.main(), 1)
            self.assertEqual([call["method"] for call in calls], ["GET"])
            self.assertNotIn(PASSWORD, stderr.getvalue())

    def test_noninteractive_input_and_echo_fallback_are_rejected(self):
        with patch.object(sys, "argv", [str(SCRIPT)]), patch.object(sys.stdin, "isatty", return_value=False), \
                patch.object(client.getpass, "getpass") as hidden, contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(client.main(), 1)
            hidden.assert_not_called()

        def fallback(_prompt):
            warnings.warn("cannot hide input", client.getpass.GetPassWarning)

        with patch.object(sys, "argv", [str(SCRIPT), "--http"]), patch.object(sys.stdin, "isatty", return_value=True), \
                patch.object(client, "check_api"), patch.object(client.getpass, "getpass", side_effect=fallback), \
                contextlib.redirect_stderr(io.StringIO()) as stderr:
            self.assertEqual(client.main(), 1)
            self.assertIn("无法隐藏输入", stderr.getvalue())

    @unittest.skipUnless(os.name == "posix", "Linux interactive terminal")
    def test_real_tty_prompts_hide_secrets_and_create_only_one_fixture_admin(self):
        import pty
        with tempfile.TemporaryDirectory() as directory, api() as (port, calls):
            profile = Path(directory) / "profile.json"
            profile.write_text(json.dumps({"environment": {"AUTH_ADMIN_BOOTSTRAP_SECRET": PROOF}}))
            profile.chmod(0o600)
            fd, slave = pty.openpty()
            process = subprocess.Popen([sys.executable, str(SCRIPT), "--http", "--runtime-profile", str(profile), "--port", str(port)],
                                       stdin=slave, stdout=slave, stderr=slave, start_new_session=True)
            os.close(slave)
            prompts = [("管理员用户名", "operator"), ("管理员邮箱", "operator@example.test"),
                       ("为新管理员设置登录密码", PASSWORD), ("再次输入登录密码", PASSWORD)]
            output, position = b"", 0
            deadline = time.monotonic() + 10
            try:
                while time.monotonic() < deadline:
                    if select.select([fd], [], [], 0.1)[0]:
                        try:
                            chunk = os.read(fd, 4096)
                        except OSError:
                            break
                        if not chunk:
                            break
                        output += chunk
                        if position < len(prompts) and prompts[position][0] in output.decode("utf-8", "ignore"):
                            os.write(fd, (prompts[position][1] + "\n").encode())
                            position += 1
                self.assertEqual(process.wait(timeout=3), 0)
                self.assertEqual([call["method"] for call in calls], ["GET", "POST"])
                self.assertEqual(calls[1]["body"]["password"], PASSWORD)
                self.assertNotIn(PROOF.encode(), output)
                self.assertNotIn(PASSWORD.encode(), output)
                self.assertIn("管理员创建成功".encode(), output)
            finally:
                os.close(fd)
                if process.poll() is None:
                    process.kill()
                    process.wait()


class LocalBootstrapTests(unittest.TestCase):
    def test_default_flow_needs_no_bootstrap_secret_and_sends_password_only_on_stdin(self):
        target = (["fixture-cli", "identity", "create-admin", "--stdin"], Path.cwd(), None)
        with patch.object(sys, "argv", [str(SCRIPT)]), patch.object(sys.stdin, "isatty", return_value=True), \
                patch.object(client, "local_command", return_value=target), \
                patch("builtins.input", side_effect=["operator", "operator@example.test"]), \
                patch.object(client.getpass, "getpass", side_effect=[PASSWORD, PASSWORD]) as hidden, \
                patch.object(client.subprocess, "run", return_value=SimpleNamespace(returncode=0, stdout=b'{"code":0}')) as run, \
                contextlib.redirect_stdout(io.StringIO()) as stdout:
            self.assertEqual(client.main(), 0)
            self.assertEqual(hidden.call_count, 2)
            self.assertEqual(json.loads(run.call_args.kwargs["input"])["password"], PASSWORD)
            self.assertNotIn(PASSWORD, str(run.call_args.args))
            self.assertNotIn(PASSWORD, stdout.getvalue())
            run.assert_called_once()

    def test_docker_target_uses_running_api_identity_without_reading_or_changing_proof(self):
        args = SimpleNamespace(native=False, runtime_profile=None, env_file=Path("fixture.env"))
        container = {"State": {"Running": True}, "Config": {"Env": ["CAMPUSOS_DEV_UID=1234", "CAMPUSOS_DEV_GID=4321", "AUTH_ADMIN_BOOTSTRAP_SECRET=" + PROOF]}}
        with patch.object(client.subprocess, "run", side_effect=[SimpleNamespace(stdout=b"a" * 64 + b"\n"), SimpleNamespace(stdout=json.dumps([container]).encode())]):
            command, _, environment = client.local_command(args)
        self.assertIn("1234:4321", command)
        self.assertEqual(command[-3:], ["identity", "create-admin", "--stdin"])
        self.assertNotIn(PROOF, " ".join(command))
        self.assertIsNone(environment)

    def test_local_failures_never_echo_child_output_or_retry(self):
        target = (["fixture-cli"], Path.cwd(), None)
        failures = [SimpleNamespace(returncode=1, stdout=PROOF.encode(), stderr=PASSWORD.encode()),
                    SimpleNamespace(returncode=0, stdout=b'{"code":false}'),
                    SimpleNamespace(returncode=0, stdout=b'[]')]
        for failure in failures:
            with self.subTest(failure=failure.returncode), patch.object(client.subprocess, "run", return_value=failure) as run:
                with self.assertRaises(client.BootstrapError) as error:
                    client.create_local_admin(target, "operator", "operator@example.test", PASSWORD)
                self.assertNotIn(PROOF, str(error.exception))
                self.assertNotIn(PASSWORD, str(error.exception))
                run.assert_called_once()
        with patch.object(client.subprocess, "run", side_effect=subprocess.TimeoutExpired("fixture-cli", 120)):
            with self.assertRaisesRegex(client.BootstrapError, "不确定"):
                client.create_local_admin(target, "operator", "operator@example.test", PASSWORD)


if __name__ == "__main__":
    unittest.main()
