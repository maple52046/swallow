#!/usr/bin/env python3
"""Claude Code PreToolUse adapter for `.cursor/hooks/enforce-plan-location.py`.

The plan-location policy lives only in the Cursor guard. This adapter feeds it the
Claude Code hook input, which already carries the fields the guard reads
(`tool_name`, `tool_input.file_path`, `cwd`), and translates its decision:

- Cursor `{"permission": "deny", ...}` becomes a Claude Code
  `hookSpecificOutput.permissionDecision: "deny"`.
- Cursor `allow` becomes no output. A Claude Code `"allow"` would skip the user's
  permission prompt, which the guard never meant to do.

The guard's workspace root is the swallow checkout that contains the target file,
found by walking up to the directory holding the guard. This keeps a write into a
sibling git worktree checked against that worktree, not against the checkout the
session started in. Like the guard, the adapter fails open: any error allows.
"""

import json
import os
import subprocess
import sys
from pathlib import Path

GUARD = Path(".cursor/hooks/enforce-plan-location.py")


def checkout_root(target: Path) -> Path | None:
    for directory in (target, *target.parents):
        if (directory / GUARD).is_file():
            return directory
    return None


def main() -> None:
    raw = sys.stdin.read()
    payload = json.loads(raw)
    target = (payload.get("tool_input") or {}).get("file_path")
    if payload.get("tool_name") != "Write" or not isinstance(target, str) or not target:
        return
    if not os.path.isabs(target):
        target = os.path.join(payload.get("cwd") or os.getcwd(), target)
    root = checkout_root(Path(os.path.normpath(target)).parent)
    if root is None:
        return

    result = subprocess.run(
        [sys.executable, str(root / GUARD)],
        input=raw,
        capture_output=True,
        text=True,
        env={**os.environ, "CURSOR_PROJECT_DIR": str(root)},
        timeout=10,
    )
    decision = json.loads(result.stdout)
    if decision.get("permission") != "deny":
        return
    print(
        json.dumps(
            {
                "hookSpecificOutput": {
                    "hookEventName": "PreToolUse",
                    "permissionDecision": "deny",
                    "permissionDecisionReason": decision.get("agent_message", ""),
                },
                "systemMessage": decision.get("user_message", ""),
            }
        )
    )


if __name__ == "__main__":
    try:
        main()
    except Exception:
        pass
    sys.exit(0)
