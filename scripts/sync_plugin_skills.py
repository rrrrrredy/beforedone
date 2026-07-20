#!/usr/bin/env python3
"""Mirror canonical standalone skills into the BeforeDone plugin."""

from __future__ import annotations

import argparse
import hashlib
import shutil
import sys
import uuid
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parent.parent
SOURCE = REPO_ROOT / "skills"
PLUGIN_ROOT = REPO_ROOT / "plugins" / "beforedone"
TARGET = PLUGIN_ROOT / "skills"
REQUIRED_SKILLS = ("verify-before-done", "investigate-agent-incident")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Synchronize canonical skills into plugins/beforedone/skills."
    )
    parser.add_argument(
        "--check",
        action="store_true",
        help="Fail if the plugin copy differs; do not modify files.",
    )
    return parser.parse_args()


def digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def snapshot(root: Path) -> dict[str, tuple[str, str]]:
    if not root.is_dir():
        return {}
    result: dict[str, tuple[str, str]] = {}
    for path in sorted(root.rglob("*")):
        relative = path.relative_to(root).as_posix()
        if path.is_symlink():
            result[relative] = ("symlink", str(path.readlink()))
        elif path.is_dir():
            result[relative] = ("directory", "")
        elif path.is_file():
            result[relative] = ("file", digest(path))
        else:
            result[relative] = ("other", "")
    return result


def differences() -> list[str]:
    source = snapshot(SOURCE)
    target = snapshot(TARGET)
    messages: list[str] = []
    for skill_name in REQUIRED_SKILLS:
        relative = f"{skill_name}/SKILL.md"
        if not (SOURCE / skill_name / "SKILL.md").is_file():
            messages.append(f"required canonical skill is missing: {relative}")
        if not (TARGET / skill_name / "SKILL.md").is_file():
            messages.append(f"required plugin skill is missing: {relative}")
    for relative in sorted(source.keys() | target.keys()):
        if relative not in target:
            messages.append(f"missing from plugin: {relative}")
        elif relative not in source:
            messages.append(f"extra in plugin: {relative}")
        elif source[relative] != target[relative]:
            messages.append(f"content or type differs: {relative}")
    return messages


def assert_safe_paths() -> None:
    expected_plugin = (REPO_ROOT / "plugins" / "beforedone").resolve()
    if PLUGIN_ROOT.resolve() != expected_plugin:
        raise RuntimeError("refusing to operate outside plugins/beforedone")
    if TARGET.resolve().parent != expected_plugin:
        raise RuntimeError("refusing to replace a non-plugin skills directory")
    if SOURCE.resolve().parent != REPO_ROOT.resolve():
        raise RuntimeError("canonical skills directory is outside the repository root")


def sync() -> None:
    assert_safe_paths()
    if not SOURCE.is_dir():
        raise RuntimeError(f"canonical skills directory is missing: {SOURCE}")
    missing = [
        name for name in REQUIRED_SKILLS if not (SOURCE / name / "SKILL.md").is_file()
    ]
    if missing:
        raise RuntimeError(f"required canonical skills are missing: {', '.join(missing)}")

    staging = PLUGIN_ROOT / f".skills-sync-{uuid.uuid4().hex}"
    try:
        shutil.copytree(SOURCE, staging, symlinks=True)
        if TARGET.exists():
            shutil.rmtree(TARGET)
        staging.rename(TARGET)
    finally:
        if staging.exists():
            shutil.rmtree(staging)


def main() -> int:
    args = parse_args()
    if args.check:
        drift = differences()
        if drift:
            print("Plugin skill drift detected:", file=sys.stderr)
            for message in drift:
                print(f"- {message}", file=sys.stderr)
            print(
                "Run `python scripts/sync_plugin_skills.py` to refresh the plugin copy.",
                file=sys.stderr,
            )
            return 1
        print("Plugin skills match canonical skills.")
        return 0

    sync()
    drift = differences()
    if drift:
        print("Skill synchronization did not converge.", file=sys.stderr)
        for message in drift:
            print(f"- {message}", file=sys.stderr)
        return 1
    print("Synchronized canonical skills into plugins/beforedone/skills.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
