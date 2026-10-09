#!/usr/bin/env python3
"""Current Admin admission baseline; this does not certify V12-02d isolation.

Only run through the disposable PostgreSQL/HTTP G0 drill. No credential,
Token, response body or audit reason is written into the evidence report.
"""

import json
import os
import threading
import urllib.parse
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from pathlib import Path

from v12_g0_http import Client, require


ADMISSIONS = "/api/v1/identity/admin-accounts"
BOOTSTRAP_ID = "1000000000000000001"
TARGET_ID = "2001"


def main():
    base = os.environ["V12_G0_BASE_URL"]
    url = urllib.parse.urlsplit(base)
    require(url.scheme == "http" and url.hostname == "127.0.0.1" and url.port,
            "G0 Admin drill requires an isolated loopback API")
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
            require(payload.get("error", {}).get("code") == error,
                    f"{name}: unexpected error code")
            result["error_code"] = error
        checks[name] = result
        return payload.get("data")

    def login(name, client, email, password, user_id, admin=False):
        path = "/api/v1/auth/admin/login" if admin else "/api/v1/auth/login"
        data = check(name, client, "POST", path,
                     body={"email": email, "password": password})
        require(str(data["user"]["id"]) == user_id, f"{name}: wrong identity")
        require(data.get("access_token") and not data.get("refresh_token"),
                f"{name}: unexpected token transport")
        client.token = data["access_token"]
        require(client.cookies.get("campusos_refresh")
                and client.cookies["campusos_refresh"]["httponly"]
                and client.cookies.get("campusos_csrf"), f"{name}: cookies missing")
        checks[name]["user_id"] = user_id

    anonymous, user_a, user_b, bootstrap, target = [Client(base) for _ in range(5)]
    a_email = "g0-owner-a@example.invalid"
    a_password = os.environ["V12_G0_OWNER_A_PASSWORD"]
    login("user_a_login", user_a, a_email, a_password, TARGET_ID)
    login("user_b_login", user_b, "g0-owner-b@example.invalid",
          os.environ["V12_G0_OWNER_B_PASSWORD"], "2002")
    check("anonymous_admin_read", anonymous, "GET", ADMISSIONS,
          status=401, error="auth.required")
    check("ordinary_user_admin_login", anonymous, "POST", "/api/v1/auth/admin/login",
          status=401, error="auth.required", body={"email": a_email, "password": a_password})
    check("ordinary_user_admin_read", user_a, "GET", ADMISSIONS,
          status=403, error="permission.denied")
    check("ordinary_user_admin_suspend", user_b, "POST", ADMISSIONS + f"/{BOOTSTRAP_ID}/suspend",
          status=403, error="permission.denied", body={"expected_version": 1, "reason": "G0 denied"})
    login("bootstrap_admin_login", bootstrap, "admin@campusos.local",
          os.environ["V12_G0_BOOTSTRAP_PASSWORD"], BOOTSTRAP_ID, admin=True)
    data = check("initial_admin_list", bootstrap, "GET", ADMISSIONS)
    require(data["pagination"]["total"] == 1
            and {item["account"]["user_id"] for item in data["items"]} == {BOOTSTRAP_ID},
            "initial_admin_list: unexpected fixture admissions")

    # Current role assignment auto-creates admission. This is measured current
    # coupling slated for removal in V12-02d, not a new authorization contract.
    data = check("assign_fixture_admin", bootstrap, "POST", f"/api/v1/users/{TARGET_ID}/roles",
                 body={"role_id": 1})
    require(data.get("assigned") is True, "assign_fixture_admin: role was not assigned")
    account = check("target_admission_active", bootstrap, "GET", ADMISSIONS + f"/{TARGET_ID}")["account"]
    require(account["status"] == "active" and account["user_id"] == TARGET_ID,
            "target_admission_active: wrong state")
    login("target_admin_login", target, a_email, a_password, TARGET_ID, admin=True)
    check("target_admin_read", target, "GET", ADMISSIONS)
    check("baseline_user_session_admitted_after_role_grant", user_a, "GET", ADMISSIONS)
    check("ordinary_user_still_denied", user_b, "GET", ADMISSIONS,
          status=403, error="permission.denied")

    policy = check("read_mfa_policy", bootstrap, "GET", "/api/v1/identity/mfa-policy")
    require(policy["policy"]["mode"] == "off", "unexpected fixture MFA policy")
    version = account["version"]
    saved_target_cookies = target.cookies.copy()
    suspended = check("suspend_target", bootstrap, "POST", ADMISSIONS + f"/{TARGET_ID}/suspend",
                      body={"expected_version": version, "reason": "G0 admission suspend"})["account"]
    require(suspended["status"] == "suspended" and suspended["version"] == version + 1
            and suspended["status_changed_by"] == BOOTSTRAP_ID, "suspend_target: state mismatch")
    check("suspended_admin_login", anonymous, "POST", "/api/v1/auth/admin/login",
          status=401, error="auth.required", body={"email": a_email, "password": a_password})
    check("suspended_old_admin_access", target, "GET", ADMISSIONS,
          status=401, error="auth.invalid_token")
    check("suspended_old_user_access", user_a, "GET", "/api/v1/threads/3003/me",
          status=401, error="auth.invalid_token")
    old_refresh = Client(base)
    old_refresh.cookies = saved_target_cookies.copy()
    check("suspended_old_refresh", old_refresh, "POST", "/api/v1/auth/refresh",
          csrf=True, status=401, error="auth.invalid_token")
    check("stale_suspend_rejected", bootstrap, "POST", ADMISSIONS + f"/{TARGET_ID}/suspend",
          status=409, error="identity.admin_admission.version_conflict",
          body={"expected_version": version, "reason": "G0 stale suspend"})

    # User login remains available with suspended Admin admission in the current
    # shared identity model. It must still fail the management-plane gate.
    suspended_user = Client(base)
    login("suspended_subject_user_login", suspended_user, a_email, a_password, TARGET_ID)
    check("suspended_new_user_session_admin_denied", suspended_user, "GET", ADMISSIONS,
          status=403, error="permission.denied")
    check("suspended_subject_own_private", suspended_user, "GET", "/api/v1/threads/3003/me")
    check("ordinary_user_unaffected", user_b, "GET", "/api/v1/threads/3006/me")

    bootstrap_account = check("last_admin_before", bootstrap, "GET", ADMISSIONS + f"/{BOOTSTRAP_ID}")["account"]
    check("last_admin_suspend_rejected", bootstrap, "POST", ADMISSIONS + f"/{BOOTSTRAP_ID}/suspend",
          status=409, error="identity.admin_admission.last_active",
          body={"expected_version": bootstrap_account["version"], "reason": "G0 last admin"})
    after = check("last_admin_after", bootstrap, "GET", ADMISSIONS + f"/{BOOTSTRAP_ID}")["account"]
    require(after["status"] == "active" and after["version"] == bootstrap_account["version"],
            "last_admin_after: rejected command changed state")
    check("stale_restore_rejected", bootstrap, "POST", ADMISSIONS + f"/{TARGET_ID}/restore",
          status=409, error="identity.admin_admission.version_conflict",
          body={"expected_version": version, "reason": "G0 stale restore"})
    restored = check("restore_target", bootstrap, "POST", ADMISSIONS + f"/{TARGET_ID}/restore",
                     body={"expected_version": suspended["version"], "reason": "G0 admission restore"})["account"]
    require(restored["status"] == "active" and restored["version"] == version + 2,
            "restore_target: state mismatch")
    check("restored_old_access_still_invalid", target, "GET", ADMISSIONS,
          status=401, error="auth.invalid_token")
    old_refresh.cookies = saved_target_cookies.copy()
    check("restored_old_refresh_still_invalid", old_refresh, "POST", "/api/v1/auth/refresh",
          csrf=True, status=401, error="auth.invalid_token")
    login("restored_admin_login", target, a_email, a_password, TARGET_ID, admin=True)
    check("restored_admin_access", target, "GET", ADMISSIONS)
    data = check("admission_audits", bootstrap, "GET", ADMISSIONS + "/audits")
    # Middleware allow decisions are not successful mutation evidence. Only
    # service audits carry this resource identity and the exact fixture reason.
    mutations = [item for item in data["items"]
                 if item.get("resource_type") == "identity_admin_account"]
    for action in ("suspend", "restore"):
        matches = [item for item in mutations
                   if item.get("resource_id") == TARGET_ID and item.get("actor_id") == BOOTSTRAP_ID
                   and item.get("permission_code") == f"identity.admin_account.{action}"
                   and item.get("reason") == f"G0 admission {action}" and item.get("outcome") == "allow"]
        require(len(matches) == 1, f"admission_audits: missing/duplicate {action} evidence")
    require(len(mutations) == 2, "admission_audits: failed mutation wrote a success audit")
    checks["admission_audits"]["successful_mutations"] = len(mutations)

    # Both requests start together; depending on scheduling, the losing request
    # can fail at session/admission validation or at the last-admin aggregate.
    barrier = threading.Barrier(2)
    contenders = [(bootstrap, BOOTSTRAP_ID, TARGET_ID, restored["version"]),
                  (target, TARGET_ID, BOOTSTRAP_ID, after["version"])]

    def race_suspend(contender):
        client, actor_id, subject_id, expected_version = contender
        barrier.wait(timeout=5)
        status, payload = client.request("POST", ADMISSIONS + f"/{subject_id}/suspend",
                                         body={"expected_version": expected_version,
                                               "reason": "G0 concurrent suspend"})
        return contender, status, payload

    with ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(race_suspend, contenders))
    winners = []
    for contender, status, payload in results:
        client, actor_id, subject_id, expected_version = contender
        if status == 200:
            require(payload.get("code") == 0 and payload["data"]["account"]["status"] == "suspended"
                    and payload["data"]["account"]["user_id"] == subject_id
                    and payload["data"]["account"]["version"] == expected_version + 1,
                    "concurrent_suspend: success state mismatch")
            winners.append((client, actor_id, subject_id))
        else:
            expected_error = {401: "auth.invalid_token", 403: "permission.denied",
                              409: "identity.admin_admission.last_active"}.get(status)
            require(expected_error and payload.get("error", {}).get("code") == expected_error
                    and payload.get("data") is None,
                    f"concurrent_suspend: unexpected HTTP {status}/error")
        checks[f"concurrent_suspend_by_{actor_id}"] = {"http_status": status}
        if status != 200:
            checks[f"concurrent_suspend_by_{actor_id}"]["error_code"] = expected_error
    require(len(winners) == 1, "concurrent_suspend: exactly one command must commit")
    survivor, survivor_id, paused_id = winners[0]
    data = check("concurrent_admin_state", survivor, "GET", ADMISSIONS)
    states = {item["account"]["user_id"]: item["account"] for item in data["items"]}
    require(set(states) == {BOOTSTRAP_ID, TARGET_ID}
            and states[survivor_id]["status"] == "active"
            and states[paused_id]["status"] == "suspended", "concurrent_admin_state: invalid final state")
    checks["concurrent_admin_state"]["active_count"] = 1
    paused_client = target if paused_id == TARGET_ID else bootstrap
    check("concurrent_paused_session_invalid", paused_client, "GET", ADMISSIONS,
          status=401, error="auth.invalid_token")
    restored_after_race = check("concurrent_restore", survivor, "POST", ADMISSIONS + f"/{paused_id}/restore",
                               body={"expected_version": states[paused_id]["version"],
                                     "reason": "G0 concurrent restore"})["account"]
    require(restored_after_race["status"] == "active", "concurrent_restore: wrong state")
    data = check("final_admin_state", survivor, "GET", ADMISSIONS)
    require(data["pagination"]["total"] == 2
            and all(item["account"]["status"] == "active" for item in data["items"]),
            "final_admin_state: admissions did not recover")
    checks["final_admin_state"]["active_count"] = 2
    data = check("final_admission_audits", survivor, "GET", ADMISSIONS + "/audits")
    mutations = [item for item in data["items"]
                 if item.get("resource_type") == "identity_admin_account"]
    require(len(mutations) == 4, "final_admission_audits: unexpected committed mutations")
    for action in ("suspend", "restore"):
        matches = [item for item in mutations
                   if item.get("resource_id") == paused_id and item.get("actor_id") == survivor_id
                   and item.get("permission_code") == f"identity.admin_account.{action}"
                   and item.get("reason") == f"G0 concurrent {action}" and item.get("outcome") == "allow"]
        require(len(matches) == 1, f"final_admission_audits: unexpected concurrent {action} audit")
    checks["final_admission_audits"]["successful_mutations"] = len(mutations)

    report = json.loads(Path(os.environ["V12_G0_PUBLIC_EVIDENCE"]).read_text())
    report["schema"] = "campusos.v12-g0-admin-admission/v1"
    report["generated_at"] = datetime.now(timezone.utc).isoformat()
    report["scope"] = "current_admin_admission_suspend_restore_baseline"
    report["mfa_policy_mode"] = policy["policy"]["mode"]
    report["fixture"].update({"credentialed_users": 2, "bootstrap_admins": 1,
                              "role_assigned_admins": 1})
    report["public_visibility_checks"] = report.pop("checks")
    report["checks"] = checks
    report["admission_versions"] = {"before": version, "suspended": suspended["version"], "restored": restored["version"]}
    report["known_identity_domain_gaps"] = [
        "admin_credential_account_references_user_account",
        "admin_role_assignment_automatically_creates_admission",
        "existing_user_session_can_enter_admin_after_role_grant",
    ]
    output = Path(os.environ["V12_G0_OUTPUT"])
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
    print(f"G0 current Admin admission checks passed: {len(checks)}; evidence={output}")


if __name__ == "__main__":
    main()
