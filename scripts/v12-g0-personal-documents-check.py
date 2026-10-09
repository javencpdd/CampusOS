#!/usr/bin/env python3
"""Private TXT document acceptance for the disposable G0 PostgreSQL drill.

Only synthetic counts, checksums and status codes enter evidence. Run with
v12-g0-public-visibility-drill.sh --personal-documents, never against user data.
"""

import hashlib
import json
import os
import threading
import urllib.parse
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from pathlib import Path

from v12_g0_http import Client, require


DOCUMENTS = "/api/v1/documents"
NOT_FOUND = "personal_document.not_found"


def main():
    base = os.environ["V12_G0_BASE_URL"]
    url = urllib.parse.urlsplit(base)
    require(url.scheme == "http" and url.hostname == "127.0.0.1" and url.port,
            "G0 personal document drill requires an isolated loopback API")
    checks = {}
    bodies = {"a": "G0 private document A\n校园 A\n", "b": "G0 private document B\n校园 B\n"}

    def safe_payload(payload, name, denied=False):
        raw = json.dumps(payload, ensure_ascii=False)
        for forbidden in ('"storage_key"', '"file_path"', '"absolute_path"',
                          'data/personal-space', os.environ["V12_G0_STORAGE_ROOT"]):
            require(forbidden not in raw, f"{name}: internal storage path exposed")
        if denied:
            require(not any(content.splitlines()[0] in raw for content in bodies.values()),
                    f"{name}: denied response exposed document text")

    def check(name, client, method, path, status=200, error=None, **kwargs):
        actual, payload = client.request(method, path, **kwargs)
        require(actual == status, f"{name}: expected HTTP {status}, got {actual}")
        result = {"http_status": actual}
        if status in (200, 201):
            require(payload.get("code") == 0, f"{name}: unsuccessful envelope")
        else:
            require(payload.get("code", 0) != 0 and payload.get("data") is None,
                    f"{name}: denied response has data or no error")
            require(payload.get("error", {}).get("code") == error,
                    f"{name}: unexpected error code")
            result["error_code"] = error
        safe_payload(payload, name, status >= 400)
        checks[name] = result
        return payload.get("data")

    def login(name, client, email, password, user_id, admin=False):
        data = check(name, client, "POST", "/api/v1/auth/admin/login" if admin else "/api/v1/auth/login",
                     body={"email": email, "password": password})
        require(data["user"]["id"] == user_id and data.get("access_token"), f"{name}: wrong identity")
        client.token = data["access_token"]

    def download(name, client, path, expected):
        status, headers, raw = client.request_bytes("GET", path)
        require(status == 200 and raw == expected.encode(), f"{name}: download status/bytes mismatch")
        require(headers.get("Cache-Control") == "private, no-store"
                and headers.get("X-Content-Type-Options") == "nosniff"
                and headers.get("Content-Disposition", "").startswith("attachment;")
                and headers.get("Content-Type", "").startswith("text/plain")
                and int(headers.get("Content-Length", -1)) == len(raw), f"{name}: download headers mismatch")
        checks[name] = {"http_status": status, "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest(),
                        "private_download_headers": "passed"}

    anonymous, admin = Client(base), Client(base)
    owners = {"a": Client(base), "b": Client(base)}
    documents = {}
    for label, client in owners.items():
        owner_id = "2001" if label == "a" else "2002"
        login(f"login_{label}", client, f"g0-owner-{label}@example.invalid",
              os.environ[f"V12_G0_OWNER_{label.upper()}_PASSWORD"], owner_id)
        data = check(f"create_{label}", client, "POST", DOCUMENTS, status=201,
                     body={"name": f"g0-private-{label}.txt", "format": "text", "content": bodies[label]})
        require(data["owner_id"] == owner_id and data["status"] == "active" and data["version"] == 1,
                f"create_{label}: wrong owner/state")
        version = data["current_version"]
        require(version["size_bytes"] == len(bodies[label].encode())
                and version["sha256"] == hashlib.sha256(bodies[label].encode()).hexdigest(),
                f"create_{label}: immutable object metadata mismatch")
        documents[label] = data

    login("admin_login", admin, "admin@campusos.local", os.environ["V12_G0_BOOTSTRAP_PASSWORD"],
          "1000000000000000001", admin=True)
    check("anonymous_list", anonymous, "GET", DOCUMENTS, status=401, error="auth.required")
    data = check("admin_own_list", admin, "GET", DOCUMENTS)
    require(data["items"] == [], "admin_own_list: another owner's document exposed")

    for label, client in owners.items():
        other = owners["b" if label == "a" else "a"]
        document = documents[label]
        path = DOCUMENTS + "/" + document["id"]
        listed = check(f"list_{label}", client, "GET", DOCUMENTS + "?status=active")
        require({item["id"] for item in listed["items"]} == {document["id"]},
                f"list_{label}: foreign or missing document")
        detail = check(f"metadata_{label}", client, "GET", path)
        require(detail["owner_id"] == document["owner_id"], f"metadata_{label}: wrong owner")
        data = check(f"content_{label}", client, "GET", path + "/content")
        require(data["content"] == bodies[label], f"content_{label}: bytes changed")
        download(f"download_{label}", client, path + "/download", bodies[label])
        versions = check(f"versions_{label}", client, "GET", path + "/versions")
        require([item["id"] for item in versions["items"]] == [document["current_version_id"]],
                f"versions_{label}: unexpected versions")
        for suffix in ("", "/content", "/download", "/versions"):
            kind = suffix.strip("/") or "metadata"
            check(f"cross_owner_{kind}_{label}", other, "GET", path + suffix, status=404, error=NOT_FOUND)
            check(f"admin_{kind}_{label}", admin, "GET", path + suffix, status=404, error=NOT_FOUND)
            check(f"anonymous_{kind}_{label}", anonymous, "GET", path + suffix,
                  status=401, error="auth.required")
        check(f"cross_owner_version_download_{label}", other, "GET",
              path + "/download?version_id=" + document["current_version_id"], status=404, error=NOT_FOUND)
        foreign = documents["b" if label == "a" else "a"]["current_version_id"]
        check(f"foreign_version_on_own_document_{label}", client, "GET",
              path + "/download?version_id=" + foreign, status=404, error=NOT_FOUND)

    a, b = owners["a"], owners["b"]
    document = documents["a"]
    path = DOCUMENTS + "/" + document["id"]
    initial_version = document["version"]
    check("forged_owner_create", a, "POST", DOCUMENTS, status=400, error="personal_document.invalid",
          body={"owner_id": "2002", "name": "forged.txt", "format": "text", "content": "denied"})
    for name, client in (("cross_owner", b), ("admin", admin)):
        for action in ("trash", "restore"):
            check(f"{name}_{action}", client, "POST", path + "/" + action,
                  status=404, error=NOT_FOUND, body={"expected_version": initial_version})

    trashed = check("owner_trash", a, "POST", path + "/trash", body={"expected_version": initial_version})
    require(trashed["status"] == "trashed" and trashed["version"] == initial_version + 1
            and trashed.get("deleted_at"), "owner_trash: wrong state/version")
    data = check("active_list_after_trash", a, "GET", DOCUMENTS + "?status=active")
    require(data["items"] == [], "active_list_after_trash: trash remains active")
    data = check("trash_list", a, "GET", DOCUMENTS + "?status=trashed")
    require({item["id"] for item in data["items"]} == {document["id"]}, "trash_list: wrong owner/filter")
    # Current recycle-bin semantics preserve owner download; this is not purge.
    download("owner_trash_download", a, path + "/download", bodies["a"])
    for name, client in (("cross_owner", b), ("admin", admin)):
        check(f"{name}_trash_download", client, "GET", path + "/download", status=404, error=NOT_FOUND)
    check("stale_restore", a, "POST", path + "/restore", status=409,
          error="personal_document.version_conflict", body={"expected_version": initial_version})
    unchanged = check("after_rejected_restore", a, "GET", path)
    require(unchanged["status"] == "trashed" and unchanged["version"] == trashed["version"],
            "after_rejected_restore: rejected command changed state")
    restored = check("owner_restore", a, "POST", path + "/restore",
                     body={"expected_version": trashed["version"]})
    require(restored["status"] == "active" and restored["version"] == initial_version + 2
            and not restored.get("deleted_at") and restored["current_version_id"] == document["current_version_id"],
            "owner_restore: immutable content/version changed")
    download("restored_download", a, path + "/download", bodies["a"])
    check("cross_owner_restored_download", b, "GET", path + "/download", status=404, error=NOT_FOUND)
    data = check("empty_trash_after_restore", a, "GET", DOCUMENTS + "?status=trashed")
    require(data["items"] == [], "empty_trash_after_restore: unexpected entries")
    download("other_owner_unaffected", b, DOCUMENTS + "/" + documents["b"]["id"] + "/download", bodies["b"])

    # The same expected revision may commit only once under concurrent commands.
    barrier = threading.Barrier(2)

    def concurrent_trash(_):
        client = Client(base)
        client.token = a.token
        barrier.wait(timeout=5)
        return client.request("POST", path + "/trash", body={"expected_version": restored["version"]})

    with ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(concurrent_trash, range(2)))
    require(sorted(status for status, _ in results) == [200, 409], "concurrent_trash: expected one commit and one conflict")
    for index, (status, payload) in enumerate(results):
        safe_payload(payload, f"concurrent_trash_{index}", status != 200)
        if status == 200:
            require(payload.get("code") == 0 and payload["data"]["status"] == "trashed"
                    and payload["data"]["version"] == restored["version"] + 1,
                    "concurrent_trash: wrong committed revision")
        else:
            require(payload.get("data") is None
                    and payload.get("error", {}).get("code") == "personal_document.version_conflict",
                    "concurrent_trash: unexpected conflict")
    checks["concurrent_trash"] = {"http_statuses": sorted(status for status, _ in results), "committed": 1}
    after_race = check("concurrent_trash_state", a, "GET", path)
    require(after_race["status"] == "trashed" and after_race["version"] == restored["version"] + 1,
            "concurrent_trash_state: conflict changed revision")
    final = check("restore_after_concurrent_trash", a, "POST", path + "/restore",
                  body={"expected_version": after_race["version"]})
    require(final["status"] == "active" and final["version"] == after_race["version"] + 1
            and final["current_version_id"] == document["current_version_id"],
            "restore_after_concurrent_trash: wrong final state")
    download("final_download", a, path + "/download", bodies["a"])
    data = check("final_active_list", a, "GET", DOCUMENTS + "?status=active")
    require({item["id"] for item in data["items"]} == {document["id"]}, "final_active_list: wrong owner/filter")

    storage = Path(os.environ["V12_G0_STORAGE_ROOT"])
    for label, owner_id in (("a", "2001"), ("b", "2002")):
        files = list((storage / owner_id).rglob("*.bin"))
        require(len(files) == 1 and files[0].read_bytes() == bodies[label].encode(),
                f"local_object_{label}: missing, duplicate or mismatched physical bytes")
    report = json.loads(Path(os.environ["V12_G0_PUBLIC_EVIDENCE"]).read_text())
    report["schema"] = "campusos.v12-g0-personal-documents/v1"
    report["generated_at"] = datetime.now(timezone.utc).isoformat()
    report["scope"] = "private_txt_owner_download_trash_restore"
    report["environment"]["object_storage"] = "isolated_temporary_directory"
    report["fixture"].update({"personal_documents": 2, "document_format": "text",
                              "document_bytes": sum(len(body.encode()) for body in bodies.values())})
    report["public_visibility_checks"] = report.pop("checks")
    report["checks"] = checks
    report["document_revisions"] = {"initial": initial_version, "trashed": trashed["version"],
                                    "restored": restored["version"], "after_concurrent_trash": after_race["version"],
                                    "final": final["version"], "content_version_unchanged": True}
    report["filesystem_checks"] = {"owner_directories": 2, "objects": 2, "bytes_match": True}
    output = Path(os.environ["V12_G0_OUTPUT"])
    output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
    print(f"G0 private document HTTP checks passed: {len(checks)}")


if __name__ == "__main__":
    main()
