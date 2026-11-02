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
INLINE_CODE_RE = re.compile(r"`([^`\n]+)`")
CODE_PATH_PREFIXES = (
    "api-server/",
    "dashboard/",
    "cli/",
    "deploy/",
    "docs/",
    "scripts/",
    "skills/",
    "internal/",
    "modules/",
    "cmd/",
    "config/",
    "tests/",
    "src/",
)
COMPONENT_RELATIVE_PREFIXES = ("internal/", "modules/", "cmd/", "config/", "tests/")
COMPONENTS = ("api-server", "dashboard", "cli")
# Inline paths in current specs that name something on purpose absent: a rejected alternative,
# a layout that was retired, an example of a file to create, or a glossary change note recording
# where something used to be.
HISTORICAL_CODE_PATHS = {
    ("docs/development/codebase-structure.md", "src/swallow"): "retired submodule layout",
    ("docs/development/codebase-structure.md", "src/dashboard"): "retired submodule layout",
    ("docs/development/core-and-extensions.md", "api-server/automation"): "rejected alternative",
    ("docs/development/core-and-extensions.md", "api-server/internal/modules"): "rejected alternative",
    (
        "api-server/docs/development/api-contracts/api-server/planned-surface.md",
        "docs/api-contracts/README.md",
    ): "migrated-from note",
    ("skills/README.md", "skills/inspect-logs/SKILL.md"): "example of a skill to add",
    ("docs/development/glossaries/terms/integration.md", "docs/glossaries/site.md"): "change note",
    ("docs/development/glossaries/terms/machine.md", "docs/glossaries/provisioning.md"): "change note",
    (
        "docs/development/glossaries/terms/os-provisioning-provider.md",
        "docs/glossaries/provisioning.md",
    ): "change note",
    ("docs/development/glossaries/terms/release.md", "docs/glossaries/provisioning.md"): "change note",
    ("docs/development/glossaries/terms/server.md", "docs/glossaries"): "change note",
    ("docs/development/glossaries/terms/site.md", "docs/glossaries/site.md"): "change note",
    ("docs/development/glossaries/terms/staleness.md", "docs/glossaries/site.md"): "change note",
}
# Terms for mechanisms that no longer exist. A document that still uses one describes a model
# the code left behind. An ADR's revision history records the past and is not checked.
RETIRED_TERMS = {
    "indeterminate": "unprovable outcomes are requires_attention with executor_outcome_unknown",
    "step observer": "Tasks declare completion behaviors (workflow-execution.md §12)",
    "completion observer": "Tasks declare completion behaviors (workflow-execution.md §12)",
    "isTimeTask": "Tasks declare completion behaviors (workflow-execution.md §12)",
    "/api/v1/operations": "the alias was removed; the route is /api/v1/workflows",
    "/api/v1/clusters": "the alias was removed; the route is /api/v1/platforms",
    "IronicProvider": "there is no Ironic adapter",
    "auto-exporter": "the Node Exporter is composed into OS deployment Workflows",
    "software assignment sweep": (
        "a Workflow that removes a Server's OS, or reconcile, marks its Software Assignments absent"
    ),
    "OS Image overlay": "the swallow-owned overrides are OS Image metadata (decision 071)",
}
# Documents whose purpose is to record the retirement itself: a contract change note or the
# public compatibility page.
RETIRED_TERM_RECORDS = {
    ("api-server/docs/development/api-contracts/api-server/overview.md", "indeterminate"),
    ("api-server/docs/development/api-contracts/api-server/workflows.md", "/api/v1/operations"),
    ("api-server/docs/development/api-contracts/api-server/platforms.md", "/api/v1/clusters"),
    ("docs/en/reference/compatibility.md", "/api/v1/operations"),
    ("docs/en/reference/compatibility.md", "/api/v1/clusters"),
    ("docs/zh-TW/reference/compatibility.md", "/api/v1/operations"),
    ("docs/zh-TW/reference/compatibility.md", "/api/v1/clusters"),
    ("api-server/docs/development/api-contracts/api-server/provisioning.md", "OS Image overlay"),
    ("docs/development/glossaries/outline.md", "OS Image overlay"),
    ("docs/development/glossaries/terms/provider-data-overlay.md", "OS Image overlay"),
}

# Architecture-review tracking codes are defined only in uncommitted plan drafts, so a committed
# document that cites one points nowhere.
TRACKING_CODE_RE = re.compile(r"\b(?:AR|DF)-\d{2}\b")
# An ADR ends with its revision history (decisions/README.md); what it says about the past may
# name paths and mechanisms that no longer exist.
REVISION_HISTORY_RE = re.compile(r"^## (?:Revision history|修訂紀錄)[ \t]*$", re.MULTILINE)


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


