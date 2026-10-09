#!/usr/bin/env python3
"""Validate swallow's Markdown documentation structure and local links."""

from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parents[1]
SKIP_PARTS = {".git", "node_modules", "vendor"}
LINK_RE = re.compile(r"!?\[[^\]]*\]\(([^)]+)\)")
REFERENCE_RE = re.compile(r"^\s*\[[^\]]+\]:\s*(\S+)", re.MULTILINE)
FENCE_RE = re.compile(r"^\s*(```|~~~)")
EXTERNAL_SCHEMES = {"http", "https", "mailto", "tel", "data"}


def markdown_files() -> list[Path]:
    """Return tracked and not-ignored untracked Markdown files."""
    result = subprocess.run(
        [
            "git",
            "ls-files",
            "--cached",
            "--others",
            "--exclude-standard",
            "--",
            "*.md",
        ],
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
    )
    files: list[Path] = []
    for name in result.stdout.splitlines():
        path = Path(name)
        if any(part in SKIP_PARTS for part in path.parts):
            continue
        if path.parts[:2] == ("docs", "plans"):
            continue
        files.append(path)
    return sorted(set(files))


def without_fenced_code(text: str) -> str:
    lines: list[str] = []
    fence: str | None = None
    for line in text.splitlines():
        match = FENCE_RE.match(line)
        if match:
            marker = match.group(1)[0]
            if fence is None:
                fence = marker
            elif marker == fence:
                fence = None
            continue
        if fence is None:
            lines.append(line)
    return "\n".join(lines)


def link_target(raw: str) -> str:
    raw = raw.strip()
    if raw.startswith("<") and ">" in raw:
        return raw[1 : raw.index(">")]
    # Markdown permits an optional title after the destination. Repository
    # paths do not contain unescaped whitespace, so the first token is enough.
    return raw.split(maxsplit=1)[0] if raw else ""


def local_targets(path: Path) -> list[str]:
    text = without_fenced_code((ROOT / path).read_text(encoding="utf-8"))
    targets = [link_target(match) for match in LINK_RE.findall(text)]
    targets.extend(link_target(match) for match in REFERENCE_RE.findall(text))
    return [target for target in targets if target]


def resolve_local(source: Path, target: str) -> Path | None:
    if target.startswith("#"):
        return None
    parsed = urlsplit(target)
    if parsed.scheme.lower() in EXTERNAL_SCHEMES or parsed.netloc:
        return None
    if not parsed.path or parsed.path.startswith("/"):
        return None
    decoded = unquote(parsed.path)
    return (ROOT / source.parent / decoded).resolve()


def public_paths(files: list[Path]) -> set[Path]:
    public = {
        Path("README.md"),
        Path("README.zh-TW.md"),
        Path("CONTRIBUTING.md"),
        Path("CONTRIBUTING.zh-TW.md"),
        Path("docs/README.md"),
        Path("cli/docs/usage.md"),
        Path("cli/docs/usage.zh-TW.md"),
    }
    for path in files:
        if path.parts[:2] in (("docs", "en"), ("docs", "zh-TW")):
            public.add(path)
        if path in {
            Path("api-server/README.md"),
            Path("api-server/README.zh-TW.md"),
            Path("dashboard/README.md"),
            Path("dashboard/README.zh-TW.md"),
            Path("cli/README.md"),
            Path("cli/README.zh-TW.md"),
        } or (
            path.name in {"README.md", "README.zh-TW.md"}
            and path.parts[0] == "deploy"
        ):
            public.add(path)
    return {(ROOT / path).resolve() for path in public}


def is_development_source(path: Path) -> bool:
    if path.name == "AGENTS.md" or "skills" in path.parts:
        return True
    return any(
        path.parts[index : index + 2] in (("docs", "development"), ("docs", "decisions"))
        for index in range(len(path.parts) - 1)
    )


def check_links(files: list[Path], errors: list[str]) -> None:
    public = public_paths(files)
    root_resolved = ROOT.resolve()
    for source in files:
        for target in local_targets(source):
            resolved = resolve_local(source, target)
            if resolved is None:
                continue
            try:
                resolved.relative_to(root_resolved)
            except ValueError:
                errors.append(f"{source}: local link escapes the repository: {target}")
                continue
            if not resolved.exists():
                errors.append(f"{source}: broken local link: {target}")
            if is_development_source(source) and resolved in public:
                errors.append(
                    f"{source}: development documentation must not link to public docs: {target}"
                )


def check_language_mirror(errors: list[str]) -> None:
    english = {
        path.relative_to(ROOT / "docs/en")
        for path in (ROOT / "docs/en").rglob("*.md")
    }
    traditional_chinese = {
        path.relative_to(ROOT / "docs/zh-TW")
        for path in (ROOT / "docs/zh-TW").rglob("*.md")
    }
    for missing in sorted(english - traditional_chinese):
        errors.append(f"docs/zh-TW is missing counterpart: {missing}")
    for missing in sorted(traditional_chinese - english):
        errors.append(f"docs/en is missing counterpart: {missing}")


def check_required_counterparts(errors: list[str]) -> None:
    pairs = [
        (Path("README.md"), Path("README.zh-TW.md")),
        (Path("CONTRIBUTING.md"), Path("CONTRIBUTING.zh-TW.md")),
        (Path("api-server/README.md"), Path("api-server/README.zh-TW.md")),
        (Path("dashboard/README.md"), Path("dashboard/README.zh-TW.md")),
        (Path("cli/README.md"), Path("cli/README.zh-TW.md")),
        (Path("cli/docs/usage.md"), Path("cli/docs/usage.zh-TW.md")),
    ]
    pairs.extend(
        (path.relative_to(ROOT), path.with_name("README.zh-TW.md").relative_to(ROOT))
        for path in (ROOT / "deploy").rglob("README.md")
    )
    for english, traditional_chinese in pairs:
        if not (ROOT / english).is_file():
            errors.append(f"missing required English document: {english}")
        if not (ROOT / traditional_chinese).is_file():
            errors.append(
                f"{english}: missing Traditional Chinese counterpart: {traditional_chinese}"
            )


def check_documentation_screenshots(errors: list[str]) -> None:
    # Docs screenshots are curated assets. They are no longer tied to tracked
    # Playwright pixel baselines (see dashboard visual-review workflow).
    assets = {
        Path("docs/assets/overview.png"): {
            Path("README.md"),
            Path("README.zh-TW.md"),
        },
        Path("docs/assets/platform-deployment-wizard.png"): {
            Path("docs/en/introduction.md"),
            Path("docs/zh-TW/introduction.md"),
            Path("docs/en/guides/platforms.md"),
            Path("docs/zh-TW/guides/platforms.md"),
        },
    }
    for asset, documents in assets.items():
        asset_path = ROOT / asset
        if not asset_path.is_file():
            errors.append(f"missing documentation screenshot asset: {asset}")
            continue
        expected = asset_path.resolve()
        for document in documents:
            linked = {
                resolved
                for target in local_targets(document)
                if (resolved := resolve_local(document, target)) is not None
            }
            if expected not in linked:
                errors.append(f"{document}: must reference documentation asset {asset}")


def main() -> int:
    errors: list[str] = []
    files = markdown_files()
    check_links(files, errors)
    check_language_mirror(errors)
    check_required_counterparts(errors)
    check_documentation_screenshots(errors)
    if errors:
        print("Documentation validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print(f"Documentation validation passed ({len(files)} Markdown files checked).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
