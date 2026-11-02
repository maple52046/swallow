# Repository Agent Skills

This document is the root-level entry point that directs AI agents to the
skills available in this repository. All paths in this document are relative
to the repository root.

A skill is a task-specific operating guide. Read a skill only when the current
task matches its purpose. Skills never replace [`AGENTS.md`](../AGENTS.md) or the
documents it requires; satisfy those reading requirements first, then consult the
relevant skill.

## Where to Find Skills

Every skill lives in this root `skills/` directory, whether it concerns one
component or the whole repository; no component keeps its own skills index.

Each skill is a folder containing a `SKILL.md` (plus optional `scripts/`,
`references/`, or `assets/` resources), and must be listed in the Skill Index
below.

## Skill Index

| Skill | Path | When to Use |
| --- | --- | --- |
| Git Commit | `skills/git-commit/SKILL.md` | When the task needs to analyse the repository's changes, draft a Conventional Commits message following `docs/development/commit-spec.md`, and optionally run `git commit` / `git push`. Triggered by `/git-commit`. |
| Summarize Manuscript Plans | `skills/summarize-manuscript-plans/SKILL.md` | When the task needs to consolidate, summarize, or roll up the AI plan manuscripts under `docs/plans/manuscripts/` into a single long-term plan (following `docs/plans/manuscripts/README.md`) and then delete the original draft manuscripts (never `README.md`). Triggered by `/summarize-manuscript-plans`. |

## Lookup Workflow

When a task arrives, scan the Skill Index above. If a skill matches the task,
follow it; otherwise fall back to `AGENTS.md` and the documentation it requires.

## Adding A New Skill

1. Create a folder named after the skill containing a `SKILL.md` (for example
   `skills/inspect-logs/SKILL.md`). Put any helper scripts or resources inside
   that folder.
2. Use a clear, action-oriented title that matches the skill's intent.
3. Open the file with a short "When to Use This Skill" section so agents can
   quickly decide whether the skill applies.
4. Keep all paths inside the skill relative to the repository root.
5. Add a row to the Skill Index with the skill title, its path, and a
   one-sentence "When to Use" description.
6. Add the IDE wrappers described below.

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
- **Codex**: `.agents/skills/<name>/SKILL.md` carries Codex frontmatter and
  points back to `skills/<name>/SKILL.md`; Codex invokes it as `$<name>`.
  Codex-only settings (for example `agents/openai.yaml`) live beside the wrapper.
- **Other IDEs / agents**: register the skill wherever the tool scans and
  reference `skills/<name>/SKILL.md` rather than copying it.

One exception: `.agents/skills/migrate-cursor-to-codex/SKILL.md` is a Codex tool
that copies this repository's Cursor setup for Codex. It has no canonical copy
here and no Cursor wrapper, and it is not listed in the Skill Index.

Keep operational steps only in the canonical `SKILL.md`; wrappers just point
here so there is a single source of truth.
