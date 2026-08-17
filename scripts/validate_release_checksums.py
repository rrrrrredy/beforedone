#!/usr/bin/env python3
"""Validate the exact checksum contract for a formal BeforeDone release."""

from __future__ import annotations

import argparse
import re
import sys
from collections.abc import Iterable
from pathlib import Path


FINAL_TAG_RE = re.compile(r"^v(?P<version>[0-9]+\.[0-9]+\.[0-9]+)$")
CHECKSUM_LINE_RE = re.compile(
    r"^(?P<digest>[0-9a-f]{64})  (?P<name>[^/\\\s][^/\\]*)$"
)
ARCHIVE_TEMPLATES = (
    "beforedone_{version}_darwin_amd64.tar.gz",
    "beforedone_{version}_darwin_arm64.tar.gz",
    "beforedone_{version}_linux_amd64.tar.gz",
    "beforedone_{version}_linux_arm64.tar.gz",
    "beforedone_{version}_windows_amd64.zip",
    "beforedone_{version}_windows_arm64.zip",
)


class ContractError(ValueError):
    """The checksum manifest does not match the formal release contract."""


def release_version(tag: str) -> str:
    match = FINAL_TAG_RE.fullmatch(tag)
    if match is None:
        raise ContractError(
            f"release tag {tag!r} is not final SemVer such as v1.0.0"
        )
    return match.group("version")


def expected_checksum_names(tag: str) -> frozenset[str]:
    version = release_version(tag)
    archives = {
        template.format(version=version) for template in ARCHIVE_TEMPLATES
    }
    return frozenset(archives | {f"{archive}.sbom.spdx.json" for archive in archives})


def parse_checksum_lines(lines: Iterable[str]) -> dict[str, str]:
    entries: dict[str, str] = {}
    for line_number, raw_line in enumerate(lines, start=1):
        line = raw_line.rstrip("\r\n")
        if not line:
            continue
        match = CHECKSUM_LINE_RE.fullmatch(line)
        if match is None:
            raise ContractError(
                f"checksum line {line_number} must contain a lowercase SHA-256, "
                "two spaces, and a flat asset name"
            )
        name = match.group("name")
        if name in entries:
            raise ContractError(f"duplicate checksum entry for {name!r}")
        entries[name] = match.group("digest")
    return entries


def validate_checksum_lines(lines: Iterable[str], tag: str) -> dict[str, str]:
    entries = parse_checksum_lines(lines)
    expected = expected_checksum_names(tag)
    actual = set(entries)
    missing = sorted(expected - actual)
    unexpected = sorted(actual - expected)
    if missing or unexpected:
        details: list[str] = []
        if missing:
            details.append("missing: " + ", ".join(missing))
        if unexpected:
            details.append("unexpected: " + ", ".join(unexpected))
        raise ContractError("checksum asset set mismatch (" + "; ".join(details) + ")")
    return entries


def validate_checksum_file(path: Path, tag: str) -> dict[str, str]:
    try:
        lines = path.read_text(encoding="utf-8").splitlines(keepends=True)
    except OSError as error:
        raise ContractError(f"cannot read checksum manifest {path}: {error}") from error
    return validate_checksum_lines(lines, tag)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--checksums", type=Path, required=True)
    parser.add_argument("--tag", required=True)
    args = parser.parse_args(argv)

    try:
        entries = validate_checksum_file(args.checksums, args.tag)
    except ContractError as error:
        print(f"release checksum contract failed: {error}", file=sys.stderr)
        return 1

    print(f"validated {len(entries)} release checksums for {args.tag}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
