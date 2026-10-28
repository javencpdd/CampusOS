import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import select
import subprocess
import sys
import tempfile
import threading
import time
import unittest
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
def api(status=200, body=None, redirect=False):
    requests = []

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            requests.append({"path": self.path, "proof": self.headers.get("X-CampusOS-Bootstrap-Secret"),
                             "body": json.loads(self.rfile.read(int(self.headers["Content-Length"])))})
            self.send_response(status)
            if redirect:
                self.send_header("Location", f"http://127.0.0.1:{self.server.server_port}/redirected")
            self.end_headers()
            self.wfile.write(body if body is not None else b'{"code":0,"data":{"id":"synthetic"}}')

        def do_GET(self):
            requests.append({"path": self.path})
            self.send_response(200)
            self.end_headers()

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
        with api() as (port, calls), patch.object(sys, "argv", [str(SCRIPT), "--port", str(port)]), \
                patch.object(sys.stdin, "isatty", return_value=True), \
                patch("builtins.input", side_effect=["operator", "operator@example.test"]), \
                patch.object(client.getpass, "getpass", side_effect=[PROOF, PASSWORD, "different"]), \
                contextlib.redirect_stderr(io.StringIO()) as stderr:
            self.assertEqual(client.main(), 1)
            self.assertEqual(calls, [])
            self.assertNotIn(PASSWORD, stderr.getvalue())

    def test_noninteractive_input_and_echo_fallback_are_rejected(self):
        with patch.object(sys, "argv", [str(SCRIPT)]), patch.object(sys.stdin, "isatty", return_value=False), \
                patch.object(client.getpass, "getpass") as hidden, contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(client.main(), 1)
            hidden.assert_not_called()

        def fallback(_prompt):
            warnings.warn("cannot hide input", client.getpass.GetPassWarning)

        with patch.object(sys, "argv", [str(SCRIPT)]), patch.object(sys.stdin, "isatty", return_value=True), \
                patch.object(client.getpass, "getpass", side_effect=fallback), contextlib.redirect_stderr(io.StringIO()) as stderr:
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
            process = subprocess.Popen([sys.executable, str(SCRIPT), "--runtime-profile", str(profile), "--port", str(port)],
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
                self.assertEqual(len(calls), 1)
                self.assertEqual(calls[0]["body"]["password"], PASSWORD)
                self.assertNotIn(PROOF.encode(), output)
                self.assertNotIn(PASSWORD.encode(), output)
                self.assertIn("管理员创建成功".encode(), output)
            finally:
                os.close(fd)
                if process.poll() is None:
                    process.kill()
                    process.wait()


if __name__ == "__main__":
    unittest.main()
