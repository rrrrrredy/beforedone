#!/usr/bin/env python3
"""Regression tests for path-neutral formal release SBOM generation."""

from __future__ import annotations

import json
import tarfile
import tempfile
import unittest
import zipfile
from pathlib import Path

from generate_release_sbom import (
    NORMALIZER_CREATOR,
    SbomError,
    assert_path_neutral,
    extract_archive,
    normalize_document,
    sha256_file,
)


ARTIFACT = "beforedone_1.1.1_linux_amd64.tar.gz"
DIGEST = "a" * 64


def valid_document() -> dict[str, object]:
    return {
        "spdxVersion": "SPDX-2.3",
        "SPDXID": "SPDXRef-DOCUMENT",
        "creationInfo": {"creators": ["Tool: syft-1.44.0"]},
        "packages": [
            {
                "name": "github.com/rrrrrredy/beforedone",
                "SPDXID": "SPDXRef-Package-beforedone",
                "sourceInfo": (
                    "acquired package info from go module information: \\beforedone"
                ),
            },
            {
                "name": ARTIFACT,
                "SPDXID": "SPDXRef-DocumentRoot",
                "primaryPackagePurpose": "FILE",
            },
        ],
        "files": [
            {
                "fileName": "\\beforedone",
                "SPDXID": "SPDXRef-File-beforedone",
            }
        ],
        "relationships": [
            {
                "spdxElementId": "SPDXRef-DOCUMENT",
                "relatedSpdxElement": "SPDXRef-DocumentRoot",
                "relationshipType": "DESCRIBES",
            }
        ],
    }


class ReleaseSbomTests(unittest.TestCase):
    def test_normalizes_internal_paths_and_binds_archive_digest(self) -> None:
        document = normalize_document(
            valid_document(), artifact_name=ARTIFACT, artifact_sha256=DIGEST
        )
        root = next(
            package
            for package in document["packages"]
            if package["name"] == ARTIFACT
        )
        self.assertEqual(root["versionInfo"], f"sha256:{DIGEST}")
        self.assertEqual(
            root["checksums"],
            [{"algorithm": "SHA256", "checksumValue": DIGEST}],
        )
        self.assertEqual(document["files"][0]["fileName"], "/beforedone")
        self.assertEqual(
            document["packages"][0]["sourceInfo"],
            "acquired package info from go module information: /beforedone",
        )
        self.assertIn(
            NORMALIZER_CREATOR, document["creationInfo"]["creators"]
        )

    def test_rejects_absolute_host_paths(self) -> None:
        document = valid_document()
        document["packages"][0]["sourceInfo"] = (
            "acquired package info from go module information: "
            "C:\\Users\\builder\\AppData\\Local\\Temp\\beforedone"
        )
        with self.assertRaisesRegex(SbomError, "absolute host path"):
            assert_path_neutral(document, ())

    def test_rejects_missing_document_root(self) -> None:
        document = valid_document()
        document["packages"] = document["packages"][:1]
        with self.assertRaisesRegex(SbomError, "exactly one root package"):
            normalize_document(
                document, artifact_name=ARTIFACT, artifact_sha256=DIGEST
            )

    def test_rejects_zip_path_traversal(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root / "malicious.zip"
            with zipfile.ZipFile(archive, "w") as handle:
                handle.writestr("../escape.txt", "unsafe")
            destination = root / "extract"
            destination.mkdir()
            with self.assertRaisesRegex(SbomError, "escapes extraction root"):
                extract_archive(archive, destination)
            self.assertFalse((root / "escape.txt").exists())

    def test_rejects_windows_drive_qualified_member(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root / "malicious.zip"
            with zipfile.ZipFile(archive, "w") as handle:
                handle.writestr("C:/escape.txt", "unsafe")
            destination = root / "extract"
            destination.mkdir()
            with self.assertRaisesRegex(SbomError, "escapes extraction root"):
                extract_archive(archive, destination)

    def test_rejects_tar_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root / "malicious.tar.gz"
            with tarfile.open(archive, "w:gz") as handle:
                member = tarfile.TarInfo("beforedone")
                member.type = tarfile.SYMTYPE
                member.linkname = "outside"
                handle.addfile(member)
            destination = root / "extract"
            destination.mkdir()
            with self.assertRaisesRegex(SbomError, "regular file or directory"):
                extract_archive(archive, destination)

    def test_sha256_file_matches_known_content(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "content"
            path.write_bytes(b"beforedone\n")
            self.assertEqual(
                sha256_file(path),
                "bcd3f638a03516fc8c6937eaf3e2a1f46ae8e11e54fbf6434029cb34aa195378",
            )

    def test_normalized_document_serializes_without_host_markers(self) -> None:
        document = normalize_document(
            valid_document(), artifact_name=ARTIFACT, artifact_sha256=DIGEST
        )
        serialized = json.dumps(document)
        self.assertNotIn("Users", serialized)
        self.assertNotIn("AppData", serialized)
        self.assertNotIn("syft-archive-contents", serialized)


if __name__ == "__main__":
    unittest.main()
