#!/usr/bin/env python3
"""Package the explicit runtime allowlist; never include local bot data."""
import argparse
import hashlib
from pathlib import Path
import zipfile

ROOT = Path(__file__).resolve().parents[1]
REPO = ROOT.parents[1]
FILES = ("package.json", "runner.mjs", "onebot.mjs", "core.mjs", "bot.mjs", "receipts.mjs", "catalog.json", "README.md", "config.example.json", "campusos-qqbot.service.example")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=REPO / ".cache/qqbot-plugin/campusos-qqbot-0.1.0.zip")
    args = parser.parse_args()
    contents = {name: (ROOT / name).read_bytes() for name in FILES}
    contents["LICENSE"] = (REPO / "LICENSE").read_bytes()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open("xb") as handle:
        with zipfile.ZipFile(handle, "w", compression=zipfile.ZIP_DEFLATED) as archive:
            for name, content in contents.items():
                info = zipfile.ZipInfo(name, (2026, 1, 1, 0, 0, 0))
                info.compress_type = zipfile.ZIP_DEFLATED
                info.external_attr = 0o100644 << 16
                archive.writestr(info, content)
    print(f"{args.output}: sha256={hashlib.sha256(args.output.read_bytes()).hexdigest()}")


if __name__ == "__main__":
    main()
