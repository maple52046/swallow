---
name: git-commit
description: >-
  Analyse the changes of a target repository (the platform superproject root or a
  src/<project> submodule), write a high-quality commit message, and run git
  commit (optionally git add and git push). Use when the user runs /git-commit or
  asks to commit staged changes with a generated message.
---

# git-commit

This is a thin Cursor wrapper. The canonical instructions live in
[`skills/git-commit/SKILL.md`](../../../skills/git-commit/SKILL.md)
(relative to the repository root). Read that file and follow it.

Invocation:

```
/git-commit <target> [--auto-add] [--all] [--push] [--date <when>]
```

`<target>` is required and selects the repository to commit in: `root` for the
platform superproject, or a source project name under `src/` (e.g. `swallow`,
`dashboard`), in which case every git command runs inside `src/<project>`.
