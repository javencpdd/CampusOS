#!/usr/bin/env python3
"""HTTP assertions for the disposable --authenticated G0 visibility drill.

Credentials/tokens stay in memory. Evidence contains only fixture identifiers,
status/error codes and assertion results. Run via v12-g0-public-visibility-drill.sh.
"""

import json
import os
import urllib.parse
from datetime import datetime, timezone
from pathlib import Path

from v12_g0_http import Client, require


def main():
    base = os.environ["V12_G0_BASE_URL"]
    url = urllib.parse.urlsplit(base)
    require(url.scheme == "http" and url.hostname == "127.0.0.1" and url.port,
            "G0 owner/session drill requires an isolated loopback API")
    checks = {}

    def check(name, client, method, path, status=200, error=None, **kwargs):
        actual, payload = client.request(method, path, **kwargs)
        require(actual == status, f"{name}: expected HTTP {status}, got {actual}")
        result = {"http_status": actual}
        if status == 200:
            require(payload.get("code") == 0, f"{name}: unsuccessful envelope")
        else:
            require(payload.get("code", 0) != 0, f"{name}: missing error code")
            require(payload.get("data") is None, f"{name}: denied response has data")
            machine_code = payload.get("error", {}).get("code")
            require(machine_code == error, f"{name}: unexpected error code")
            result["error_code"] = machine_code
        checks[name] = result
        return payload.get("data")

    anonymous = Client(base)
    owners = {"a": Client(base), "b": Client(base)}
    check("wrong_password", anonymous, "POST", "/api/v1/auth/login",
          status=401, error="auth.required",
          body={"email": "g0-owner-a@example.invalid",
                "password": os.environ["V12_G0_OWNER_B_PASSWORD"]})
    for label, client in owners.items():
        user_id = "2001" if label == "a" else "2002"
        data = check(f"login_{label}", client, "POST", "/api/v1/auth/login",
                     body={"email": f"g0-owner-{label}@example.invalid",
                           "password": os.environ[f"V12_G0_OWNER_{label.upper()}_PASSWORD"]})
        require(str(data["user"]["id"]) == user_id, f"login_{label}: wrong identity")
        require(data.get("access_token") and not data.get("refresh_token"),
                f"login_{label}: unexpected token transport")
        require(data.get("token_type") == "Bearer", f"login_{label}: wrong token type")
        client.token = data["access_token"]
        refresh = client.cookies.get("campusos_refresh")
        csrf_cookie = client.cookies.get("campusos_csrf")
        require(refresh and refresh.value and refresh["httponly"]
                and refresh["path"] == "/api/v1/auth"
                and refresh["samesite"].lower() == "lax"
                and csrf_cookie and csrf_cookie.value and not csrf_cookie["httponly"],
                f"login_{label}: cookie contract mismatch")
        checks[f"login_{label}"]["user_id"] = user_id
        checks[f"login_{label}"]["cookie_contract"] = "passed"
        me = check(f"me_{label}", client, "GET", "/api/v1/auth/me")
        require(str(me["id"]) == user_id, f"me_{label}: wrong identity")

    # The second private row has stale legacy status=published. Authorization
    # must still use the current state and author_id for both directions.
    for thread_id, owner, state in [
        (3003, "a", "private"), (3004, "a", "taken_down"),
        (3005, "b", "trashed"), (3006, "b", "stale_private"),
        (3007, "b", "stale_moderated"),
    ]:
        path = f"/api/v1/threads/{thread_id}/me"
        check(f"anonymous_{state}", anonymous, "GET", path,
              status=401, error="auth.required")
        data = check(f"owner_{state}", owners[owner], "GET", path)
        require(str(data["id"]) == str(thread_id)
                and str(data["author_id"]) == ("2001" if owner == "a" else "2002"),
                f"owner_{state}: wrong resource/owner")
        require(bool(data.get("content")), f"owner_{state}: missing own content")
        other = "b" if owner == "a" else "a"
        check(f"cross_owner_{state}", owners[other], "GET", path,
              status=404, error="thread.not_found")

    check("cross_owner_public", owners["b"], "GET", "/api/v1/threads/3001/me")

    # Missing CSRF must reject the command without revoking the session.
    owner_a = owners["a"]
    owner_b = owners["b"]
    check("logout_without_csrf", owner_a, "POST", "/api/v1/auth/logout",
          status=403, error="permission.denied")
    check("owner_a_after_rejected_logout", owner_a, "GET", "/api/v1/threads/3003/me")

    # Save pre-logout bearer/cookies to replay them after the server has revoked
    # the persisted session; cookie clearing alone would not prove revocation.
    revoked = Client(base)
    revoked.token = owner_a.token
    revoked.cookies = owner_a.cookies.copy()
    data = check("logout_a", owner_a, "POST", "/api/v1/auth/logout", csrf=True)
    require(data.get("revoked") is True, "logout_a: missing revocation acknowledgement")
    require(all(not owner_a.cookies[name].value
                and owner_a.cookies[name]["max-age"] == "0"
                for name in ("campusos_refresh", "campusos_csrf")),
            "logout_a: session cookies were not expired")
    checks["logout_a"]["cookies_cleared"] = True
    check("revoked_access_me", revoked, "GET", "/api/v1/auth/me",
          status=401, error="auth.invalid_token")
    check("revoked_access_private", revoked, "GET", "/api/v1/threads/3003/me",
          status=401, error="auth.invalid_token")
    check("revoked_refresh", revoked, "POST", "/api/v1/auth/refresh",
          csrf=True, status=401, error="auth.invalid_token")

    check("owner_b_after_a_logout", owner_b, "GET", "/api/v1/threads/3006/me")
    check("refresh_b_without_csrf", owner_b, "POST", "/api/v1/auth/refresh",
          status=403, error="permission.denied")
    previous_b = Client(base)
    previous_b.token = owner_b.token
    previous_refresh = owner_b.cookies["campusos_refresh"].value
    data = check("refresh_b", owner_b, "POST", "/api/v1/auth/refresh", csrf=True)
    require(data.get("access_token") and not data.get("refresh_token")
            and data["access_token"] != owner_b.token
            and owner_b.cookies["campusos_refresh"].value != previous_refresh,
            "refresh_b: tokens did not rotate")
    owner_b.token = data["access_token"]
    check("owner_b_after_refresh", owner_b, "GET", "/api/v1/threads/3006/me")
    check("rotated_access_b", previous_b, "GET", "/api/v1/threads/3006/me",
          status=401, error="auth.invalid_token")

    report = json.loads(Path(os.environ["V12_G0_PUBLIC_EVIDENCE"]).read_text())
    report["schema"] = "campusos.v12-g0-owner-session/v1"
    report["generated_at"] = datetime.now(timezone.utc).isoformat()
    report["fixture"]["authenticated_owners"] = 2
    report["fixture"]["credential_source"] = "disposable_verified_accounts_bcrypt"
    report["public_visibility_checks"] = report.pop("checks")
    report["checks"] = checks
    report["scope"] = "two_user_login_owner_visibility_logout_refresh"
    output = Path(os.environ["V12_G0_OUTPUT"])
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
    print(f"G0 authenticated owner checks passed: {len(checks)}; evidence={output}")


if __name__ == "__main__":
    main()
