# Agent Entry Guide

This file is the entry point for AI agents working in this repository.

## Start Here

Always read this root `AGENTS.md` first for repository work.

After this file, choose the minimum required documents for the task. Do not skip
the root codebase structure document; it explains how platform components map to
top-level directories.

This repository is the swallow platform monorepo. Every component lives in its
own top-level directory (`api-server/`, `dashboard/`); the repository root also
owns the shared model — the ubiquitous language and the contracts between
contexts — under `docs/`.

## Always Read

Before planning, editing, reviewing, or explaining repository work, read:

1. `docs/development/codebase-structure.md` — understand the repository structure, the platform components, and how each component maps to its top-level directory.

## Development Work

When the task involves implementation planning, code changes, review of code
changes, domain behavior, data models, cross-component integration, or core
flows, also read:

1. `docs/development/architecture-spec.md` — understand root codebase design principles and the Strategic DDD constraints.
2. `docs/development/glossaries/README.md` — understand the glossary workflow and locate relevant domain terminology.

If the task creates, modifies, consumes, or validates APIs, also read:

1. `docs/development/api-contracts.md` — understand the API contract workflow and which component owns the contract.

## Component Work

After completing the required root reading for the task, identify the affected
platform component, then read that component's local `AGENTS.md`:

- `api-server/AGENTS.md` — the Go backend / Data Center API Service.
- `dashboard/AGENTS.md` — the React + TypeScript frontend.

Component boundaries are logical: sharing one repository never means sharing a
component boundary. Cross-component interaction goes through the provider-owned
API contract and the shared `docs/`, not through another component's internals.

Component-local `AGENTS.md` files define that component's own required documents
using paths relative to that component. Each component uses **Clean
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

This is a single git repository: a change that spans components (for example an
API contract plus its provider and consumer) is one commit here, not a
submodule-pointer dance.

## Skills

Task-specific operating guides live under `skills/`. When a task may benefit from
one, read `skills/README.md` and follow its lookup workflow before acting.
Skills never replace this file or the documents it requires; satisfy those
reading requirements first, then consult the relevant skill.
