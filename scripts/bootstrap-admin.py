#!/usr/bin/env python3
"""Interactively initialize the first independent Admin through loopback HTTP."""
from __future__ import annotations

import argparse
import getpass
import json
import os
from pathlib import Path
import re
import stat
import sys
import urllib.error
import urllib.request
import warnings


class BootstrapError(Exception):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def hidden_input(prompt):
    with warnings.catch_warnings():
        warnings.simplefilter("error", getpass.GetPassWarning)
        try:
            return getpass.getpass(prompt)
        except getpass.GetPassWarning:
            raise BootstrapError("终端无法隐藏输入，已停止；请在交互终端执行，SSH使用ssh -t") from None


def validate_proof(value):
    if not isinstance(value, str):
        raise BootstrapError("管理域初始化凭证无效")
    value = value.strip()
    if not 16 <= len(value) <= 4096 or not value.isascii() or any(ord(char) < 33 or ord(char) > 126 for char in value):
        raise BootstrapError("初始化凭证须为16–4096字节的无空白ASCII字符串；建议使用随机生成值")
    return value


def runtime_proof(file):
    descriptor = None
    try:
        descriptor = os.open(file, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or info.st_size > 65536:
            raise BootstrapError("运行配置须为本人私有文件（chmod 600），且不超过64KiB")
        with os.fdopen(descriptor, "r", encoding="utf-8") as handle:
            descriptor = None
            data = json.load(handle)
        return validate_proof(data["environment"]["AUTH_ADMIN_BOOTSTRAP_SECRET"])
    except BootstrapError:
        raise
    except (OSError, ValueError, KeyError, TypeError):
        raise BootstrapError("无法读取有效的管理域初始化凭证，请核对实际生效的API运行配置") from None
    finally:
        if descriptor is not None:
            os.close(descriptor)


def create_admin(port, proof, username, email, password):
    if type(port) is not int or not 1 <= port <= 65535:
        raise BootstrapError("API端口无效")
    proof = validate_proof(proof)
    if not re.fullmatch(r"[a-z0-9][a-z0-9._-]{1,63}", username) or not email or not password:
        raise BootstrapError("用户名格式无效，或邮箱/密码为空")
    body = json.dumps({"username": username, "email": email, "password": password,
                       "reason": "部署者首次初始化独立管理域"}, ensure_ascii=False).encode("utf-8")
    if len(body) > 4096:
        raise BootstrapError("初始化请求超过4096字节，请缩短输入")
    request = urllib.request.Request(
        f"http://127.0.0.1:{port}/api/v1/admin/identity/bootstrap", data=body,
        headers={"Content-Type": "application/json", "X-CampusOS-Bootstrap-Secret": proof}, method="POST")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    try:
        with opener.open(request, timeout=15) as response:
            raw = response.read(65537)
            if response.status != 200 or len(raw) > 65536:
                raise BootstrapError("初始化响应无效，请先核对账号是否已创建，不要盲目重试")
            result = json.loads(raw)
    except urllib.error.HTTPError as error:
        status = error.code
        error.close()
        raise BootstrapError(f"初始化未成功（HTTP {status}）。请核对凭证是否生效、输入格式及是否已有安全管理员") from None
    except (OSError, ValueError):
        raise BootstrapError("连接失败或结果不确定，请先检查API及账号结果，不要盲目重复初始化") from None
    if not isinstance(result, dict) or type(result.get("code")) is not int or result["code"] != 0:
        raise BootstrapError("初始化未确认成功，请先核对账号状态，勿重复提交")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runtime-profile", type=Path, help="读取本机API私有运行JSON中的独立管理域初始化凭证")
    parser.add_argument("--port", type=int, default=8080, help="本机API端口，默认8080；地址固定为127.0.0.1")
    args = parser.parse_args()
    try:
        if not sys.stdin.isatty():
            raise BootstrapError("请在交互终端执行；通过SSH时使用ssh -t，不要通过管道传入密码")
        if args.runtime_profile:
            proof = runtime_proof(args.runtime_profile)
            print("已读取本机API运行配置中的管理域初始化凭证（不显示）。")
        else:
            proof = validate_proof(hidden_input("输入AUTH_ADMIN_BOOTSTRAP_SECRET（不回显）："))
        username = input("管理员用户名（例如 operator）：").strip()
        email = input("管理员邮箱：").strip()
        password = hidden_input("为新管理员设置登录密码（不回显）：")
        if not password or password != hidden_input("再次输入登录密码（不回显）："):
            raise BootstrapError("两次密码不一致或为空，未发送初始化请求")
        create_admin(args.port, proof, username, email, password)
        print("管理员创建成功。请清空实际生效配置中的AUTH_ADMIN_BOOTSTRAP_SECRET并重启对应API。")
        print("随后在管理端使用刚设置的用户名/密码登录，并按引导配置MFA。")
        return 0
    except (BootstrapError, EOFError, KeyboardInterrupt) as error:
        message = str(error) if isinstance(error, BootstrapError) else "操作已取消，未确认初始化成功"
        print(message, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
