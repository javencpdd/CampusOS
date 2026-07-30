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

    def test_compose_bindings_find_all_three_surfaces(self) -> None:
        config = {
            "services": {
                "web": {"ports": [{"host_ip": "0.0.0.0", "published": "3000", "target": 3000}]},
                "admin": {"ports": [{"host_ip": "0.0.0.0", "published": "3001", "target": 3001}]},
                "docs": {"ports": [{"host_ip": "127.0.0.1", "published": "3002", "target": 3002}]},
            }
        }
        self.assertEqual(
            MODULE.compose_bindings(config),
            {
                "web": ("0.0.0.0", 3000),
                "admin": ("0.0.0.0", 3001),
                "docs": ("127.0.0.1", 3002),
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
            [
                MODULE.LanInterface(
                    "192.168.10.23",
                    "eth0",
                    "192.168.10.0/24",
                    "Linux",
                )
            ],
        )


if __name__ == "__main__":
    unittest.main()
