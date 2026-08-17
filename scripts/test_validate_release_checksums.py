#!/usr/bin/env python3
"""Regression tests for the formal release checksum contract."""

from __future__ import annotations

import re
import unittest

from validate_release_checksums import (
    ContractError,
    expected_checksum_names,
    validate_checksum_lines,
)


TAG = "v1.1.0"


def valid_lines() -> list[str]:
    return [f"{'0' * 64}  {name}\n" for name in sorted(expected_checksum_names(TAG))]


class ReleaseChecksumContractTests(unittest.TestCase):
    def test_accepts_exact_archive_and_sbom_set(self) -> None:
        entries = validate_checksum_lines(valid_lines(), TAG)
        self.assertEqual(set(entries), set(expected_checksum_names(TAG)))
        self.assertEqual(len(entries), 12)

    def test_rejects_missing_asset(self) -> None:
        lines = valid_lines()
        missing_name = lines.pop().split("  ", 1)[1].strip()
        with self.assertRaisesRegex(ContractError, rf"missing: .*{re.escape(missing_name)}"):
            validate_checksum_lines(lines, TAG)

    def test_rejects_unexpected_asset(self) -> None:
        lines = valid_lines() + [f"{'1' * 64}  unexpected.zip\n"]
        with self.assertRaisesRegex(ContractError, r"unexpected: unexpected\.zip"):
            validate_checksum_lines(lines, TAG)

    def test_rejects_duplicate_asset(self) -> None:
        lines = valid_lines()
        lines.append(lines[0])
        with self.assertRaisesRegex(ContractError, "duplicate checksum entry"):
            validate_checksum_lines(lines, TAG)

    def test_rejects_malformed_checksum_line(self) -> None:
        lines = valid_lines()
        lines[0] = f"{'0' * 63}  malformed.tar.gz\n"
        with self.assertRaisesRegex(ContractError, "checksum line 1"):
            validate_checksum_lines(lines, TAG)

    def test_rejects_nested_asset_path_before_hashing(self) -> None:
        lines = valid_lines()
        lines[0] = f"{'0' * 64}  ../outside.tar.gz\n"
        with self.assertRaisesRegex(ContractError, "flat asset name"):
            validate_checksum_lines(lines, TAG)

    def test_rejects_non_final_release_tag(self) -> None:
        with self.assertRaisesRegex(ContractError, "is not final SemVer"):
            validate_checksum_lines(valid_lines(), "v1.1.0-rc.1")


if __name__ == "__main__":
    unittest.main()
