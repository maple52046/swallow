# Project Agent Skills

This document is the entry point that directs AI agents to the skills available
in this project. All paths in this document are relative to the dashboard project
root.

A skill is a task-specific operating guide. Read a skill only when the current
task matches its purpose. Skills never replace [`AGENTS.md`](../AGENTS.md) or the
documents it requires — including the coding-style completion gate; satisfy those
reading requirements first, then consult the relevant skill.

## Scope

This index covers skills owned by the `dashboard` component. This component lives
in the swallow monorepo as `dashboard/`; repository-wide skills are indexed
separately in the repository root `skills/README.md`. Use this index for work
scoped to this component.

## Skill Layout

All skills for this project live under `skills/`. Each skill is a folder
containing a `SKILL.md` (plus optional `scripts/` or `references/` resources),
and must be listed in the Skill Index below.

## Skill Index

This component currently defines no component-scoped skills. Repository-wide
skills (for example `git-commit` and `summarize-manuscript-plans`) live in the
repository root `skills/` and are indexed in the root
[`skills/README.md`](../../skills/README.md).

## Lookup Workflow

1. This component currently defines no component-scoped skills.
2. For repository-wide skills, consult the repository root
   [`skills/README.md`](../../skills/README.md).
3. Otherwise, fall back to [`AGENTS.md`](../AGENTS.md) and the documentation it
   requires.

## Adding A New Skill

For every new skill:

1. Create a folder named after the skill containing a `SKILL.md` (for example
   `skills/inspect-logs/SKILL.md`). Put any helper scripts or resources inside
   that folder.
2. Use a clear, action-oriented title and open with a short "When to Use This
   Skill" section so agents can quickly decide whether the skill applies.
3. Keep all paths inside the skill relative to the project root.
4. Add a row to the Skill Index above with the skill title, its path, and a
   one-sentence "When to Use" description.

Behavior that belongs to the platform as a whole rather than to this component
does not go here; it belongs in the repository root `skills/` directory.

## IDE Integration (Wrapper Convention)

Skills are IDE-neutral. The canonical instructions (and any helper scripts) for
each skill live once in `skills/<name>/SKILL.md`; that file is the single source
of truth. Individual `SKILL.md` files MUST NOT restate this convention — it is
defined here only.

Each IDE adds a thin wrapper that *references* the canonical file instead of
duplicating it:

- **Cursor**: `.cursor/skills/<name>/SKILL.md` carries Cursor frontmatter
  (`name`, `description`) and points back to `skills/<name>/SKILL.md` so
  `/<name>` works as a slash command. Always use this folder form — never a flat
  `.cursor/skills/<name>.md`, and never drop the `skills/` path segment.
  `.cursor/skills/skills.md` is the lookup entry that routes to this README.
- **Other IDEs / agents**: register the skill wherever the tool scans and
  reference `skills/<name>/SKILL.md` rather than copying it.