def is_current_spec(path: Path) -> bool:
    """Return whether path is a current specification whose inline paths must exist.

    Plans are history, so they may name paths that moved since.
    """
    return is_development_source(path) and "plans" not in path.parts


def current_text(path: Path, text: str) -> str:
    """Return the part of a document that describes the present: an ADR without its revision history."""
    if path.parts[:2] != ("docs", "decisions") or path.name == "README.md":
        return text
    match = REVISION_HISTORY_RE.search(text)
    return text[: match.start()] if match else text


def code_path_candidates(source: Path, token: str) -> list[Path]:
    """Return the directories an inline repository path in source may be relative to."""
    bases = [ROOT]
    component = source.parts[0] if source.parts and source.parts[0] in COMPONENTS else None
    if component:
        bases.append(ROOT / component)
    if token.startswith(COMPONENT_RELATIVE_PREFIXES):
        bases.append(ROOT / "api-server")
    if token.startswith("src/"):
        bases.append(ROOT / "dashboard")
    bases.append(ROOT / source.parent)
    return bases


def code_path_exists(base: Path, token: str) -> bool:
    pattern = re.sub(r"<[^>]*>", "*", token).replace("...", "*")
    alternatives = [pattern]
    brace = re.search(r"\{([^{}]*)\}", pattern)
    if brace:
        alternatives = [
            pattern[: brace.start()] + choice + pattern[brace.end() :]
            for choice in brace.group(1).split(",")
        ]
    for alternative in alternatives:
        if "*" in alternative or "?" in alternative:
            if any(base.glob(alternative)):
                return True
        elif (base / alternative).exists():
            return True
    return False


def normalize_code_path(token: str) -> str:
    token = token.split("#", 1)[0]
    token = re.sub(r":\d+(-\d+)?$", "", token)
    return token.rstrip(".,;:)").rstrip("/")


def check_code_paths(files: list[Path], errors: list[str]) -> None:
    """Report inline repository paths in current specs that do not exist.

    A path that a spec mentions but that was renamed or removed misleads every reader that
    follows it, and check_links only sees Markdown links. Only tokens that start with a known
    repository prefix are checked; bare file names are not.
    """
    for source in files:
        if not is_current_spec(source):
            continue
        text = without_fenced_code(current_text(source, (ROOT / source).read_text(encoding="utf-8")))
        for raw in INLINE_CODE_RE.findall(text):
            if any(character.isspace() for character in raw) or "/" not in raw:
                continue
            if not raw.startswith(CODE_PATH_PREFIXES):
                continue
            token = normalize_code_path(raw)
            if (str(source), token) in HISTORICAL_CODE_PATHS:
                continue
            candidates = [token]
            symbol = re.sub(r"\.[A-Z]\w*$", "", token)
            if symbol != token:
                candidates.append(symbol)
            if not Path(token).suffix:
                candidates.extend([token + ".ts", token + ".tsx"])
            if any(
                code_path_exists(base, candidate)
                for base in code_path_candidates(source, raw)
                for candidate in candidates
            ):
                continue
            errors.append(f"{source}: inline path does not exist: {raw}")


def check_retired_terms(files: list[Path], errors: list[str]) -> None:
    """Report retired mechanism names outside the documents that record the retirement."""
    patterns = {
        term: re.compile(r"(?<![\w-])" + re.escape(term) + r"(?![\w-])", re.IGNORECASE)
        for term in RETIRED_TERMS
    }
    for source in files:
        text = current_text(source, (ROOT / source).read_text(encoding="utf-8"))
        for term, pattern in patterns.items():
            if (str(source), term) in RETIRED_TERM_RECORDS:
                continue
            for number, line in enumerate(text.splitlines(), start=1):
                if pattern.search(line):
                    errors.append(
                        f"{source}:{number}: retired term {term!r} ({RETIRED_TERMS[term]})"
                    )


def check_tracking_codes(files: list[Path], errors: list[str]) -> None:
    """Report architecture-review tracking codes in committed documents."""
    for source in files:
        text = (ROOT / source).read_text(encoding="utf-8")
        for number, line in enumerate(text.splitlines(), start=1):
            match = TRACKING_CODE_RE.search(line)
            if match:
                errors.append(
                    f"{source}:{number}: tracking code {match.group(0)} (describe the change instead)"
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
    check_code_paths(files, errors)
    check_retired_terms(files, errors)
    check_tracking_codes(files, errors)
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
