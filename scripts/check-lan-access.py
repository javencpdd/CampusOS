#!/usr/bin/env python3
"""Diagnose CampusOS Docker development UI access from a trusted LAN."""

from __future__ import annotations

import argparse
import http.client
import ipaddress
import json
import os
import shutil
import socket
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any


FIREWALL_RULE_NAME = "CampusOS Dev UI (Private LAN)"
FIREWALL_RULE_PREFIX = "CampusOS-Dev-UI-LAN-"


@dataclass(frozen=True)
class Surface:
    service: str
    label: str
    bind_key: str
    port_key: str
    default_port: int
    target_port: int
    probe_path: str


@dataclass(frozen=True)
class LanInterface:
    address: str
    interface: str
    network: str
    category: str
    prefix_length: int | None = None


@dataclass(frozen=True)
class FirewallAssessment:
    ready: bool
    unsafe: bool
    details: tuple[str, ...]


SURFACES = (
    Surface("web", "Web", "CAMPUSOS_DEV_WEB_BIND", "CAMPUSOS_DEV_WEB_PORT", 3000, 3000, "/api/v1/health"),
    Surface("admin", "Admin", "CAMPUSOS_DEV_ADMIN_BIND", "CAMPUSOS_DEV_ADMIN_PORT", 3001, 3001, "/api/v1/health"),
    Surface("docs", "Docs", "CAMPUSOS_DEV_DOCS_BIND", "CAMPUSOS_DEV_DOCS_PORT", 3002, 3002, "/"),
    Surface("plugin-ui", "Plugin UI gateway", "CAMPUSOS_DEV_PLUGIN_UI_BIND", "CAMPUSOS_DEV_PLUGIN_UI_PORT", 3003, 3003, "/health"),
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Check CampusOS Docker UI publication, running containers, local LAN addresses, "
            "HTTP reachability and Windows/Linux network/firewall hints."
        )
    )
    parser.add_argument(
        "--env-file",
        type=Path,
        default=Path("deploy/docker/.env.dev.local"),
        help="Docker development env file (default: deploy/docker/.env.dev.local)",
    )
    parser.add_argument(
        "--compose-file",
        type=Path,
        default=Path("compose.dev.yml"),
        help="Compose file (default: compose.dev.yml)",
    )
    parser.add_argument(
        "--host",
        action="append",
        default=[],
        help="explicit LAN IPv4 to probe; can be repeated",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=5.0,
        help="HTTP probe timeout in seconds (default: 5)",
    )
    return parser.parse_args()


def repository_root(start: Path) -> Path:
    process = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=start,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if process.returncode != 0:
        raise RuntimeError(process.stderr.strip() or "not inside a Git repository")
    return Path(process.stdout.strip()).resolve()


def resolve_input_path(root: Path, value: Path) -> Path:
    return value.resolve() if value.is_absolute() else (root / value).resolve()


def load_env(path: Path) -> dict[str, str]:
    result: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        if not key:
            continue
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
            value = value[1:-1]
        result[key] = value
    return result


