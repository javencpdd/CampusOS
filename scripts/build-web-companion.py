#!/usr/bin/env python3
"""Build the fixed download artifacts for the user-run public web companion."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parents[1]
platforms = ["windows-amd64", "linux-amd64", "linux-arm64", "darwin-arm64", "darwin-amd64"]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--output",type=Path,default=root / ".campusos/tools/web-companion")
parser.add_argument("--platform", choices=platforms, action="append")
args = parser.parse_args()
output = args.output.resolve()
output.mkdir(parents=True, exist_ok=True)
for platform in args.platform or platforms:
    target_os, target_arch = platform.split("-")
    name = "campusos-web-companion-" + platform + (".exe" if target_os == "windows" else "")
    target = output / name
    print("Building " + platform, flush=True)
    subprocess.run(["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(target), "./cmd/campusos-web-companion"], cwd=root, env={**os.environ, "CGO_ENABLED": "0", "GOOS": target_os, "GOARCH": target_arch}, check=True)
    target.chmod(0o755)
manifest = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(output.glob("campusos-web-companion-*")) if path.is_file()}
(output / "SHA256SUMS.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
print("Download artifacts: " + str(output), flush=True)
