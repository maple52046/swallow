# Skill Lookup Entry

When the current task may benefit from a task-specific skill, read the
repository skills index before acting.

## Required Behavior

Look for a matching skill in this order:

1. **Current directory** — first check the skills defined directly in this
   directory (`.cursor/skills/`). If one matches the current task, follow it.
   These Cursor entries are thin wrappers, so follow the canonical skill file
   they reference.
2. **Repository skills index** — if no local skill matches, open
   `skills/README.md` at the repository root and use the workflow described
   there to locate the skill.

Follow the selected skill before continuing with implementation.

If neither a matching local skill nor `skills/README.md` exists, stop and
report that the repository skills index is missing.

## Important

A Cursor skill in this directory is a thin wrapper that references a canonical,
IDE-neutral skill (for example `.cursor/skills/git-commit/SKILL.md` points to
`skills/git-commit/SKILL.md`). Always follow the referenced canonical file
rather than duplicating its steps. When no local skill applies,
`skills/README.md` is the entry point for the rest of the repository's skills.

Skills never replace the reading requirements in the root `AGENTS.md`. Satisfy
those first, then consult the relevant skill.