def run_command(command: list[str], cwd: Path) -> str:
    process = subprocess.run(
        command,
        cwd=cwd,
        text=True,
        encoding="utf-8",
        errors="replace",
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if process.returncode != 0:
        message = process.stderr.strip() or process.stdout.strip()
        raise RuntimeError(f"{' '.join(command[:3])} failed: {message}")
    return process.stdout


def compose_command(env_file: Path, compose_file: Path, *args: str) -> list[str]:
    return [
        "docker",
        "compose",
        "--env-file",
        str(env_file),
        "-f",
        str(compose_file),
        *args,
    ]


def parse_json_records(payload: str) -> list[dict[str, Any]]:
    stripped = payload.strip()
    if not stripped:
        return []
    try:
        parsed = json.loads(stripped)
        if isinstance(parsed, list):
            return [item for item in parsed if isinstance(item, dict)]
        if isinstance(parsed, dict):
            return [parsed]
    except json.JSONDecodeError:
        pass
    records: list[dict[str, Any]] = []
    for line in stripped.splitlines():
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(item, dict):
            records.append(item)
    return records


def compose_bindings(config: dict[str, Any]) -> dict[str, tuple[str, int]]:
    result: dict[str, tuple[str, int]] = {}
    services = config.get("services", {})
    for surface in SURFACES:
        service = services.get(surface.service, {})
        for port in service.get("ports", []) or []:
            if not isinstance(port, dict):
                continue
            try:
                target = int(port.get("target", 0))
                published = int(port.get("published", 0))
            except (TypeError, ValueError):
                continue
            if target == surface.target_port:
                result[surface.service] = (str(port.get("host_ip", "")), published)
                break
    return result


def windows_interfaces(root: Path) -> list[LanInterface]:
    command = r"""
$profiles = @{}
Get-NetConnectionProfile -ErrorAction SilentlyContinue | ForEach-Object {
  $profiles[[int]$_.InterfaceIndex] = $_
}
$items = @(
  Get-NetIPConfiguration -ErrorAction SilentlyContinue |
    Where-Object {
      $_.NetAdapter.Status -eq 'Up' -and
      $null -ne $_.IPv4DefaultGateway -and
      $null -ne $_.IPv4Address
    } |
    ForEach-Object {
      $configuration = $_
      $profile = $profiles[[int]$configuration.InterfaceIndex]
      foreach ($address in @($configuration.IPv4Address)) {
        [pscustomobject]@{
          address = [string]$address.IPAddress
          interface = [string]$configuration.InterfaceAlias
          network = if ($null -ne $profile) { [string]$profile.Name } else { '' }
          category = if ($null -ne $profile) { [string]$profile.NetworkCategory } else { 'Unknown' }
          prefix_length = [int]$address.PrefixLength
        }
      }
    }
)
$items | ConvertTo-Json -Compress
"""
    executable = "powershell.exe"
    output = run_command([executable, "-NoProfile", "-NonInteractive", "-Command", command], root)
    if not output.strip():
        return []
    parsed = json.loads(output)
    records = parsed if isinstance(parsed, list) else [parsed]
    return [
        LanInterface(
            str(record.get("address", "")),
            str(record.get("interface", "")),
            str(record.get("network", "")),
            str(record.get("category", "Unknown")),
            int(record["prefix_length"]) if record.get("prefix_length") is not None else None,
        )
        for record in records
        if isinstance(record, dict) and record.get("address")
    ]


def parse_linux_interfaces(address_payload: str, route_payload: str) -> list[LanInterface]:
    address_records = json.loads(address_payload)
    route_records = json.loads(route_payload)
    default_devices = {
        str(record.get("dev", ""))
        for record in route_records
        if isinstance(record, dict) and record.get("dev")
    }
    result: list[LanInterface] = []
    for record in address_records:
        if not isinstance(record, dict):
            continue
        interface = str(record.get("ifname", ""))
        if default_devices and interface not in default_devices:
            continue
        for address_record in record.get("addr_info", []) or []:
            if not isinstance(address_record, dict) or address_record.get("family") != "inet":
                continue
            candidate = str(address_record.get("local", ""))
            try:
                address = ipaddress.ip_address(candidate)
                prefix_length = int(address_record.get("prefixlen", 32))
                subnet = str(ipaddress.ip_network(f"{candidate}/{prefix_length}", strict=False))
            except (TypeError, ValueError):
                continue
            if address.is_loopback or address.is_link_local or address.is_unspecified:
                continue
            result.append(LanInterface(candidate, interface or "auto-detected", subnet, "Linux"))
    return result


def linux_interfaces(root: Path) -> list[LanInterface]:
    address_payload = run_command(["ip", "-j", "-4", "addr", "show", "up"], root)
    route_payload = run_command(["ip", "-j", "-4", "route", "show", "default"], root)
    return parse_linux_interfaces(address_payload, route_payload)


def fallback_addresses() -> list[LanInterface]:
    candidates: set[str] = set()
    route_socket = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        route_socket.connect(("192.0.2.1", 9))
        candidates.add(route_socket.getsockname()[0])
    except OSError:
        pass
    finally:
        route_socket.close()
    try:
        for item in socket.getaddrinfo(socket.gethostname(), None, socket.AF_INET):
            candidates.add(item[4][0])
    except OSError:
        pass

    result: list[LanInterface] = []
    for candidate in sorted(candidates):
        try:
            address = ipaddress.ip_address(candidate)
        except ValueError:
            continue
        if address.is_loopback or address.is_link_local or address.is_unspecified:
            continue
        result.append(LanInterface(candidate, "auto-detected", "", "Unknown"))
    return result


def select_interfaces(explicit_hosts: list[str], root: Path) -> list[LanInterface]:
    if explicit_hosts:
        result: list[LanInterface] = []
        for host in explicit_hosts:
            try:
                parsed = ipaddress.ip_address(host)
            except ValueError as error:
                raise RuntimeError(f"invalid --host address {host!r}: {error}") from error
            if parsed.version != 4 or parsed.is_loopback or parsed.is_unspecified:
                raise RuntimeError(f"--host must be a non-loopback IPv4 address: {host}")
            result.append(LanInterface(host, "explicit", "", "Unknown"))
        return result

    if os.name == "nt":
        try:
            detected = windows_interfaces(root)
            if detected:
                return detected
        except (RuntimeError, OSError, json.JSONDecodeError):
            pass
    elif sys.platform.startswith("linux"):
        try:
            detected = linux_interfaces(root)
            if detected:
                return detected
        except (RuntimeError, OSError, json.JSONDecodeError):
            pass
    return fallback_addresses()


def windows_firewall_rules(root: Path) -> list[dict[str, Any]] | None:
    if os.name != "nt":
        return None
    escaped_name = FIREWALL_RULE_NAME.replace("'", "''")
    command = rf"""
$ErrorActionPreference = 'Stop'
$items = @(
  Get-NetFirewallRule -PolicyStore ActiveStore | Where-Object {{
    $_.DisplayName -eq '{escaped_name}' -or $_.Name -like '{FIREWALL_RULE_PREFIX}*'
  }} | ForEach-Object {{
    $rule = $_
    $port = $rule | Get-NetFirewallPortFilter
    $address = $rule | Get-NetFirewallAddressFilter
    $interface = $rule | Get-NetFirewallInterfaceFilter
    [pscustomobject]@{{
      name = [string]$rule.Name
      display_name = [string]$rule.DisplayName
      enabled = [string]$rule.Enabled
      direction = [string]$rule.Direction
      action = [string]$rule.Action
      profiles = [string]$rule.Profile
      edge_traversal = [string]$rule.EdgeTraversalPolicy
      protocol = [string]$port.Protocol
      local_ports = @($port.LocalPort)
      local_addresses = @($address.LocalAddress)
      remote_addresses = @($address.RemoteAddress)
      interfaces = @($interface.InterfaceAlias)
    }}
  }}
)
$items | ConvertTo-Json -Depth 4 -Compress
"""
    try:
        return parse_json_records(run_command(
            ["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command], root,
        ))
    except RuntimeError:
        return None


def firewall_values(value: Any) -> set[str]:
    values = value if isinstance(value, list) else [value]
    return {part.strip() for item in values for part in str(item or "").split(",") if part.strip()}


def firewall_ports(value: Any, expected: set[int]) -> set[int] | None:
    ports: set[int] = set()
    for token in firewall_values(value):
        try:
            bounds = token.split("-")
            if len(bounds) == 1:
                first = last = int(bounds[0])
            elif len(bounds) == 2:
                first, last = map(int, bounds)
            else:
                return None
            if first < 1 or last > 65535 or last < first or last - first + 1 > len(expected):
                return None
            ports.update(range(first, last + 1))
        except ValueError:
            return None
    return ports if ports and ports <= expected else None


def assess_windows_firewall_rules(
    rules: list[dict[str, Any]], interfaces: list[LanInterface], ports: set[int],
) -> FirewallAssessment:
    """Validate owned rules, not the whole Windows firewall policy or remote reachability."""
    if not interfaces or any(interface.prefix_length is None for interface in interfaces):
        return FirewallAssessment(False, False, ("current Windows LAN prefix metadata is unavailable; firewall scope was not verified",))
    covered = {interface.address: set() for interface in interfaces}
    details: list[str] = []
    unsafe = False
    for rule in rules:
        name = str(rule.get("name", ""))
        if not name.startswith(FIREWALL_RULE_PREFIX) and rule.get("display_name") != FIREWALL_RULE_NAME:
            continue
        if (str(rule.get("enabled", "")).lower() != "true" or
                str(rule.get("direction", "")).lower() != "inbound" or
                str(rule.get("action", "")).lower() != "allow"):
            continue
        selected_ports = firewall_ports(rule.get("local_ports"), ports)
        profiles = firewall_values(rule.get("profiles"))
        safe = (selected_ports is not None and str(rule.get("protocol", "")).upper() in {"TCP", "6"}
                and str(rule.get("edge_traversal", "")).lower() == "block"
                and bool(profiles) and profiles <= {"Public", "Private", "Domain"})
        matched = False
        for interface in interfaces:
            try:
                if interface.prefix_length is None:
                    continue
                address = ipaddress.ip_address(interface.address)
                subnet = ipaddress.ip_network(f"{interface.address}/{interface.prefix_length}", strict=False)
                local = {ipaddress.ip_network(value, strict=False) for value in firewall_values(rule.get("local_addresses"))}
                remote = {ipaddress.ip_network(value, strict=False) for value in firewall_values(rule.get("remote_addresses"))}
            except ValueError:
                continue
            if (local != {ipaddress.ip_network(f"{address}/32")} or remote != {subnet}
                    or firewall_values(rule.get("interfaces")) != {interface.interface}):
                continue
            matched = True
            profile = "Domain" if interface.category == "DomainAuthenticated" else interface.category
            if safe and profile in profiles:
                covered[interface.address].update(selected_ports or set())
        if not safe or not matched:
            unsafe = True
            details.append(f'owned firewall rule "{name}" is not restricted to the current LAN IP/subnet/interface and UI TCP ports')
    missing = [address for address, allowed in covered.items() if allowed != ports]
    if not covered or missing:
        details.append("no verified owned firewall rule covers every current LAN interface and UI port")
    return FirewallAssessment(bool(covered) and not missing and not unsafe, unsafe, tuple(details))


def probe_http(address: str, port: int, path: str, timeout: float) -> tuple[bool, str]:
    connection = http.client.HTTPConnection(address, port, timeout=timeout)
    try:
        connection.request("GET", path, headers={"Connection": "close"})
        response = connection.getresponse()
        response.read(1024)
        if 200 <= response.status < 400:
            return True, f"HTTP {response.status}"
        return False, f"HTTP {response.status}"
    except OSError as error:
        return False, str(error)
    finally:
        connection.close()


def main() -> int:
    args = parse_args()
    try:
        root = repository_root(Path.cwd())
        env_file = resolve_input_path(root, args.env_file)
        compose_file = resolve_input_path(root, args.compose_file)
        if not env_file.is_file():
            raise RuntimeError(f"env file does not exist: {env_file}")
        if not compose_file.is_file():
            raise RuntimeError(f"Compose file does not exist: {compose_file}")
        env = load_env(env_file)
        config_payload = run_command(
            compose_command(env_file, compose_file, "config", "--format", "json"),
            root,
        )
        config = json.loads(config_payload)
        bindings = compose_bindings(config)
        ps_payload = run_command(
            compose_command(env_file, compose_file, "ps", "--format", "json"),
            root,
        )
        runtime_records = parse_json_records(ps_payload)
        interfaces = select_interfaces(args.host, root)
    except (RuntimeError, OSError, json.JSONDecodeError) as error:
        print(f"CampusOS LAN access check failed: {error}", file=sys.stderr)
        return 2

    runtime = {
        str(record.get("Service", record.get("service", ""))): record
        for record in runtime_records
    }
    failures: list[str] = []
    warnings: list[str] = []

    print("CampusOS LAN access diagnostic")
    print(f"env: {env_file}")
    print()
    print("Compose and runtime:")

    allow_lan = env.get("CAMPUSOS_DEV_ALLOW_LAN", "false").strip().lower()
    if allow_lan != "true":
        failures.append("CAMPUSOS_DEV_ALLOW_LAN is not true")

    effective_ports: dict[str, int] = {}
    for surface in SURFACES:
        configured_bind = env.get(surface.bind_key, "127.0.0.1")
        try:
            configured_port = int(env.get(surface.port_key, str(surface.default_port)))
        except ValueError:
            configured_port = 0
        binding = bindings.get(surface.service)
        record = runtime.get(surface.service, {})
        state = str(record.get("State", record.get("state", "unknown"))).lower()
        health = str(record.get("Health", record.get("health", ""))).lower()
        running = state == "running" and health in {"", "healthy"}
        binding_ok = binding == ("0.0.0.0", configured_port)
        effective_ports[surface.service] = configured_port
        marker = "PASS" if binding_ok and running and configured_bind == "0.0.0.0" else "FAIL"
        rendered = f"{binding[0]}:{binding[1]}" if binding else "missing"
        print(
            f"[{marker}] {surface.label}: configured={configured_bind}:{configured_port}, "
            f"published={rendered}, state={state or 'unknown'}, health={health or 'n/a'}"
        )
        if configured_bind != "0.0.0.0":
            failures.append(f"{surface.bind_key} is {configured_bind}, expected 0.0.0.0")
        if not binding_ok:
            failures.append(f"{surface.label} is not published as 0.0.0.0:{configured_port}")
        if not running:
            failures.append(f"{surface.label} container is not running and healthy")

    print()
    print("Detected LAN IPv4 addresses:")
    if not interfaces:
        failures.append("no non-loopback LAN IPv4 address was detected")
        print("[FAIL] none")
    for interface in interfaces:
        details = f"interface={interface.interface}"
        if interface.network:
            details += f", network={interface.network}"
        details += f", category={interface.category}"
        print(f"- {interface.address} ({details})")

    print()
    print("Local probes through each LAN address:")
    successful_addresses: list[str] = []
    for interface in interfaces:
        address_ok = True
        for surface in SURFACES:
            port = effective_ports.get(surface.service, 0)
            ok, detail = probe_http(interface.address, port, surface.probe_path, args.timeout)
            print(
                f"[{'PASS' if ok else 'FAIL'}] "
                f"http://{interface.address}:{port}{surface.probe_path} -> {detail}"
            )
            if not ok:
                address_ok = False
                failures.append(f"{surface.label} probe failed through {interface.address}:{port}: {detail}")
        if address_ok:
            successful_addresses.append(interface.address)

    if os.name == "nt":
        firewall = windows_firewall_rules(root)
        if firewall is None:
            warnings.append("Windows firewall state could not be determined")
        else:
            assessment = assess_windows_firewall_rules(firewall, interfaces, set(effective_ports.values()))
            if assessment.ready:
                print("[PASS] Windows owned firewall rules restrict UI TCP ports to the current LAN IP/subnet/interface")
            elif assessment.unsafe:
                failures.extend(assessment.details)
            else:
                warnings.extend(assessment.details)
            if not assessment.ready and any(interface.category.lower() == "public" for interface in interfaces):
                warnings.append("Windows Public network profile has no verified current-LAN UI rule")
    elif sys.platform.startswith("linux"):
        if shutil.which("ufw"):
            warnings.append(
                "Linux UFW may require an inbound rule for TCP 3000:3003 from the trusted LAN subnet"
            )
        elif shutil.which("firewall-cmd"):
            warnings.append(
                "Linux firewalld may require TCP 3000-3003 in the active trusted LAN zone"
            )
        else:
            warnings.append(
                "Linux host firewall state was not determined; check nftables/iptables policy for TCP 3000-3003"
            )

    print()
    print("Use these URLs from another device on the same trusted LAN:")
    for address in successful_addresses:
        for surface in SURFACES:
            print(f"- {surface.label}: http://{address}:{effective_ports[surface.service]}")

    print()
    if successful_addresses:
        sample = successful_addresses[0]
        print("Remote verification (run from another LAN device):")
        print(
            "Windows PowerShell: "
            + "; ".join(
                f"Test-NetConnection {sample} -Port {effective_ports[surface.service]}"
                for surface in SURFACES
            )
        )
        print(
            "Linux/macOS shell: "
            + " && ".join(
                f"curl --fail --silent --show-error "
                f"http://{sample}:{effective_ports[surface.service]}{surface.probe_path}"
                for surface in SURFACES
            )
        )
        print(
            "Linux TCP check (when nc is installed): "
            + " && ".join(
                f"nc -vz {sample} {effective_ports[surface.service]}"
                for surface in SURFACES
            )
        )
        print(f"Browser: http://{sample}:{effective_ports['web']}")

    if warnings:
        print()
        print("Warnings:")
        for warning in dict.fromkeys(warnings):
            print(f"- {warning}")
    print()

    if failures:
        print("Result: NOT READY")
        for failure in dict.fromkeys(failures):
            print(f"- {failure}")
        return 1

    if warnings:
        print(
            "Result: LOCAL READY; REMOTE ACCESS MAY STILL BE BLOCKED. "
            "Confirm the network is trusted, firewall policy permits LocalSubnet, "
            "and the router has no AP/client isolation."
        )
    else:
        print(
            "Result: LOCAL READY; REMOTE ACCESS IS LIKELY. "
            "A probe from another LAN device is still required to prove end-to-end access."
        )
    return 0


if __name__ == "__main__":
    sys.exit(main())
