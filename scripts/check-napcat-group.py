#!/usr/bin/env python3
"""Check an existing local NapCat; send one marked group test only by opt-in.

This operator diagnostic uses NapCat's authenticated WebUI debug API. It is not
a CampusOS v5 plugin and never changes the bot's persistent network settings.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import secrets
import sys
import urllib.error
import urllib.request


class SmokeError(Exception):
    pass


class LoginRequired(SmokeError):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def identifier(value):
    if isinstance(value, bool) or not isinstance(value, (str, int)):
        raise SmokeError("invalid group or bot identifier")
    value = str(value)
    if not value.isascii() or not value.isdecimal() or value.startswith("0") or len(value) > 20:
        raise SmokeError("invalid group or bot identifier")
    return value


def message_text(value):
    if isinstance(value, str):
        return value
    if not isinstance(value, list):
        raise SmokeError("message readback is not plain text")
    if any(not isinstance(item, dict) or item.get("type") != "text" or not isinstance(item.get("data"), dict) or not isinstance(item["data"].get("text"), str) for item in value):
        raise SmokeError("message readback contains unexpected segments")
    return "".join(item["data"]["text"] for item in value)


class NapCat:
    def __init__(self, config_dir, group, allow_send=False, opener=None):
        self.config_dir = Path(config_dir)
        self.group = identifier(group)
        self.allow_send = allow_send
        config = json.loads((self.config_dir / "webui.json").read_text())
        port = config.get("port")
        if type(port) is not int or not 1 <= port <= 65535 or not isinstance(config.get("token"), str) or not config["token"]:
            raise SmokeError("local WebUI configuration is incomplete")
        self.base = f"http://127.0.0.1:{port}"
        self.password = config["token"]
        self.credential = None
        self.opener = opener or urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def request(self, path, body):
        headers = {"Content-Type": "application/json"}
        if self.credential:
            headers["Authorization"] = "Bearer " + self.credential
        request = urllib.request.Request(self.base + path, data=json.dumps(body).encode(), headers=headers)
        try:
            with self.opener.open(request, timeout=10) as response:
                raw = response.read(262145)
                if response.status != 200 or len(raw) > 262144:
                    raise SmokeError("WebUI response rejected")
                result = json.loads(raw)
        except (OSError, ValueError, urllib.error.HTTPError):
            # Never echo a URL, credential, raw response, or provider exception.
            raise SmokeError("local WebUI request failed") from None
        if not isinstance(result, dict) or result.get("code") != 0:
            raise SmokeError("local WebUI rejected the operation")
        return result.get("data")

    def login(self):
        data = self.request("/api/auth/login", {"hash": hashlib.sha256((self.password + ".napcat").encode()).hexdigest()})
        if not isinstance(data, dict) or data.get("require2FA"):
            raise LoginRequired("WebUI requires operator authentication")
        if not isinstance(data.get("Credential"), str) or not data["Credential"]:
            raise SmokeError("WebUI did not issue a credential")
        self.credential = data["Credential"]

    def logout(self):
        if self.credential:
            try:
                self.request("/api/auth/logout", {})
            finally:
                self.credential = None
                self.password = None

    def action(self, action, params):
        allowed = {"get_login_info", "get_status", "get_group_info", "get_group_member_info", "get_msg", "send_group_msg"}
        if action not in allowed:
            raise SmokeError("action is outside the diagnostic allowlist")
        if action in {"get_group_info", "get_group_member_info", "send_group_msg"} and identifier(params.get("group_id")) != self.group:
            raise SmokeError("group differs from the explicitly selected target")
        if action == "send_group_msg" and not self.allow_send:
            raise SmokeError("sending requires --send-test")
        data = self.request("/api/Debug/call", {"action": action, "params": params})
        if isinstance(data, dict) and "retcode" in data:
            if data["retcode"] != 0 or data.get("status") != "ok":
                raise SmokeError("OneBot action failed")
            data = data.get("data")
        if not isinstance(data, dict):
            raise SmokeError("OneBot returned an unexpected object")
        return data


def run_check(client):
    status = client.request("/api/QQLogin/CheckLoginStatus", {})
    if not isinstance(status, dict) or status.get("isLogin") is not True or status.get("isOffline") is True:
        raise LoginRequired("QQ is not logged in; complete login in the existing NapCat WebUI")
    bot = identifier(client.action("get_login_info", {})["user_id"])
    state = client.action("get_status", {})
    if state.get("online") is not True or state.get("good") is not True:
        raise SmokeError("existing QQ bot is not ready")
    group = client.action("get_group_info", {"group_id": int(client.group), "no_cache": True})
    if identifier(group.get("group_id")) != client.group:
        raise SmokeError("target group verification failed")
    member = client.action("get_group_member_info", {"group_id": int(client.group), "user_id": int(bot), "no_cache": True})
    if identifier(member.get("group_id")) != client.group or identifier(member.get("user_id")) != bot:
        raise SmokeError("bot membership verification failed")
    report = {"status": "preflight_passed", "group_id": client.group, "bot_id_suffix": bot[-4:], "bot_online": True,
              "bot_in_target_group": True, "messages_sent": 0, "event_stream_verified": False,
              "CampusOS_v5_plugin_installed": False}
    if not client.allow_send:
        return report
    text = "[CampusOS 接入测试] " + secrets.token_hex(6) + "：验证本群 OneBot 发送与读回；无需回复。"
    # A single text-only send. Never retry a send with an uncertain outcome.
    sent = client.action("send_group_msg", {"group_id": int(client.group), "message": [{"type": "text", "data": {"text": text}}]})
    message_id = sent.get("message_id")
    if isinstance(message_id, bool) or not isinstance(message_id, (int, str)) or not str(message_id).lstrip("-").isascii() or not str(message_id).lstrip("-").isdecimal() or int(message_id) == 0:
        raise SmokeError("send outcome is uncertain; do not automatically retry")
    readback = client.action("get_msg", {"message_id": message_id})
    if str(readback.get("message_id")) != str(message_id) or readback.get("message_type") != "group" or identifier(readback.get("sender", {}).get("user_id")) != bot or message_text(readback.get("message")) != text:
        raise SmokeError("group message readback verification failed; do not resend")
    if "group_id" in readback and identifier(readback["group_id"]) != client.group:
        raise SmokeError("message readback group mismatch")
    report.update(status="group_send_readback_passed", messages_sent=1, message_id=str(message_id), readback_verified=True)
    return report


def config_fingerprints(directory):
    paths = [Path(directory) / "webui.json", *sorted(Path(directory).glob("onebot11_*.json"))]
    return {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in paths}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config-dir", type=Path, default=Path.home() / "qqbot/napcat/napcat/config")
    parser.add_argument("--group", required=True)
    parser.add_argument("--send-test", action="store_true", help="send exactly one marked text to the verified target group")
    parser.add_argument("--output", type=Path, help="new result file; never stores credentials or message content")
    args = parser.parse_args()
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        with args.output.open("x") as stream:
            stream.write('{"status":"in_progress"}\n')
        args.output.chmod(0o600)
    report, client, code, before = {}, None, 1, None
    try:
        before = config_fingerprints(args.config_dir)
        client = NapCat(args.config_dir, args.group, args.send_test)
        client.login()
        report = run_check(client)
        code = 0
    except LoginRequired as error:
        report = {"status": "blocked_login_required", "reason": str(error), "messages_sent": 0}
        code = 2
    except SmokeError as error:
        report = {"status": "failed", "reason": str(error), "send_retry_performed": False}
    except Exception:
        report = {"status": "failed", "reason": "diagnostic failed; raw provider responses and credentials omitted"}
    finally:
        if client:
            report.setdefault("group_id", client.group)
            try:
                client.logout()
                report["own_WebUI_credential_revoked"] = True
            except Exception:
                report["own_WebUI_credential_revoked"] = False
                code = 1
        if before is not None:
            report["persistent_config_unchanged"] = config_fingerprints(args.config_dir) == before
            if not report["persistent_config_unchanged"]:
                code = 1
        if code == 1:
            report["status"] = "failed"
        report["exit_code"] = code
        if args.output:
            args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report, ensure_ascii=False))
    return code


if __name__ == "__main__":
    sys.exit(main())
