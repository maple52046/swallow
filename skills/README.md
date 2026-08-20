# Repository Agent Skills

This document is the root-level entry point that directs AI agents to the
skills available in this repository. All paths in this document are relative
to the repository root.

A skill is a task-specific operating guide. Read a skill only when the current
task matches its purpose. Skills never replace [`AGENTS.md`](../AGENTS.md) or the
documents it requires; satisfy those reading requirements first, then consult the
relevant skill.

## Where to Find Skills

Skills in this repository are organized by scope.

### Component And Source Project Skills

Skills that operate on one platform component or source project are owned by
that scope. To find them, first identify the affected platform component using
[`docs/development/codebase-structure.md`](../docs/development/codebase-structure.md),
then read that scope's skills index:

- `<component>/skills/README.md` through the root component symlink — prefer this
  path so component boundaries stay visible.
- `src/<project>/skills/README.md` — use this path when the task is explicitly
  source-project scoped, for example submodule or project-wide tooling work.

Because a root component symlink points at a `src/<project>`, multiple component
scopes can share the same physical skills directory. For example,
`api-server/skills/` and `agent/skills/` both resolve to `src/swallow/skills/`.

The following scopes currently expose a skills index:

| Scope | Skills Index |
| --- | --- |
| Swallow source project (covers the `api-server` and `agent` components) | `src/swallow/skills/README.md` |
| Dashboard | `src/dashboard/skills/README.md` |

If the relevant component or source project does not yet expose a skills index,
no scoped skill exists for that area. Fall back to the regular documentation
referenced from that scope's `AGENTS.md`.

### Root Level Skills

Skills that apply across the entire repository, multiple source projects, or
operations outside any single component live alongside this README in `skills/`.

Each root-level skill is a folder containing a `SKILL.md` (plus optional
`scripts/`, `references/`, or `assets/` resources), and must be listed in the
Skill Index below.

## Skill Index

This index lists root-level skills only. Component and source project skills
are indexed in their own scope's skills README.

| Skill | Path | When to Use |
| --- | --- | --- |
| Git Commit | `skills/git-commit/SKILL.md` | When the task needs to analyse the changes of a target repository — the superproject `root` or a `src/<project>` submodule — draft a Conventional Commits message following that target's commit spec, and optionally run `git commit` / `git push`. Triggered by `/git-commit <target>`. |
| Summarize Manuscript Plans | `skills/summarize-manuscript-plans/SKILL.md` | When the task needs to consolidate, summarize, or roll up the AI plan manuscripts under `docs/plans/manuscripts/` into a single long-term plan (following `docs/plans/manuscripts/README.md`) and then delete the original draft manuscripts (never `README.md`). Triggered by `/summarize-manuscript-plans`. |

## Lookup Workflow

When a task arrives, decide which skills index to consult in this order:

1. Determine whether the task is repository-wide, cross-project, or
   infrastructure-level. If yes, scan the root-level Skill Index above.
2. Otherwise, identify the affected platform component using
   [`docs/development/codebase-structure.md`](../docs/development/codebase-structure.md).
3. Open `<component>/skills/README.md` through the component symlink, or
   `src/<project>/skills/README.md` for source-project scoped work.
4. If the scope's skills index lists a skill matching the task, follow that
   skill. If not, fall back to the scope's `AGENTS.md` and the documentation it
   requires.

## Adding A New Skill

When introducing a new skill, place it at the smallest scope that owns the
behavior:

1. Component-specific or source-project-specific behavior belongs under the
   matching `src/<project>/skills/` directory and must be indexed in that
   project's `skills/README.md`.
2. Behavior that genuinely applies across the entire repository, or to
   operations that do not belong to any single source project, belongs in this
   root `skills/` directory and must be indexed in the Skill Index above.

For every new skill:

1. Create a folder named after the skill containing a `SKILL.md` (for example
   `skills/inspect-logs/SKILL.md`). Put any helper scripts or resources inside
   that folder.
2. Use a clear, action-oriented title that matches the skill's intent.
3. Open the file with a short "When to Use This Skill" section so agents can
   quickly decide whether the skill applies.
4. Keep all paths inside the skill relative to that skill's owning scope
   (repository root for root-level skills, project root for project-scoped
   skills).
5. Add a row to the matching Skill Index with the skill title, its path, and a
   one-sentence "When to Use" description.

## IDE Integration (Wrapper Convention)

Skills in this repository are IDE-neutral. The canonical instructions (and any
helper scripts) for each skill live once in `skills/<name>/SKILL.md`; that file
is the single source of truth. Individual `SKILL.md` files MUST NOT restate this
convention — it is defined here only.

Each IDE adds a thin wrapper that *references* the canonical file instead of
duplicating it:

- **Cursor**: `.cursor/skills/<name>/SKILL.md` carries Cursor frontmatter
  (`name`, `description`) and points back to `skills/<name>/SKILL.md` so
  `/<name>` works as a slash command. Always use this folder form — never a flat
  `.cursor/skills/<name>.md`, and never drop the `skills/` path segment.
  `.cursor/skills/skills.md` is the lookup entry that routes to this README.
- **Other IDEs / agents**: register the skill wherever the tool scans and
  reference `skills/<name>/SKILL.md` rather than copying it.

Keep operational steps only in the canonical `SKILL.md`; wrappers just point
here so there is a single source of truth.
