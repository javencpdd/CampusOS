#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def match(path: str, pattern: str, label: str) -> str:
    found = re.search(pattern, read(path), re.MULTILINE)
    if not found:
        raise ValueError(f"cannot find {label} in {path}")
    return found.group(1)


def main() -> int:
    errors: list[str] = []
    try:
        application = match(
            "internal/platform/version/version.go",
            r'^\s*Number\s*=\s*"(\d+\.\d+\.\d+)"',
            "application version",
        )
        major_minor = ".".join(application.split(".")[:2])
        expected_sdk = f"v{major_minor}"

        docs_package = json.loads(read("docs-site/package.json"))["version"]
        if docs_package != application:
            errors.append(
                f"docs-site package version={docs_package}, expected={application}"
            )

        typescript_sdk = json.loads(read("sdk/typescript/package.json"))["version"]
        expected_typescript_sdk = f"{application}-dev"
        if typescript_sdk != expected_typescript_sdk:
            errors.append(
                "TypeScript SDK package version="
                f"{typescript_sdk}, expected={expected_typescript_sdk}"
            )

        go_sdk = match(
            "sdk/go/hostapi.go",
            r'^\s*SDKVersion\s*=\s*"(v\d+\.\d+)"',
            "Go SDK version",
        )
        if go_sdk != expected_sdk:
            errors.append(f"Go SDK version={go_sdk}, expected={expected_sdk}")

        openapi = match(
            "docs/api/openapi-v0.6-current.yaml",
            r"(?m)^\s*version:\s*([^\s]+)\s*$",
            "OpenAPI version",
        )
        expected_openapi = f"{application}-experimental"
        if openapi != expected_openapi:
            errors.append(f"OpenAPI version={openapi}, expected={expected_openapi}")
    except (KeyError, OSError, ValueError, json.JSONDecodeError) as error:
        errors.append(str(error))

    if errors:
        for error in errors:
            print(f"version sync error: {error}", file=sys.stderr)
        return 1
    print(
        "version sync check passed: "
        f"application={application}, sdk={expected_sdk}, openapi={expected_openapi}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
