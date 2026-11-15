#!/usr/bin/env python3
"""Create a security administrator using the current local deployment."""
from __future__ import annotations

import argparse
import getpass
import http.client
import json
import os
from pathlib import Path
import re
import stat
import subprocess
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
    if not 8 <= len(value) <= 4096 or not value.isascii() or any(ord(char) < 33 or ord(char) > 126 for char in value):
        raise BootstrapError("初始化凭证须为8–4096个无空白ASCII字符；建议使用随机生成值")
    return value


def runtime_profile(file):
    descriptor = None
    try:
        descriptor = os.open(file, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or info.st_size > 65536:
            raise BootstrapError("运行配置须为本人私有文件（chmod 600），且不超过64KiB")
        with os.fdopen(descriptor, "r", encoding="utf-8") as handle:
            descriptor = None
            data = json.load(handle)
        environment = data["environment"]
        if not isinstance(environment, dict) or any(not isinstance(key, str) or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key) or not isinstance(value, str) for key, value in environment.items()):
            raise BootstrapError("运行配置中的environment无效")
        return environment
    except BootstrapError:
        raise
    except (OSError, ValueError, KeyError, TypeError):
        raise BootstrapError("无法读取有效的管理域初始化凭证，请核对实际生效的API运行配置") from None
    finally:
        if descriptor is not None:
            os.close(descriptor)


def runtime_proof(file):
    try:
        return validate_proof(runtime_profile(file)["AUTH_ADMIN_BOOTSTRAP_SECRET"])
    except KeyError:
        raise BootstrapError("运行配置中没有独立管理域初始化凭证") from None


def local_command(args):
    root = Path(__file__).resolve().parent.parent
    command = ["go", "run", "-buildvcs=false", "./cmd/campusosctl", "identity", "create-admin", "--stdin"]
    if args.native or args.runtime_profile:
        environment = dict(os.environ)
        if args.runtime_profile:
            environment.update(runtime_profile(args.runtime_profile))
        return command, root, environment
    env_file = args.env_file or Path(os.environ.get("CAMPUSOS_DOCKER_DEV_ENV", root / "deploy/docker/.env.dev.local"))
    compose = ["docker", "compose", "--env-file", str(env_file)]
    if os.environ.get("CAMPUSOS_DOCKER_PROJECT"):
        compose.extend(["--project-name", os.environ["CAMPUSOS_DOCKER_PROJECT"]])
    compose.extend(["-f", str(root / "compose.dev.yml")])
    try:
        result = subprocess.run(compose + ["ps", "-q", "api"], cwd=root, capture_output=True, timeout=15, check=True)
        containers = result.stdout.decode().strip().splitlines()
        if len(containers) != 1 or not re.fullmatch(r"[0-9a-f]{12,64}", containers[0]):
            raise BootstrapError("未找到当前Docker开发API；请先运行docker-dev.sh up，原生部署使用--native")
        result = subprocess.run(["docker", "inspect", containers[0]], capture_output=True, timeout=15, check=True)
        info = json.loads(result.stdout)[0]
        if not info["State"]["Running"]:
            raise BootstrapError("Docker API未运行，请先启动当前部署")
        environment = dict(value.split("=", 1) for value in info["Config"]["Env"] if "=" in value)
        uid, gid = environment.get("CAMPUSOS_DEV_UID", "1000"), environment.get("CAMPUSOS_DEV_GID", "1000")
        if not uid.isdigit() or not gid.isdigit():
            raise BootstrapError("Docker API的开发用户配置无效")
        return compose + ["exec", "-T", "--user", f"{uid}:{gid}", "api"] + command, root, None
    except BootstrapError:
        raise
    except (OSError, ValueError, KeyError, IndexError, TypeError, subprocess.SubprocessError):
        raise BootstrapError("无法定位当前Docker API；请核对Docker及本地配置，原生部署使用--native") from None


def create_local_admin(target, username, email, password):
    if not re.fullmatch(r"[a-z0-9][a-z0-9._-]{1,63}", username) or not email or not password:
        raise BootstrapError("用户名格式无效，或邮箱/密码为空")
    body = json.dumps({"username": username, "email": email, "password": password}, ensure_ascii=False).encode()
    if len(body) > 4096:
        raise BootstrapError("初始化请求超过4096字节，请缩短输入")
    command, root, environment = target
    try:
        result = subprocess.run(command, cwd=root, env=environment, input=body, capture_output=True, timeout=120)
        if result.returncode != 0:
            raise BootstrapError("管理员创建未确认；请核对用户名/邮箱是否已存在及数据库状态，已有账号和系统配置不会被覆盖")
        response = json.loads(result.stdout)
        if len(result.stdout) > 65536 or not isinstance(response, dict) or type(response.get("code")) is not int or response["code"] != 0:
            raise BootstrapError("创建结果未确认，请先核对账号状态，勿重复提交")
    except subprocess.TimeoutExpired:
        raise BootstrapError("本机创建命令超时，结果不确定；请先核对账号状态，勿重复提交") from None
    except (OSError, ValueError, AttributeError):
        raise BootstrapError("本机创建结果未确认；请检查部署环境及账号状态，勿重复提交") from None


def api_url(port, path):
    if type(port) is not int or not 1 <= port <= 65535:
        raise BootstrapError("API端口无效")
    return f"http://127.0.0.1:{port}/api/v1/{path}"


