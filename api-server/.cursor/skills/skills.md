# Skill Lookup Entry

When the current task may benefit from a task-specific skill, read the project
skills index before acting.

## Required Behavior

Look for a matching skill in this order:

1. **Current directory** — first check the skills defined directly in this
   directory (`.cursor/skills/`). If one matches the current task, follow it.
   These Cursor entries are thin wrappers, so follow the canonical skill file
   they reference.
2. **Project skills index** — if no local skill matches, open `skills/README.md`
   at the project root and use the workflow described there to locate the skill.

Follow the selected skill before continuing with implementation.

If neither a matching local skill nor `skills/README.md` exists, stop and report
that the project skills index is missing.

## Important

A Cursor skill in this directory is a thin wrapper that references a canonical,
IDE-neutral skill (for example `.cursor/skills/git-commit/SKILL.md` points to
`skills/git-commit/SKILL.md`). Always follow the referenced canonical file rather
than duplicating its steps.

Skills never replace the reading requirements in `AGENTS.md`, including its
coding-style completion gate. Satisfy those first, then consult the relevant
skill.
