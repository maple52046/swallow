# Agent Entry Guide

This file is the entry point for AI agents working in this repository.

## Start Here

Always read this root `AGENTS.md` first for repository work.

After this file, choose the minimum required documents for the task. Do not skip
the root codebase structure document; it explains how platform components map to
source projects.

This repository is the platform superproject. It contains no business source
code: all implementation lives in `src/<project>` submodules. This repository
owns the shared model — the ubiquitous language and the contracts between
contexts.

## Always Read

Before planning, editing, reviewing, or explaining repository work, read:

1. `docs/development/codebase-structure.md` — understand the root codebase structure, platform components, source projects, and the relationship between the root component symlinks and `src/`.

## Development Work

When the task involves implementation planning, code changes, review of code
changes, domain behavior, data models, cross-component integration, or core
flows, also read:

1. `docs/development/architecture-spec.md` — understand root codebase design principles and the Strategic DDD constraints.
2. `docs/development/glossaries/README.md` — understand the glossary workflow and locate relevant domain terminology.

If the task creates, modifies, consumes, or validates APIs, also read:

1. `docs/development/api-contracts.md` — understand the API contract workflow and which component owns the contract.

## Component Or Sub Project Work

After completing the required root reading for the task, identify the affected
platform component or source project, then read that local `AGENTS.md`.

- `<component>/AGENTS.md` through the root component symlink (`api-server`, `agent`, `dashboard`)
- `src/<project>/AGENTS.md`

Prefer entering through the component symlink to preserve platform component
boundaries. Because `api-server` and `agent` both resolve to `src/swallow`, a
shared source project never means a shared component boundary.

Project-local `AGENTS.md` files define that project or component's own required
documents using paths relative to that project. Each sub project uses **Clean
Architecture** as its internal design principle; its own architecture spec is
authoritative for layering, data flow, and implementation constraints.

## Architecture Decisions

`docs/decisions/` holds lightweight ADRs for decisions that cross component
boundaries. Read an ADR when you need to know *why* a boundary or interaction
looks the way it does. Add one — following `docs/decisions/README.md` — when a
decision affects multiple components or has a non-obvious rejected alternative.
Never fabricate a historical rationale.

## Plan Backup

Do not proactively read `docs/plans/` during normal development work. Plans are
historical records and should only be read when the user asks for historical
planning context, decision archaeology, or plan consolidation.

When creating or updating an implementation plan for the current project, store
the project-level manuscript under `docs/plans/manuscripts/` instead of relying
only on user-level plan storage. Use the naming convention
`YYYYMMDD-<short-topic>.md`, and update an existing plan for the same topic
instead of creating a duplicate.

## Commit Messages

When asked to draft or validate a commit message, read and follow:

- `docs/development/commit-spec.md`

Base the message on the relevant staged diff or user-provided change summary.
Use the Conventional Commits format required by the commit spec, including an
appropriate type, optional scope, concise description, optional body, and
`BREAKING CHANGE` footer when the staged changes require one. Commit messages
MUST be written in English, regardless of the chat language.

This repository and each `src/<project>` submodule are separate git
repositories. Commit source changes inside the submodule first, then commit the
updated submodule pointer here.

## Skills

Task-specific operating guides live under `skills/`. When a task may benefit from
one, read `skills/README.md` and follow its lookup workflow before acting.
Skills never replace this file or the documents it requires; satisfy those
reading requirements first, then consult the relevant skill.