def local_opener():
    return urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())


def check_api(port):
    """Check the API without sending credentials or an initialization request."""
    request = urllib.request.Request(api_url(port, "health"), method="GET")
    try:
        with local_opener().open(request, timeout=5) as response:
            raw = response.read(65537)
            if response.status != 200 or len(raw) > 65536:
                raise BootstrapError("API健康检查未通过，尚未发送初始化请求；请检查API服务与端口")
            result = json.loads(raw)
    except urllib.error.HTTPError as error:
        status = error.code
        error.close()
        raise BootstrapError(f"API健康检查失败（HTTP {status}），尚未发送初始化请求；请检查API服务与端口") from None
    except (OSError, http.client.HTTPException):
        raise BootstrapError(f"无法连接本机API（127.0.0.1:{port}），尚未发送初始化请求；请先启动API及数据库等依赖，或用--port指定实际端口") from None
    except ValueError:
        raise BootstrapError("API健康检查响应无效，尚未发送初始化请求；请检查API服务与端口") from None
    if not isinstance(result, dict) or type(result.get("code")) is not int or result["code"] != 0:
        raise BootstrapError("API健康检查未通过，尚未发送初始化请求；请检查API服务与端口")


def create_admin(port, proof, username, email, password):
    url = api_url(port, "admin/identity/bootstrap")
    proof = validate_proof(proof)
    if not re.fullmatch(r"[a-z0-9][a-z0-9._-]{1,63}", username) or not email or not password:
        raise BootstrapError("用户名格式无效，或邮箱/密码为空")
    body = json.dumps({"username": username, "email": email, "password": password,
                       "reason": "部署者首次初始化独立管理域"}, ensure_ascii=False).encode("utf-8")
    if len(body) > 4096:
        raise BootstrapError("初始化请求超过4096字节，请缩短输入")
    request = urllib.request.Request(
        url, data=body,
        headers={"Content-Type": "application/json", "X-CampusOS-Bootstrap-Secret": proof}, method="POST")
    try:
        with local_opener().open(request, timeout=15) as response:
            raw = response.read(65537)
            if response.status != 200 or len(raw) > 65536:
                raise BootstrapError("初始化响应无效，请先核对账号是否已创建，不要盲目重试")
            result = json.loads(raw)
    except urllib.error.HTTPError as error:
        status = error.code
        error.close()
        raise BootstrapError(f"初始化未成功（HTTP {status}）。请核对凭证是否生效、输入格式及是否已有安全管理员") from None
    except urllib.error.URLError as error:
        if isinstance(error.reason, ConnectionRefusedError):
            raise BootstrapError(f"本机API拒绝连接（127.0.0.1:{port}），初始化请求未送达；请启动API或检查--port") from None
        raise BootstrapError("连接中断或超时，初始化结果不确定；请先核对账号是否已创建，不要盲目重复初始化") from None
    except (OSError, ValueError, http.client.HTTPException):
        raise BootstrapError("连接失败或结果不确定，请先检查API及账号结果，不要盲目重复初始化") from None
    if not isinstance(result, dict) or type(result.get("code")) is not int or result["code"] != 0:
        raise BootstrapError("初始化未确认成功，请先核对账号状态，勿重复提交")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runtime-profile", type=Path, help="原生部署使用的私有API运行JSON；--http时仅读取初始化凭证")
    parser.add_argument("--native", action="store_true", help="原生部署：使用当前环境运行本机Go CLI")
    parser.add_argument("--env-file", type=Path, help="Docker开发配置，默认deploy/docker/.env.dev.local")
    parser.add_argument("--http", action="store_true", help="兼容首次回环HTTP初始化，需要手动提供部署凭证")
    parser.add_argument("--port", type=int, default=8080, help="--http模式的本机API端口，默认8080")
    args = parser.parse_args()
    try:
        if not sys.stdin.isatty():
            raise BootstrapError("请在交互终端执行；通过SSH时使用ssh -t，不要通过管道传入密码")
        if args.http:
            check_api(args.port)
            if args.runtime_profile:
                proof = runtime_proof(args.runtime_profile)
                print("已读取本机API运行配置中的管理域初始化凭证（不显示）。")
            else:
                proof = validate_proof(hidden_input("输入AUTH_ADMIN_BOOTSTRAP_SECRET（不回显）："))
        else:
            target = local_command(args)
            print("将通过当前本机部署新增管理员；已有账号及系统配置保持不变。")
        username = input("管理员用户名（例如 operator）：").strip()
        email = input("管理员邮箱：").strip()
        password = hidden_input("为新管理员设置登录密码（不回显）：")
        if not password or password != hidden_input("再次输入登录密码（不回显）："):
            raise BootstrapError("两次密码不一致或为空，未发送初始化请求")
        if args.http:
            create_admin(args.port, proof, username, email, password)
        else:
            create_local_admin(target, username, email, password)
        print("管理员创建成功；已有账号和系统配置保持不变。")
        print("随后在管理端使用刚设置的用户名/密码登录，并按引导配置MFA。")
        return 0
    except (BootstrapError, EOFError, KeyboardInterrupt) as error:
        message = str(error) if isinstance(error, BootstrapError) else "操作已取消，未确认初始化成功"
        print(message, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
