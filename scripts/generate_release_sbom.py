#!/usr/bin/env python3
"""Generate a path-neutral SPDX 2.3 SBOM for a release archive."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import stat
import subprocess
import sys
import tarfile
import tempfile
import zipfile
from collections.abc import Iterable
from pathlib import Path, PurePosixPath
from typing import Any


WINDOWS_PATH_RE = re.compile(r"(?:^|[\s=\"'(])(?:[A-Za-z]:[\\/]|\\\\[^\\]+\\)")
WINDOWS_DRIVE_MEMBER_RE = re.compile(r"^[A-Za-z]:")
POSIX_HOST_PATH_RE = re.compile(r"(?:^|[\s=\"'(])/(?:home|Users|private|tmp)/")
FORBIDDEN_MARKERS = (
    "appdata\\local\\temp",
    "appdata/local/temp",
    "syft-archive-contents",
)
NORMALIZER_CREATOR = "Tool: beforedone-release-sbom-wrapper-1"


class SbomError(ValueError):
    """The generated SBOM or source archive violates the release contract."""


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def _safe_member_name(raw_name: str) -> PurePosixPath:
    normalized = raw_name.replace("\\", "/")
    path = PurePosixPath(normalized)
    if (
        not normalized
        or path.is_absolute()
        or WINDOWS_DRIVE_MEMBER_RE.match(normalized)
        or ".." in path.parts
    ):
        raise SbomError(f"archive member escapes extraction root: {raw_name!r}")
    return path


def _safe_destination(root: Path, member: PurePosixPath) -> Path:
    destination = (root / Path(*member.parts)).resolve()
    try:
        destination.relative_to(root.resolve())
    except ValueError as error:
        raise SbomError(f"archive member escapes extraction root: {member}") from error
    return destination


def _extract_tar(archive: Path, root: Path) -> None:
    with tarfile.open(archive, mode="r:*") as handle:
        members = handle.getmembers()
        for member in members:
            relative = _safe_member_name(member.name)
            _safe_destination(root, relative)
            if not (member.isdir() or member.isfile()):
                raise SbomError(
                    f"archive member must be a regular file or directory: {member.name!r}"
                )
        handle.extractall(root, members=members, filter="data")


def _extract_zip(archive: Path, root: Path) -> None:
    with zipfile.ZipFile(archive) as handle:
        for member in handle.infolist():
            relative = _safe_member_name(member.filename)
            _safe_destination(root, relative)
            unix_mode = (member.external_attr >> 16) & 0xFFFF
            if stat.S_ISLNK(unix_mode):
                raise SbomError(f"archive symlink is forbidden: {member.filename!r}")
        handle.extractall(root)


def extract_archive(archive: Path, root: Path) -> None:
    if archive.name.endswith(".tar.gz"):
        _extract_tar(archive, root)
        return
    if archive.suffix == ".zip":
        _extract_zip(archive, root)
        return
    raise SbomError(f"unsupported release archive: {archive.name!r}")


def _walk_strings(value: Any) -> Iterable[str]:
    if isinstance(value, str):
        yield value
    elif isinstance(value, dict):
        for key, item in value.items():
            yield str(key)
            yield from _walk_strings(item)
    elif isinstance(value, list):
        for item in value:
            yield from _walk_strings(item)


def assert_path_neutral(document: dict[str, Any], forbidden_roots: Iterable[Path]) -> None:
    roots = tuple(str(path.resolve()).casefold() for path in forbidden_roots)
    for value in _walk_strings(document):
        folded = value.casefold()
        if any(root and root in folded for root in roots):
            raise SbomError("SBOM contains its host extraction path")
        if WINDOWS_PATH_RE.search(value) or POSIX_HOST_PATH_RE.search(value):
            raise SbomError(f"SBOM contains an absolute host path: {value!r}")
        if any(marker in folded for marker in FORBIDDEN_MARKERS):
            raise SbomError(f"SBOM contains a temporary host path: {value!r}")


def _normalize_internal_path(value: str) -> str:
    normalized = value.replace("\\", "/")
    return "/" + normalized.lstrip("/")


def normalize_document(
    document: dict[str, Any],
    *,
    artifact_name: str,
    artifact_sha256: str,
    forbidden_roots: Iterable[Path] = (),
) -> dict[str, Any]:
    if document.get("spdxVersion") != "SPDX-2.3":
        raise SbomError("Syft did not produce an SPDX 2.3 document")

    assert_path_neutral(document, forbidden_roots)

    packages = document.get("packages")
    if not isinstance(packages, list):
        raise SbomError("SPDX document must contain a package list")

    roots = [
        package
        for package in packages
        if isinstance(package, dict) and package.get("name") == artifact_name
    ]
    if len(roots) != 1:
        raise SbomError(
            f"SPDX document must contain exactly one root package for {artifact_name!r}"
        )
    root = roots[0]
    root["versionInfo"] = f"sha256:{artifact_sha256}"
    root["checksums"] = [
        {"algorithm": "SHA256", "checksumValue": artifact_sha256}
    ]
    root["primaryPackagePurpose"] = "FILE"

    for package in packages:
        if not isinstance(package, dict):
            continue
        name = package.get("name")
        if isinstance(name, str) and name.startswith("\\") and not name.startswith("\\\\"):
            package["name"] = _normalize_internal_path(name)
        source_info = package.get("sourceInfo")
        if isinstance(source_info, str):
            prefix, separator, location = source_info.rpartition(": ")
            if separator and location.startswith(("\\", "/")):
                package["sourceInfo"] = (
                    prefix + separator + _normalize_internal_path(location)
                )

    files = document.get("files", [])
    if not isinstance(files, list):
        raise SbomError("SPDX files field must be a list when present")
    for item in files:
        if not isinstance(item, dict):
            continue
        file_name = item.get("fileName")
        if isinstance(file_name, str) and file_name.startswith(("\\", "/")):
            item["fileName"] = _normalize_internal_path(file_name)

    creation_info = document.get("creationInfo")
    if not isinstance(creation_info, dict):
        raise SbomError("SPDX document must contain creationInfo")
    creators = creation_info.get("creators")
    if not isinstance(creators, list):
        raise SbomError("SPDX creationInfo must contain a creator list")
    if NORMALIZER_CREATOR not in creators:
        creators.append(NORMALIZER_CREATOR)

    assert_path_neutral(document, forbidden_roots)
    return document


def generate_sbom(artifact: Path, document: Path, syft: str) -> None:
    artifact = artifact.resolve()
    document = document.resolve()
    if not artifact.is_file():
        raise SbomError(f"release archive does not exist: {artifact}")
    if document == artifact:
        raise SbomError("SBOM document must not overwrite its source archive")
    document.parent.mkdir(parents=True, exist_ok=True)
    artifact_sha256 = sha256_file(artifact)

    with tempfile.TemporaryDirectory(prefix="beforedone-release-sbom-") as temporary:
        temporary_root = Path(temporary).resolve()
        extraction_root = temporary_root / "contents"
        extraction_root.mkdir()
        extract_archive(artifact, extraction_root)
        raw_document = temporary_root / "syft.spdx.json"

        subprocess.run(
            [
                syft,
                "scan",
                f"dir:{extraction_root}",
                "--base-path",
                str(extraction_root),
                "--source-name",
                artifact.name,
                "--source-version",
                f"sha256:{artifact_sha256}",
                "--output",
                f"spdx-json={raw_document}",
            ],
            check=True,
            shell=False,
        )

        try:
            parsed = json.loads(raw_document.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as error:
            raise SbomError(f"cannot read Syft SPDX output: {error}") from error
        if not isinstance(parsed, dict):
            raise SbomError("Syft SPDX output must be a JSON object")

        normalized = normalize_document(
            parsed,
            artifact_name=artifact.name,
            artifact_sha256=artifact_sha256,
            forbidden_roots=(temporary_root, extraction_root),
        )
        encoded = json.dumps(
            normalized, ensure_ascii=False, separators=(",", ":")
        ) + "\n"
        staged = document.with_name(document.name + ".tmp")
        staged.write_text(encoded, encoding="utf-8", newline="\n")
        os.replace(staged, document)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifact", type=Path, required=True)
    parser.add_argument("--document", type=Path, required=True)
    parser.add_argument("--syft", default="syft")
    args = parser.parse_args(argv)

    try:
        generate_sbom(args.artifact, args.document, args.syft)
    except (SbomError, OSError, subprocess.CalledProcessError) as error:
        print(f"release SBOM generation failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
