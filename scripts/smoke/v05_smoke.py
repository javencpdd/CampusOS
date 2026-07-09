#!/usr/bin/env python3
"""CampusOS v0.5 smoke runner.

The default mode is read-only and can be used against a running local API:

    python3 scripts/smoke/v05_smoke.py

Set --write to exercise create/reply/richtext flows. Admin-only checks run when
admin credentials are supplied, defaulting to the local seed admin account.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Any


@dataclass
class SmokeResult:
    name: str
    ok: bool
    detail: str = ""


class CampusOSClient:
    def __init__(self, base_url: str, timeout: float) -> None:
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.token = ""
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def request(
        self,
        method: str,
        path: str,
        body: dict[str, Any] | None = None,
        token: str | None = None,
        expected: tuple[int, ...] = (200, 201, 204),
    ) -> tuple[int, dict[str, Any]]:
        url = self.base_url + path
        data = None
        headers = {"Accept": "application/json"}
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        auth_token = token if token is not None else self.token
        if auth_token:
            headers["Authorization"] = f"Bearer {auth_token}"
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with self.opener.open(req, timeout=self.timeout) as resp:
                payload = resp.read()
                parsed = json.loads(payload.decode("utf-8")) if payload else {}
                if resp.status not in expected:
                    raise RuntimeError(f"{method} {path} returned {resp.status}: {parsed}")
                return resp.status, parsed
        except urllib.error.HTTPError as err:
            payload = err.read()
            try:
                parsed = json.loads(payload.decode("utf-8")) if payload else {}
            except json.JSONDecodeError:
                parsed = {"error": payload.decode("utf-8", errors="replace")}
            if err.code in expected:
                return err.code, parsed
            raise RuntimeError(f"{method} {path} returned {err.code}: {parsed}") from err

    def login(self, email: str, password: str) -> str:
        _, payload = self.request("POST", "/auth/login", {"email": email, "password": password})
        token = payload.get("data", {}).get("access_token", "")
        if not token:
            raise RuntimeError("login response did not contain access_token")
        self.token = token
        return token


def ok(name: str, detail: str = "") -> SmokeResult:
    return SmokeResult(name, True, detail)


def fail(name: str, err: Exception) -> SmokeResult:
    return SmokeResult(name, False, str(err))


def run_step(name: str, fn) -> SmokeResult:
    try:
        return ok(name, fn() or "")
    except Exception as err:  # noqa: BLE001 - smoke output should keep moving.
        return fail(name, err)


def first_item(payload: dict[str, Any]) -> dict[str, Any]:
    data = payload.get("data")
    if isinstance(data, list) and data:
        return data[0]
    if isinstance(data, dict):
        items = data.get("items")
        if isinstance(items, list) and items:
            return items[0]
    return {}


def category_id(client: CampusOSClient) -> str:
    _, payload = client.request("GET", "/categories")
    item = first_item(payload)
    value = item.get("id")
    if not value:
        raise RuntimeError("no category available")
    return str(value)


def run_read_smoke(client: CampusOSClient, admin_email: str, admin_password: str) -> list[SmokeResult]:
    results = [
        run_step("health", lambda: client.request("GET", "/health")),
        run_step("categories", lambda: client.request("GET", "/categories")),
        run_step("threads", lambda: client.request("GET", "/threads?page=1&page_size=5")),
        run_step("homepage config", lambda: client.request("GET", "/home/config")),
        run_step("richtext status", lambda: client.request("GET", "/richtext/status")),
    ]

    admin_client = CampusOSClient(client.base_url, client.timeout)
    admin_login = run_step("admin login", lambda: admin_client.login(admin_email, admin_password))
    results.append(admin_login)
    if admin_login.ok:
        results.extend(
            [
                run_step("plugins", lambda: admin_client.request("GET", "/plugins")),
                run_step("integration overview", lambda: admin_client.request("GET", "/integrations/overview")),
                run_step("mcp tools", lambda: admin_client.request("GET", "/mcp/tools")),
                run_step("message adapters", lambda: admin_client.request("GET", "/messages/adapters")),
                run_step("platform log sources", lambda: admin_client.request("GET", "/platform/logs/sources")),
            ]
        )
    return results


def run_write_smoke(client: CampusOSClient, email_prefix: str) -> list[SmokeResult]:
    stamp = int(time.time())
    username = f"smoke{stamp}"
    email = f"{email_prefix}+{stamp}@example.test"
    password = "Smoke@123456"
    results: list[SmokeResult] = []

    def register() -> str:
        client.request(
            "POST",
            "/auth/register",
            {
                "username": username,
                "nickname": "Smoke Runner",
                "email": email,
                "password": password,
            },
        )
        return email

    results.append(run_step("register smoke user", register))
    results.append(run_step("login smoke user", lambda: client.login(email, password)))
    if not results[-1].ok:
        return results

    cat_id = category_id(client)

    created: dict[str, Any] = {}

    def create_thread() -> str:
        _, payload = client.request(
            "POST",
            "/threads",
            {
                "title": f"Smoke plain thread {stamp}",
                "content": "CampusOS v0.5 smoke plain thread",
                "category_id": cat_id,
                "tags": ["smoke"],
            },
        )
        created["thread_id"] = str(payload.get("data", {}).get("id", ""))
        if not created["thread_id"]:
            raise RuntimeError("plain thread id missing")
        return created["thread_id"]

    results.append(run_step("create plain thread", create_thread))

    def reply_thread() -> str:
        thread_id = created.get("thread_id")
        if not thread_id:
            raise RuntimeError("plain thread was not created")
        _, payload = client.request("POST", f"/threads/{thread_id}/posts", {"content": "Smoke reply"})
        floor = payload.get("data", {}).get("floor_number")
        return f"floor={floor}"

    results.append(run_step("reply with floor", reply_thread))

    rich: dict[str, Any] = {}

    def create_richtext() -> str:
        _, payload = client.request(
            "POST",
            "/richtext/articles",
            {
                "title": f"Smoke richtext article {stamp}",
                "summary": "Smoke richtext summary",
                "category_id": cat_id,
                "tags": ["smoke", "richtext"],
                "content_html": "<h2>Smoke</h2><p>Richtext article body</p>",
            },
        )
        rich["thread_id"] = str(payload.get("data", {}).get("thread_id", ""))
        if not rich["thread_id"]:
            raise RuntimeError("richtext thread id missing")
        return rich["thread_id"]

    results.append(run_step("create richtext draft", create_richtext))
    results.append(
        run_step(
            "preview richtext",
            lambda: client.request("POST", "/richtext/preview", {"content_html": "<p>Preview</p><script>x()</script>"}),
        )
    )

    def publish_richtext() -> str:
        thread_id = rich.get("thread_id")
        if not thread_id:
            raise RuntimeError("richtext draft was not created")
        client.request("POST", f"/richtext/articles/{thread_id}/publish")
        client.request("GET", f"/richtext/articles/{thread_id}")
        return thread_id

    results.append(run_step("publish richtext", publish_richtext))
    return results


def main() -> int:
    parser = argparse.ArgumentParser(description="Run CampusOS v0.5 smoke checks against a running API server.")
    parser.add_argument("--base-url", default=os.getenv("CAMPUSOS_API_URL", "http://localhost:8080/api/v1"))
    parser.add_argument("--timeout", type=float, default=float(os.getenv("CAMPUSOS_SMOKE_TIMEOUT", "8")))
    parser.add_argument("--admin-email", default=os.getenv("CAMPUSOS_ADMIN_EMAIL", "admin@campusos.local"))
    parser.add_argument("--admin-password", default=os.getenv("CAMPUSOS_ADMIN_PASSWORD", "Admin@123456"))
    parser.add_argument("--write", action="store_true", help="run write-path checks that create smoke data")
    parser.add_argument("--email-prefix", default=os.getenv("CAMPUSOS_SMOKE_EMAIL_PREFIX", "campusos-smoke"))
    args = parser.parse_args()

    client = CampusOSClient(args.base_url, args.timeout)
    results = run_read_smoke(client, args.admin_email, args.admin_password)
    if args.write:
        results.extend(run_write_smoke(CampusOSClient(args.base_url, args.timeout), args.email_prefix))

    failed = [result for result in results if not result.ok]
    for result in results:
        marker = "PASS" if result.ok else "FAIL"
        suffix = f" - {result.detail}" if result.detail else ""
        print(f"[{marker}] {result.name}{suffix}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
