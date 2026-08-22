#!/usr/bin/env python3
"""preToolUse guard that enforces the project plan-manuscript location.

This hook is the enforcement backstop for `.cursor/rules/project-plan-location.mdc`.
A Cursor rule is only advisory context; it cannot mechanically stop a Write to the
wrong path. This guard runs before every `Write` tool call and *denies* any attempt
to create a plan manuscript outside the single canonical location for this
monorepo:

    docs/plans/manuscripts/<name>.md   (relative to the workspace root)

Design constraints:
- Deny only when a misplaced plan write is positively identified. On any parsing
  uncertainty (missing path, unreadable stdin, path outside the workspace, etc.)
  the guard ALLOWS, so it can never block unrelated code edits. Enforcement of the
  actual policy still holds because a wrong plan path is an unambiguous match.
- "Plan manuscript" is detected by either signal:
    1. the path contains a `manuscripts/` directory segment, or
    2. the basename matches the `YYYYMMDD-<topic>.md` naming convention.
  This catches both the missing-`plans/` bug (`docs/manuscripts/...`) and misplaced
  writes into a component tree (`<component>/docs/plans/manuscripts/...`).
"""

import json
import os
import re
import sys

# The one canonical, flat location for plan manuscripts, relative to workspace root.
ALLOWED_REL_RE = re.compile(r"^docs/plans/manuscripts/[^/]+\.md$")
# Plan manuscript naming convention: YYYYMMDD-<short-topic>.md
PLAN_NAME_RE = re.compile(r"^\d{8}-.+\.md$")

# Candidate keys that may carry the write target across tool-input shapes.
PATH_KEYS = ("path", "file_path", "target_file", "filePath", "absolute_path", "uri")


def allow():
    """Emit an allow decision and exit without blocking the tool call."""
    print(json.dumps({"permission": "allow"}))
    sys.exit(0)


def deny(rel_path: str, correct_path: str):
    """Block the write and tell both the user and the agent the correct path."""
    agent_message = (
        f"Blocked by the plan-location guard: '{rel_path}' is a plan manuscript "
        f"written outside the canonical location. In this swallow monorepo, plan "
        f"manuscripts must live at '{correct_path}' (flat, at the repository root), "
        f"regardless of which component the plan is about. Re-issue the write to "
        f"'{correct_path}'. Do not create a 'manuscripts' directory anywhere else, "
        f"and do not write plan manuscripts into a component's own docs tree."
    )
    user_message = (
        f"Plan manuscript blocked: attempted to write '{rel_path}'. "
        f"Enforced target: '{correct_path}'."
    )
    print(
        json.dumps(
            {
                "permission": "deny",
                "user_message": user_message,
                "agent_message": agent_message,
            }
        )
    )
    sys.exit(0)


def main():
    try:
        raw = sys.stdin.read()
        payload = json.loads(raw) if raw.strip() else {}
    except Exception:
        allow()
        return

    # Only Write creates brand-new files; edits target existing (already-placed) files.
    if payload.get("tool_name") != "Write":
        allow()
        return

    tool_input = payload.get("tool_input") or {}
    if not isinstance(tool_input, dict):
        allow()
        return

    target = None
    for key in PATH_KEYS:
        value = tool_input.get(key)
        if isinstance(value, str) and value.strip():
            target = value
            break
    if not target:
        allow()
        return

    # Resolve the workspace root; CURSOR_PROJECT_DIR is always present for hooks.
    root = os.environ.get("CURSOR_PROJECT_DIR")
    if not root:
        roots = payload.get("workspace_roots")
        if isinstance(roots, list) and roots and isinstance(roots[0], str):
            root = roots[0]
    if not root:
        root = payload.get("cwd") or os.getcwd()

    # Resolve the target against cwd/root when relative, then make it root-relative.
    if not os.path.isabs(target):
        base = payload.get("cwd") or root
        target = os.path.join(base, target)
    abs_target = os.path.normpath(target)

    try:
        rel = os.path.relpath(abs_target, os.path.normpath(root))
    except ValueError:
        allow()
        return
    rel = rel.replace(os.sep, "/")

    # Outside the workspace entirely: not our concern.
    if rel.startswith("../") or rel == "..":
        allow()
        return

    parts = rel.split("/")
    basename = parts[-1] if parts else rel

    is_plan_manuscript = ("manuscripts" in parts) or bool(PLAN_NAME_RE.match(basename))
    if not is_plan_manuscript:
        allow()
        return

    if ALLOWED_REL_RE.match(rel):
        allow()
        return

    deny(rel, f"docs/plans/manuscripts/{basename}")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Never let a guard bug block unrelated work; fail open on the allow side.
        print(json.dumps({"permission": "allow"}))
        sys.exit(0)
