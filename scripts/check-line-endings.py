#!/usr/bin/env python3
"""Validate or repair tracked working-tree files using Git EOL attributes."""

from __future__ import annotations

import argparse
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


UTF8_BOM = b"\xef\xbb\xbf"


@dataclass(frozen=True)
class RepositoryFile:
    path: str
    mode: str
    tracked: bool


def run_git(root: Path, *args: str, input_bytes: bytes | None = None) -> bytes:
    process = subprocess.run(
        ["git", *args],
        cwd=root,
        input=input_bytes,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if process.returncode != 0:
        message = process.stderr.decode("utf-8", "replace").strip()
        raise RuntimeError(f"git {' '.join(args)} failed: {message}")
    return process.stdout


def repository_root(start: Path) -> Path:
    output = run_git(start, "rev-parse", "--show-toplevel")
    return Path(output.decode("utf-8", "surrogateescape").strip()).resolve()


def decode_git_path(value: bytes) -> str:
    return value.decode("utf-8", "surrogateescape")


def list_repository_files(root: Path, include_untracked: bool) -> list[RepositoryFile]:
    files: list[RepositoryFile] = []
    output = run_git(root, "ls-files", "-s", "-z")
    for record in output.split(b"\0"):
        if not record:
            continue
        metadata, raw_path = record.split(b"\t", 1)
        mode = metadata.split(b" ", 1)[0].decode("ascii")
        if mode in {"120000", "160000"}:
            continue
        files.append(RepositoryFile(decode_git_path(raw_path), mode, True))

    if include_untracked:
        output = run_git(root, "ls-files", "--others", "--exclude-standard", "-z")
        tracked_paths = {item.path for item in files}
        for raw_path in output.split(b"\0"):
            if not raw_path:
                continue
            path = decode_git_path(raw_path)
            if path not in tracked_paths:
                files.append(RepositoryFile(path, "100644", False))

    return sorted(files, key=lambda item: item.path)


def git_attributes(root: Path, files: list[RepositoryFile]) -> dict[str, dict[str, str]]:
    if not files:
        return {}
    path_input = b"".join(item.path.encode("utf-8", "surrogateescape") + b"\0" for item in files)
    output = run_git(
        root,
        "check-attr",
        "-z",
        "--stdin",
        "text",
        "eol",
        input_bytes=path_input,
    )
    fields = output.split(b"\0")
    if fields and fields[-1] == b"":
        fields.pop()
    if len(fields) % 3 != 0:
        raise RuntimeError("unexpected NUL output from git check-attr")

    result: dict[str, dict[str, str]] = {}
    for index in range(0, len(fields), 3):
        path = decode_git_path(fields[index])
        attribute = fields[index + 1].decode("ascii")
        value = fields[index + 2].decode("ascii")
        result.setdefault(path, {})[attribute] = value
    return result


def newline_counts(data: bytes) -> tuple[int, int, int]:
    crlf = data.count(b"\r\n")
    bare_lf = data.count(b"\n") - crlf
    bare_cr = data.count(b"\r") - crlf
    return crlf, bare_lf, bare_cr


def newline_problem(data: bytes, expected_eol: str) -> str | None:
    crlf, bare_lf, bare_cr = newline_counts(data)
    if expected_eol == "lf" and (crlf or bare_cr):
        return f"expected LF, found CRLF={crlf}, bare-CR={bare_cr}"
    if expected_eol == "crlf" and (bare_lf or bare_cr):
        return f"expected CRLF, found bare-LF={bare_lf}, bare-CR={bare_cr}"
    return None


def normalize_newlines(data: bytes, expected_eol: str) -> bytes:
    normalized = data.replace(b"\r\n", b"\n").replace(b"\r", b"\n")
    if expected_eol == "crlf":
        return normalized.replace(b"\n", b"\r\n")
    return normalized


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Check tracked files against .gitattributes. By default the script is read-only; "
            "--fix rewrites newline bytes only and never stages files."
        )
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=Path.cwd(),
        help="repository path or a directory inside it (default: current directory)",
    )
    parser.add_argument(
        "--include-untracked",
        action="store_true",
        help="also check untracked, non-ignored files",
    )
    parser.add_argument(
        "--fix",
        action="store_true",
        help="repair LF/CRLF mismatches in the working tree; does not run git add",
    )
    parser.add_argument(
        "--max-errors",
        type=int,
        default=100,
        help="maximum number of individual errors to print (default: 100)",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        root = repository_root(args.root.resolve())
        files = list_repository_files(root, args.include_untracked)
        attributes = git_attributes(root, files)
    except RuntimeError as error:
        print(f"line ending check failed: {error}", file=sys.stderr)
        return 2

    checked_text = 0
    skipped_binary = 0
    fixed = 0
    errors: list[str] = []

    for item in files:
        relative = Path(*item.path.split("/"))
        target = root / relative
        if not target.is_file():
            errors.append(f"{item.path}: tracked file is missing from the working tree")
            continue

        attrs = attributes.get(item.path, {})
        text_attr = attrs.get("text", "unspecified")
        expected_eol = attrs.get("eol", "unspecified")
        data = target.read_bytes()

        if text_attr == "unset":
            skipped_binary += 1
            continue

        if b"\0" in data:
            errors.append(f"{item.path}: binary-looking file must be declared -text")
            continue

        if expected_eol not in {"lf", "crlf"}:
            errors.append(
                f"{item.path}: text file has no explicit LF/CRLF policy "
                f"(text={text_attr}, eol={expected_eol})"
            )
            continue

        try:
            data.decode("utf-8")
        except UnicodeDecodeError as error:
            errors.append(
                f"{item.path}: text file is not valid UTF-8 at byte {error.start}; "
                "declare real binary files -text"
            )
            continue

        checked_text += 1
        if data.startswith(UTF8_BOM):
            errors.append(f"{item.path}: UTF-8 BOM is not allowed")

        problem = newline_problem(data, expected_eol)
        if problem and args.fix:
            normalized = normalize_newlines(data, expected_eol)
            if normalized != data:
                target.write_bytes(normalized)
                data = normalized
                fixed += 1
            problem = newline_problem(data, expected_eol)
        if problem:
            errors.append(f"{item.path}: {problem}")

    for error in errors[: args.max_errors]:
        print(f"ERROR: {error}", file=sys.stderr)
    if len(errors) > args.max_errors:
        print(
            f"ERROR: {len(errors) - args.max_errors} additional issue(s) omitted",
            file=sys.stderr,
        )

    summary = (
        f"CampusOS line ending policy: text={checked_text}, "
        f"binary={skipped_binary}, fixed={fixed}, errors={len(errors)}, "
        f"platform={sys.platform}"
    )
    if errors:
        print(summary, file=sys.stderr)
        return 1
    print(summary)
    return 0


if __name__ == "__main__":
    sys.exit(main())
