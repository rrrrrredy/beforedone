#!/usr/bin/env python3
"""Build and verify the skills-only OpenAI Plugin Directory submission ZIP."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
import zipfile
from pathlib import Path, PurePosixPath


SEMVER = re.compile(
    r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)"
    r"(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"
    r"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$"
)
SKILLS = ("investigate-agent-incident", "verify-before-done")
FIXED_ZIP_TIME = (1980, 1, 1, 0, 0, 0)
TEXT_SUFFIXES = {".json", ".md", ".txt", ".yaml", ".yml"}


class PackageError(Exception):
    pass


def fail(message: str) -> None:
    raise PackageError(message)


def directory_manifest(version: str) -> dict:
    return {
        "name": "beforedone",
        "version": version,
        "description": (
            "Run evidence-based completion checks and reconstruct coding-agent "
            "incidents from observable artifacts."
        ),
        "author": {
            "name": "Song Luo",
            "url": "https://github.com/rrrrrredy",
        },
        "homepage": "https://rrrrrredy.github.io/beforedone/",
        "repository": "https://github.com/rrrrrredy/beforedone",
        "license": "Apache-2.0",
        "keywords": [
            "codex",
            "coding-agents",
            "verification",
            "incident-analysis",
            "replay",
        ],
        "skills": "./skills/",
        "interface": {
            "displayName": "BeforeDone",
            "shortDescription": "Verify agent work and investigate failed runs",
            "longDescription": (
                "BeforeDone provides two manual workflows backed by the open-source "
                "BeforeDone CLI: verify completion evidence against the current Git "
                "state, and reconstruct failed coding-agent runs from local observable "
                "events, receipts, logs, diffs, and user corrections. The CLI is a "
                "separate prerequisite and is never downloaded automatically."
            ),
            "developerName": "Song Luo",
            "category": "Developer Tools",
            "capabilities": ["Interactive", "Write"],
            "websiteURL": "https://rrrrrredy.github.io/beforedone/",
            "privacyPolicyURL": "https://rrrrrredy.github.io/beforedone/privacy.html",
            "termsOfServiceURL": "https://rrrrrredy.github.io/beforedone/terms.html",
            "defaultPrompt": [
                "Verify this coding task with BeforeDone before reporting the result.",
                "Investigate this failed coding-agent run with BeforeDone.",
            ],
            "brandColor": "#E54836",
        },
    }


def source_version(root: Path) -> str:
    path = root / "plugins" / "beforedone" / ".codex-plugin" / "plugin.json"
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        fail(f"cannot read full plugin version from {path}: {exc}")
    version = value.get("version") if isinstance(value, dict) else None
    if not isinstance(version, str) or not SEMVER.fullmatch(version):
        fail(f"{path}: version must be strict SemVer")
    return version


def normalized_bytes(path: Path) -> bytes:
    data = path.read_bytes()
    if path.suffix.lower() in TEXT_SUFFIXES:
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError as exc:
            fail(f"{path}: expected UTF-8 text: {exc}")
        return text.replace("\r\n", "\n").replace("\r", "\n").encode("utf-8")
    return data


def skill_files(root: Path) -> list[tuple[str, bytes]]:
    result: list[tuple[str, bytes]] = []
    for skill in SKILLS:
        skill_root = root / "skills" / skill
        skill_md = skill_root / "SKILL.md"
        metadata = skill_root / "agents" / "openai.yaml"
        if not skill_md.is_file() or not metadata.is_file():
            fail(f"{skill_root}: SKILL.md and agents/openai.yaml are required")
        text = skill_md.read_text(encoding="utf-8").lower()
        if "standalone skill as a manual workflow" not in text:
            fail(f"{skill_md}: Directory skill must disclose that it is a manual workflow")
        if skill == "verify-before-done" and "cannot install a stop hook" not in text:
            fail(f"{skill_md}: missing manual-only Stop boundary")
        if skill == "investigate-agent-incident" and "does not capture lifecycle events or enforce stop" not in text:
            fail(f"{skill_md}: missing manual-only incident boundary")

        for path in sorted(skill_root.rglob("*")):
            if path.is_symlink():
                fail(f"{path}: symlinks are not allowed in the Directory bundle")
            if not path.is_file():
                continue
            relative = path.relative_to(skill_root).as_posix()
            parts = PurePosixPath(relative).parts
            if any(part in {"__pycache__", "hooks", "scripts"} for part in parts):
                fail(f"{path}: hooks, scripts, and cache files are not allowed in this bundle")
            if path.name in {".mcp.json", ".app.json"}:
                fail(f"{path}: MCP and app manifests are not allowed in this bundle")
            result.append((f"skills/{skill}/{relative}", normalized_bytes(path)))
    return result


def zip_info(name: str) -> zipfile.ZipInfo:
    info = zipfile.ZipInfo(name, FIXED_ZIP_TIME)
    info.create_system = 3
    info.external_attr = 0o100644 << 16
    info.compress_type = zipfile.ZIP_DEFLATED
    return info


def build(root: Path, version: str, output_dir: Path) -> tuple[Path, Path]:
    manifest = directory_manifest(version)
    manifest_text = json.dumps(manifest, indent=2, ensure_ascii=False) + "\n"
    lowered = manifest_text.lower()
    for forbidden in ("hook", "enforc", "stop gate", "block completion"):
        if forbidden in lowered:
            fail(f"Directory manifest must not advertise lifecycle controls ({forbidden!r})")

    entries = [
        (".codex-plugin/plugin.json", manifest_text.encode("utf-8")),
        *skill_files(root),
    ]
    names = [name for name, _data in entries]
    if names != sorted(names):
        entries.sort(key=lambda item: item[0])

    output_dir.mkdir(parents=True, exist_ok=True)
    archive = output_dir / f"beforedone-openai-directory-skills-v{version}.zip"
    with zipfile.ZipFile(
        archive,
        mode="w",
        compression=zipfile.ZIP_DEFLATED,
        compresslevel=9,
        strict_timestamps=True,
    ) as handle:
        for name, data in entries:
            handle.writestr(zip_info(name), data, compresslevel=9)

    verify_archive(archive, version)
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    checksum = archive.with_suffix(archive.suffix + ".sha256")
    checksum.write_text(f"{digest}  {archive.name}\n", encoding="ascii", newline="\n")
    return archive, checksum


def verify_archive(path: Path, version: str) -> None:
    expected_manifest = directory_manifest(version)
    expected_prefixes = {f"skills/{skill}/" for skill in SKILLS}
    try:
        with zipfile.ZipFile(path) as handle:
            infos = handle.infolist()
            names = [info.filename for info in infos]
            if len(names) != len(set(names)):
                fail(f"{path}: duplicate ZIP entries")
            if names != sorted(names):
                fail(f"{path}: ZIP entries must be sorted for reproducibility")
            for info in infos:
                name = PurePosixPath(info.filename)
                if name.is_absolute() or ".." in name.parts or "\\" in info.filename:
                    fail(f"{path}: unsafe ZIP entry {info.filename!r}")
                if info.date_time != FIXED_ZIP_TIME:
                    fail(f"{path}: non-reproducible timestamp on {info.filename}")
                if any(part in {"hooks", "scripts"} for part in name.parts):
                    fail(f"{path}: forbidden lifecycle or script entry {info.filename}")
                if name.name in {".mcp.json", ".app.json"}:
                    fail(f"{path}: forbidden MCP/app entry {info.filename}")
            if ".codex-plugin/plugin.json" not in names:
                fail(f"{path}: missing .codex-plugin/plugin.json")
            for prefix in expected_prefixes:
                if not any(name.startswith(prefix) for name in names):
                    fail(f"{path}: missing skill tree {prefix}")
            unexpected = [
                name
                for name in names
                if name != ".codex-plugin/plugin.json"
                and not any(name.startswith(prefix) for prefix in expected_prefixes)
            ]
            if unexpected:
                fail(f"{path}: unexpected files outside the skills-only layout: {unexpected}")
            manifest = json.loads(handle.read(".codex-plugin/plugin.json"))
    except (OSError, zipfile.BadZipFile, KeyError, json.JSONDecodeError) as exc:
        fail(f"{path}: invalid Directory ZIP: {exc}")
    if manifest != expected_manifest:
        fail(f"{path}: generated Directory manifest does not match the versioned contract")
    if set(manifest).intersection({"hooks", "mcpServers", "apps"}):
        fail(f"{path}: Directory manifest must be skills-only")
    if manifest.get("skills") != "./skills/":
        fail(f"{path}: Directory manifest must reference only ./skills/")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--version", help="strict SemVer; defaults to the full plugin version")
    parser.add_argument("--output-dir", type=Path)
    parser.add_argument("--verify", type=Path, help="verify an existing ZIP instead of building")
    args = parser.parse_args()
    try:
        root = args.root.resolve()
        version = args.version or source_version(root)
        if not SEMVER.fullmatch(version):
            fail(f"version {version!r} is not strict SemVer")
        full_version = source_version(root)
        if version != full_version:
            fail(
                f"Directory version {version!r} does not match full plugin version "
                f"{full_version!r}"
            )
        if args.verify:
            verify_archive(args.verify, version)
            print(f"validated skills-only Directory bundle: {args.verify}")
        else:
            output_dir = args.output_dir or root / "dist" / "directory"
            archive, checksum = build(root, version, output_dir)
            print(f"created {archive}")
            print(f"created {checksum}")
    except PackageError as exc:
        print(f"Directory bundle error: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
