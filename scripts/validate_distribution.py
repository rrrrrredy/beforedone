#!/usr/bin/env python3
"""Validate the checked-in Codex plugin, marketplace, hooks, and skills contract."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from urllib.parse import urlsplit


SEMVER = re.compile(
    r"^(0|[1-9]\d*)\."
    r"(0|[1-9]\d*)\."
    r"(0|[1-9]\d*)"
    r"(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)"
    r"(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?"
    r"(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$"
)
KEBAB = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
HEX_COLOR = re.compile(r"^#[0-9A-Fa-f]{6}$")
REQUIRED_HOOK_EVENTS = {
    "SessionStart",
    "UserPromptSubmit",
    "PreToolUse",
    "PostToolUse",
    "SubagentStart",
    "SubagentStop",
    "Stop",
}
UNIX_HOOK_COMMAND = '/bin/sh "${PLUGIN_ROOT}/scripts/codex-hook.sh"'
WINDOWS_HOOK_COMMAND = (
    "Set-ExecutionPolicy -Scope Process Bypass -Force; "
    "& ([IO.Path]::Combine($env:PLUGIN_ROOT, 'scripts', 'codex-hook.ps1'))"
)


class ValidationError(Exception):
    pass


def fail(message: str) -> None:
    raise ValidationError(message)


def load_json(path: Path) -> dict:
    def unique_object(pairs: list[tuple[str, object]]) -> dict:
        value: dict[str, object] = {}
        for key, item in pairs:
            if key in value:
                fail(f"{path}: duplicate JSON key {key!r}")
            value[key] = item
        return value

    try:
        value = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique_object)
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        fail(f"{path}: cannot read valid JSON: {exc}")
    if not isinstance(value, dict):
        fail(f"{path}: root must be a JSON object")
    return value


def require_string(value: object, label: str) -> str:
    if not isinstance(value, str) or not value.strip():
        fail(f"{label} must be a non-empty string")
    return value.strip()


def require_https(value: object, label: str) -> str:
    url = require_string(value, label)
    parsed = urlsplit(url)
    if parsed.scheme != "https" or not parsed.netloc or parsed.username or parsed.password:
        fail(f"{label} must be an absolute https:// URL without embedded credentials")
    return url


def safe_reference(root: Path, raw: object, label: str, *, directory: bool = False) -> Path:
    value = require_string(raw, label)
    if not value.startswith("./") or "\\" in value:
        fail(f"{label} must be a forward-slash relative path beginning with ./")
    candidate = (root / value[2:]).resolve()
    try:
        candidate.relative_to(root.resolve())
    except ValueError:
        fail(f"{label} escapes the plugin or skill root")
    if directory:
        if not candidate.is_dir():
            fail(f"{label} references missing directory {value!r}")
    elif not candidate.is_file():
        fail(f"{label} references missing file {value!r}")
    return candidate


def validate_plugin(root: Path, expected_version: str | None) -> tuple[str, str, Path]:
    plugin_root = root / "plugins" / "beforedone"
    manifest_path = plugin_root / ".codex-plugin" / "plugin.json"
    manifest = load_json(manifest_path)

    if "[TODO:" in manifest_path.read_text(encoding="utf-8"):
        fail(f"{manifest_path}: unresolved TODO placeholder")
    name = require_string(manifest.get("name"), f"{manifest_path}: name")
    if not KEBAB.fullmatch(name) or name != plugin_root.name:
        fail(f"{manifest_path}: name must be kebab-case and match the plugin folder")
    version = require_string(manifest.get("version"), f"{manifest_path}: version")
    if not SEMVER.fullmatch(version):
        fail(f"{manifest_path}: version {version!r} is not strict SemVer")
    if expected_version is not None and version != expected_version:
        fail(
            f"{manifest_path}: version {version!r} does not match release version "
            f"{expected_version!r}"
        )
    require_string(manifest.get("description"), f"{manifest_path}: description")
    author = manifest.get("author")
    if not isinstance(author, dict):
        fail(f"{manifest_path}: author must be an object")
    require_string(author.get("name"), f"{manifest_path}: author.name")
    if "url" in author:
        require_https(author["url"], f"{manifest_path}: author.url")
    require_https(manifest.get("homepage"), f"{manifest_path}: homepage")
    require_https(manifest.get("repository"), f"{manifest_path}: repository")
    require_string(manifest.get("license"), f"{manifest_path}: license")

    if "hooks" in manifest:
        fail(
            f"{manifest_path}: hooks is not a supported manifest field; "
            "use default hooks/hooks.json discovery"
        )
    if "skills" in manifest:
        safe_reference(plugin_root, manifest["skills"], f"{manifest_path}: skills", directory=True)
    for field in ("apps",):
        if field in manifest:
            safe_reference(plugin_root, manifest[field], f"{manifest_path}: {field}")
    if isinstance(manifest.get("mcpServers"), str):
        safe_reference(plugin_root, manifest["mcpServers"], f"{manifest_path}: mcpServers")

    interface = manifest.get("interface")
    if not isinstance(interface, dict):
        fail(f"{manifest_path}: interface must be an object")
    for field in (
        "displayName",
        "shortDescription",
        "longDescription",
        "developerName",
        "category",
    ):
        require_string(interface.get(field), f"{manifest_path}: interface.{field}")
    for field in ("websiteURL", "privacyPolicyURL", "termsOfServiceURL"):
        require_https(interface.get(field), f"{manifest_path}: interface.{field}")
    brand = require_string(interface.get("brandColor"), f"{manifest_path}: interface.brandColor")
    if not HEX_COLOR.fullmatch(brand):
        fail(f"{manifest_path}: interface.brandColor must be #RRGGBB")
    prompts = interface.get("defaultPrompt")
    if not isinstance(prompts, list) or not 1 <= len(prompts) <= 3:
        fail(f"{manifest_path}: interface.defaultPrompt must contain 1 to 3 prompts")
    for index, prompt in enumerate(prompts):
        text = require_string(prompt, f"{manifest_path}: interface.defaultPrompt[{index}]")
        if len(text) > 128:
            fail(f"{manifest_path}: interface.defaultPrompt[{index}] exceeds 128 characters")
    for field in ("composerIcon", "logo", "logoDark"):
        if field in interface:
            safe_reference(plugin_root, interface[field], f"{manifest_path}: interface.{field}")
    if "screenshots" in interface:
        screenshots = interface["screenshots"]
        if not isinstance(screenshots, list) or not screenshots:
            fail(f"{manifest_path}: interface.screenshots must be a non-empty array")
        for index, screenshot in enumerate(screenshots):
            path = safe_reference(
                plugin_root,
                screenshot,
                f"{manifest_path}: interface.screenshots[{index}]",
            )
            if path.suffix.lower() != ".png" or path.parent.name != "assets":
                fail(f"{manifest_path}: screenshots must be PNG files under ./assets/")

    return name, version, plugin_root


def validate_marketplace(root: Path, plugin_name: str, plugin_root: Path) -> None:
    path = root / ".agents" / "plugins" / "marketplace.json"
    marketplace = load_json(path)
    require_string(marketplace.get("name"), f"{path}: name")
    interface = marketplace.get("interface")
    if interface is not None:
        if not isinstance(interface, dict):
            fail(f"{path}: interface must be an object")
        require_string(interface.get("displayName"), f"{path}: interface.displayName")
    entries = marketplace.get("plugins")
    if not isinstance(entries, list) or not entries:
        fail(f"{path}: plugins must be a non-empty array")

    seen: set[str] = set()
    matched = 0
    for index, entry in enumerate(entries):
        label = f"{path}: plugins[{index}]"
        if not isinstance(entry, dict):
            fail(f"{label} must be an object")
        name = require_string(entry.get("name"), f"{label}.name")
        if name in seen:
            fail(f"{path}: duplicate plugin entry {name!r}")
        seen.add(name)
        source = entry.get("source")
        if not isinstance(source, dict) or source.get("source") != "local":
            fail(f"{label}.source must declare source=local")
        expected_path = f"./plugins/{name}"
        if source.get("path") != expected_path:
            fail(f"{label}.source.path must be {expected_path!r}")
        resolved = (root / source["path"][2:]).resolve()
        if resolved != (root / "plugins" / name).resolve():
            fail(f"{label}.source.path does not resolve to the named plugin folder")
        manifest_path = resolved / ".codex-plugin" / "plugin.json"
        manifest = load_json(manifest_path)
        if manifest.get("name") != name:
            fail(f"{label}.name does not match {manifest_path} name")
        policy = entry.get("policy")
        if not isinstance(policy, dict):
            fail(f"{label}.policy must be an object")
        if policy.get("installation") not in {
            "NOT_AVAILABLE",
            "AVAILABLE",
            "INSTALLED_BY_DEFAULT",
        }:
            fail(f"{label}.policy.installation is invalid")
        if policy.get("authentication") not in {"ON_INSTALL", "ON_USE"}:
            fail(f"{label}.policy.authentication is invalid")
        require_string(entry.get("category"), f"{label}.category")
        if name == plugin_name:
            matched += 1
            if resolved != plugin_root.resolve():
                fail(f"{label} does not reference the validated BeforeDone plugin")
    if matched != 1:
        fail(f"{path}: expected exactly one marketplace entry for {plugin_name!r}")


def validate_hooks(plugin_root: Path, plugin_version: str) -> None:
    path = plugin_root / "hooks" / "hooks.json"
    document = load_json(path)
    hooks = document.get("hooks")
    if not isinstance(hooks, dict):
        fail(f"{path}: hooks must be an object")
    found = set(hooks)
    if found != REQUIRED_HOOK_EVENTS:
        missing = sorted(REQUIRED_HOOK_EVENTS - found)
        extra = sorted(found - REQUIRED_HOOK_EVENTS)
        fail(f"{path}: lifecycle hook set mismatch; missing={missing}, extra={extra}")

    for event, groups in hooks.items():
        if not isinstance(groups, list) or len(groups) != 1:
            fail(f"{path}: hooks.{event} must contain exactly one group")
        group = groups[0]
        if not isinstance(group, dict):
            fail(f"{path}: hooks.{event}[0] must be an object")
        handlers = group.get("hooks")
        if not isinstance(handlers, list) or len(handlers) != 1:
            fail(f"{path}: hooks.{event}[0].hooks must contain exactly one handler")
        handler = handlers[0]
        if not isinstance(handler, dict) or handler.get("type") != "command":
            fail(f"{path}: hooks.{event} handler must have type=command")
        if handler.get("command") != UNIX_HOOK_COMMAND:
            fail(f"{path}: hooks.{event} must use the checked-in Unix wrapper command")
        if handler.get("commandWindows") != WINDOWS_HOOK_COMMAND:
            fail(f"{path}: hooks.{event} must use the checked-in Windows wrapper command")
        timeout = handler.get("timeout")
        if not isinstance(timeout, int) or isinstance(timeout, bool) or not 1 <= timeout <= 600:
            fail(f"{path}: hooks.{event} timeout must be an integer from 1 to 600")

    unix_wrapper = safe_reference(
        plugin_root,
        "./scripts/codex-hook.sh",
        f"{path}: Unix hook wrapper",
    )
    windows_wrapper = safe_reference(
        plugin_root,
        "./scripts/codex-hook.ps1",
        f"{path}: Windows hook wrapper",
    )
    for wrapper in (unix_wrapper, windows_wrapper):
        text = wrapper.read_text(encoding="utf-8")
        if "hook codex" not in text:
            fail(f"{wrapper}: wrapper must invoke `beforedone hook codex`")
        if "go install github.com/rrrrrredy/beforedone/cmd/beforedone@latest" not in text:
            fail(f"{wrapper}: missing-CLI output must include an actionable installation command")
        if re.search(r"(?:curl|wget|Invoke-WebRequest).*(?:beforedone|releases)", text, re.IGNORECASE):
            fail(f"{wrapper}: plugin wrappers must not download the CLI")

    unix_versions = re.findall(
        r"(?m)^export BEFOREDONE_PLUGIN_VERSION=([^\s#]+)\s*$",
        unix_wrapper.read_text(encoding="utf-8"),
    )
    windows_versions = re.findall(
        r'(?m)^\$env:BEFOREDONE_PLUGIN_VERSION\s*=\s*"([^"]+)"\s*$',
        windows_wrapper.read_text(encoding="utf-8"),
    )
    if unix_versions != [plugin_version]:
        fail(
            f"{unix_wrapper}: BEFOREDONE_PLUGIN_VERSION must appear once and equal "
            f"plugin.json version {plugin_version}"
        )
    if windows_versions != [plugin_version]:
        fail(
            f"{windows_wrapper}: BEFOREDONE_PLUGIN_VERSION must appear once and equal "
            f"plugin.json version {plugin_version}"
        )


def parse_skill_frontmatter(path: Path) -> tuple[str, str]:
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError) as exc:
        fail(f"{path}: cannot read skill: {exc}")
    if not lines or lines[0] != "---":
        fail(f"{path}: missing opening YAML front matter delimiter")
    try:
        end = lines.index("---", 1)
    except ValueError:
        fail(f"{path}: missing closing YAML front matter delimiter")
    values: dict[str, str] = {}
    for line in lines[1:end]:
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if line.startswith((" ", "\t")) or ":" not in line:
            fail(f"{path}: front matter must contain simple top-level scalar fields")
        key, value = line.split(":", 1)
        if key in values:
            fail(f"{path}: duplicate front matter field {key!r}")
        values[key] = value.strip().strip('"\'')
    if set(values) != {"name", "description"}:
        fail(f"{path}: front matter fields must be exactly name and description")
    name = require_string(values["name"], f"{path}: name")
    description = require_string(values["description"], f"{path}: description")
    if not KEBAB.fullmatch(name) or name != path.parent.name:
        fail(f"{path}: skill name must be kebab-case and match its folder")
    if not any(line.strip() for line in lines[end + 1 :]):
        fail(f"{path}: skill body must not be empty")
    return name, description


def parse_openai_interface(path: Path) -> dict[str, str]:
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError) as exc:
        fail(f"{path}: cannot read agents metadata: {exc}")
    if not lines or lines[0] != "interface:":
        fail(f"{path}: first line must be interface:")
    values: dict[str, str] = {}
    for line in lines[1:]:
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        match = re.fullmatch(r'  ([a-z][a-z0-9_]*):\s*("(?:[^"\\]|\\.)*")', line)
        if not match:
            fail(f"{path}: interface strings must use two-space indentation and double quotes")
        key, encoded = match.groups()
        if key in values:
            fail(f"{path}: duplicate interface field {key!r}")
        try:
            values[key] = json.loads(encoded)
        except json.JSONDecodeError as exc:
            fail(f"{path}: invalid quoted string for {key}: {exc}")
    return values


def validate_skills(root: Path, plugin_root: Path) -> None:
    standalone_root = root / "skills"
    standalone = sorted(standalone_root.glob("*/SKILL.md"))
    if not standalone:
        fail(f"{standalone_root}: at least one standalone skill is required")
    expected_names = {path.parent.name for path in standalone}

    for skills_root in (standalone_root, plugin_root / "skills"):
        found = {path.parent.name for path in skills_root.glob("*/SKILL.md")}
        if found != expected_names:
            fail(f"{skills_root}: skill set does not match standalone skills: {sorted(found)}")
        for skill_path in sorted(skills_root.glob("*/SKILL.md")):
            name, _description = parse_skill_frontmatter(skill_path)
            metadata_path = skill_path.parent / "agents" / "openai.yaml"
            interface = parse_openai_interface(metadata_path)
            required = {"display_name", "short_description", "default_prompt"}
            missing = sorted(required - set(interface))
            if missing:
                fail(f"{metadata_path}: missing required interface fields {missing}")
            require_string(interface["display_name"], f"{metadata_path}: display_name")
            short = require_string(
                interface["short_description"],
                f"{metadata_path}: short_description",
            )
            if not 25 <= len(short) <= 64:
                fail(f"{metadata_path}: short_description must be 25 to 64 characters")
            prompt = require_string(interface["default_prompt"], f"{metadata_path}: default_prompt")
            if f"${name}" not in prompt:
                fail(f"{metadata_path}: default_prompt must explicitly mention ${name}")
            for field in ("icon_small", "icon_large"):
                if field in interface:
                    safe_reference(skill_path.parent, interface[field], f"{metadata_path}: {field}")
            if "brand_color" in interface and not HEX_COLOR.fullmatch(interface["brand_color"]):
                fail(f"{metadata_path}: brand_color must be #RRGGBB")


def validate(root: Path, expected_version: str | None) -> None:
    root = root.resolve()
    plugin_name, plugin_version, plugin_root = validate_plugin(root, expected_version)
    validate_marketplace(root, plugin_name, plugin_root)
    validate_hooks(plugin_root, plugin_version)
    validate_skills(root, plugin_root)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--expected-version")
    args = parser.parse_args()
    try:
        validate(args.root, args.expected_version)
    except ValidationError as exc:
        print(f"distribution validation failed: {exc}", file=sys.stderr)
        return 1
    print("validated Codex plugin, marketplace, hooks, and skills distribution contract")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
