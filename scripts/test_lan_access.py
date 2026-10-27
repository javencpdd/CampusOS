#!/usr/bin/env python3
"""Unit tests for the CampusOS LAN access diagnostic."""

from __future__ import annotations

import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-lan-access.py")
SPEC = importlib.util.spec_from_file_location("campusos_lan_access", SCRIPT)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"cannot load {SCRIPT}")
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class LanAccessTest(unittest.TestCase):
    def firewall_fixture(self) -> tuple[dict, list, set[int]]:
        return (
            {
                "name": "CampusOS-Dev-UI-LAN-192.168.31.0-24",
                "display_name": "CampusOS Dev UI (WLAN subnet only)",
                "enabled": "True", "direction": "Inbound", "action": "Allow",
                "profiles": "Private, Public", "edge_traversal": "Block", "protocol": "TCP",
                "local_ports": ["3000", "3001", "3002", "3003"],
                "local_addresses": ["192.168.31.188"],
                "remote_addresses": ["192.168.31.0/24"], "interfaces": ["WLAN"],
            },
            [MODULE.LanInterface("192.168.31.188", "WLAN", "trusted", "Public", 24)],
            {3000, 3001, 3002, 3003},
        )

    def test_load_env_uses_last_value_and_strips_quotes(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "test.env"
            path.write_text(
                "# comment\nCAMPUSOS_DEV_ALLOW_LAN=false\n"
                "CAMPUSOS_DEV_ALLOW_LAN=true\n"
                'CAMPUSOS_DEV_WEB_BIND="0.0.0.0"\n',
                encoding="utf-8",
            )
            result = MODULE.load_env(path)
        self.assertEqual(result["CAMPUSOS_DEV_ALLOW_LAN"], "true")
        self.assertEqual(result["CAMPUSOS_DEV_WEB_BIND"], "0.0.0.0")

    def test_parse_json_records_accepts_array_and_json_lines(self) -> None:
        self.assertEqual(
            MODULE.parse_json_records('[{"Service":"web"},{"Service":"admin"}]'),
            [{"Service": "web"}, {"Service": "admin"}],
        )
        self.assertEqual(
            MODULE.parse_json_records('{"Service":"web"}\n{"Service":"docs"}\n'),
            [{"Service": "web"}, {"Service": "docs"}],
        )

    def test_compose_bindings_find_all_ui_surfaces(self) -> None:
        config = {
            "services": {
                "web": {"ports": [{"host_ip": "0.0.0.0", "published": "3000", "target": 3000}]},
                "admin": {"ports": [{"host_ip": "0.0.0.0", "published": "3001", "target": 3001}]},
                "docs": {"ports": [{"host_ip": "127.0.0.1", "published": "3002", "target": 3002}]},
                "plugin-ui": {"ports": [{"host_ip": "0.0.0.0", "published": "3003", "target": 3003}]},
            }
        }
        self.assertEqual(
            MODULE.compose_bindings(config),
            {
                "web": ("0.0.0.0", 3000),
                "admin": ("0.0.0.0", 3001),
                "docs": ("127.0.0.1", 3002),
                "plugin-ui": ("0.0.0.0", 3003),
            },
        )

    def test_linux_interfaces_prefers_default_route_and_reports_subnet(self) -> None:
        addresses = json.dumps(
            [
                {
                    "ifname": "eth0",
                    "addr_info": [
                        {"family": "inet", "local": "192.168.10.23", "prefixlen": 24}
                    ],
                },
                {
                    "ifname": "docker0",
                    "addr_info": [
                        {"family": "inet", "local": "172.17.0.1", "prefixlen": 16}
                    ],
                },
            ]
        )
        routes = json.dumps([{"dst": "default", "dev": "eth0"}])
        self.assertEqual(
            MODULE.parse_linux_interfaces(addresses, routes),
            [MODULE.LanInterface("192.168.10.23", "eth0", "192.168.10.0/24", "Linux")],
        )

    def test_firewall_recognizes_owned_name_and_actual_filters(self) -> None:
        rule, interfaces, ports = self.firewall_fixture()
        result = MODULE.assess_windows_firewall_rules([rule], interfaces, ports)
        self.assertTrue(result.ready)
        self.assertFalse(result.unsafe)
        self.assertEqual(result.details, ())
        rule["remote_addresses"] = ["192.168.31.0/255.255.255.0"]
        self.assertTrue(MODULE.assess_windows_firewall_rules([rule], interfaces, ports).ready)

    def test_firewall_accepts_legacy_display_name_only_with_safe_filters(self) -> None:
        rule, interfaces, ports = self.firewall_fixture()
        rule.update(name="legacy-generated-name", display_name=MODULE.FIREWALL_RULE_NAME)
        self.assertTrue(MODULE.assess_windows_firewall_rules([rule], interfaces, ports).ready)

    def test_firewall_accepts_bounded_port_range_and_split_rules(self) -> None:
        rule, interfaces, ports = self.firewall_fixture()
        rule["local_ports"] = "3000-3003"
        self.assertTrue(MODULE.assess_windows_firewall_rules([rule], interfaces, ports).ready)
        rule["local_ports"] = ["3000", "3001"]
        other = {**rule, "name": rule["name"] + "-second", "local_ports": ["3002", "3003"]}
        self.assertTrue(MODULE.assess_windows_firewall_rules([rule, other], interfaces, ports).ready)

    def test_firewall_rejects_broad_filters_even_if_safe_rule_also_exists(self) -> None:
        good, interfaces, ports = self.firewall_fixture()
        for key, value in {
            "remote_addresses": ["Any"], "local_addresses": ["Any"],
            "interfaces": ["Any"], "local_ports": ["3000-3003", "8080"],
            "protocol": "Any", "profiles": "Any", "edge_traversal": "Allow",
        }.items():
            with self.subTest(filter=key):
                broad = {**good, "name": good["name"] + "-broad", key: value}
                result = MODULE.assess_windows_firewall_rules([good, broad], interfaces, ports)
                self.assertFalse(result.ready)
                self.assertTrue(result.unsafe)

    def test_firewall_rejects_wider_remote_network_or_stale_interface(self) -> None:
        rule, interfaces, ports = self.firewall_fixture()
        for changes in (
            {"remote_addresses": ["192.168.0.0/16"]},
            {"local_addresses": ["192.168.31.189"]},
            {"interfaces": ["Ethernet"]},
        ):
            with self.subTest(changes=changes):
                self.assertFalse(MODULE.assess_windows_firewall_rules([{**rule, **changes}], interfaces, ports).ready)

    def test_firewall_disabled_or_wrong_profile_cannot_prove_access(self) -> None:
        rule, interfaces, ports = self.firewall_fixture()
        for changes in ({"enabled": "False"}, {"profiles": "Private"}, {"direction": "Outbound"}):
            with self.subTest(changes=changes):
                result = MODULE.assess_windows_firewall_rules([{**rule, **changes}], interfaces, ports)
                self.assertFalse(result.ready)
                self.assertFalse(result.unsafe)

    def test_firewall_does_not_audit_unrelated_rules_or_guess_missing_prefix(self) -> None:
        rule, interfaces, ports = self.firewall_fixture()
        unrelated = {**rule, "name": "other-software", "local_ports": ["Any"]}
        self.assertTrue(MODULE.assess_windows_firewall_rules([rule, unrelated], interfaces, ports).ready)
        explicit = [MODULE.LanInterface("192.168.31.188", "explicit", "", "Unknown")]
        unknown = MODULE.assess_windows_firewall_rules([rule], explicit, ports)
        self.assertFalse(unknown.ready)
        self.assertFalse(unknown.unsafe)

    def test_firewall_port_parser_rejects_unbounded_or_invalid_ranges(self) -> None:
        _, _, ports = self.firewall_fixture()
        for value in ("Any", "1-65535", "3003-3000", "3000-3004", "3000-3001-3002", "0", "65536"):
            with self.subTest(value=value):
                self.assertIsNone(MODULE.firewall_ports(value, ports))


if __name__ == "__main__":
    unittest.main()
