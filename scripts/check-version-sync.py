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


def current_development_stage() -> str:
    status = json.loads(read("config/project-status.json"))
    if not isinstance(status, dict) or status.get("schema") != "campusos.project-status/v1":
        raise ValueError("invalid project status schema")
    stage = status.get("development_stage")
    plan = status.get("plan")
    if not isinstance(stage, str) or not re.fullmatch(r"v\d+\.\d+-dev", stage):
        raise ValueError("invalid development stage in project status")
    if not isinstance(plan, str) or not plan:
        raise ValueError("missing current plan in project status")
    plan_path = (ROOT / plan).resolve()
    if not plan_path.is_relative_to(ROOT.resolve()):
        raise ValueError("current plan must be inside the repository")
    heading = re.search(
        r"^#\s+CampusOS\s+(v\d+\.\d+)\b",
        plan_path.read_text(encoding="utf-8"),
        re.MULTILINE,
    )
    if not heading or heading.group(1) != stage.removesuffix("-dev"):
        raise ValueError("development stage does not match the explicitly selected plan")
    return stage


def main() -> int:
    errors: list[str] = []
    try:
        application = match(
            "internal/platform/version/version.go",
            r'^\s*Number\s*=\s*"(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)"',
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
        expected_typescript_sdk = application if "-" in application else f"{application}-dev"
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

        deployment_tag = match(
            "deploy/docker/.env.example",
            r"^CAMPUSOS_IMAGE_TAG=([^\s#]+)$",
            "deployment image tag",
        )
        if deployment_tag != application:
            errors.append(
                f"deployment image tag={deployment_tag}, expected={application}"
            )

        dockerfile_version = match(
            "deploy/docker/Dockerfile",
            r"^ARG CAMPUSOS_VERSION=([^\s#]+)$",
            "Docker image version",
        )
        if dockerfile_version != application:
            errors.append(
                f"Docker image version={dockerfile_version}, expected={application}"
            )

        for path in (
            "compose.deploy.yml",
            "deploy/docker/components/compose.api.yml",
            "deploy/docker/components/compose.web.yml",
            "deploy/docker/components/compose.admin.yml",
            "deploy/docker/components/compose.docs.yml",
        ):
            default_tags = re.findall(r"\$\{CAMPUSOS_IMAGE_TAG:-([^}]+)\}", read(path))
            if not default_tags:
                errors.append(f"{path} does not define a default image tag")
            for tag in default_tags:
                if tag != application:
                    errors.append(f"{path} default image tag={tag}, expected={application}")

        display = f"v{application}"
        # Runtime release identity and the explicitly selected development plan
        # can advance independently. Keep every runtime/package/tag check above.
        development_stage = current_development_stage()
        for path in (
            "README.md",
            "docs/README.md",
            "docs-site/index.md",
            "docs-site/guide/introduction.md",
        ):
            content = read(path)
            if display not in content:
                errors.append(f"{path} does not show application version {display}")
            if development_stage not in content:
                errors.append(f"{path} does not show development stage {development_stage}")
    except (KeyError, OSError, ValueError, json.JSONDecodeError) as error:
        errors.append(str(error))

    if errors:
        for error in errors:
            print(f"version sync error: {error}", file=sys.stderr)
        return 1
    print(
        "version sync check passed: "
        f"application={application}, development_stage={development_stage}, "
        f"sdk={expected_sdk}, openapi={expected_openapi}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
