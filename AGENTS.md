# Agent Entry Guide

This file is the entry point for AI agents working in this repository.

## Start Here

Always read this root `AGENTS.md` first for repository work.

After this file, choose the minimum required documents for the task. Do not skip
the root codebase structure document; it explains how components map to
top-level directories.

swallow's core is generic; composed with providers and plugins, it becomes
out-of-the-box datacenter management. The design principles in the root
architecture spec state this idea and the principles that follow from it.

This repository is the swallow monorepo. Every component lives in its
own top-level directory (`api-server/`, `dashboard/`, `cli/`); the repository root also
owns the shared model — the ubiquitous language and the contracts between
contexts — under `docs/`.

## Documentation Audiences

Public documentation is English-first with an equivalent Traditional Chinese
version. Cross-project public guides live in the mirrored `docs/en/` and
`docs/zh-TW/` trees; colocated component and installation references use a
`.zh-TW.md` companion.

Agent-oriented files — this guide, component AGENTS files, development specs,
API contracts, glossaries, ADRs, and skills — stay single-source. They must not
link to public documentation, because doing so expands agent reading paths and
context unnecessarily. Public documentation may link inward to these
authoritative development sources. Read public guides only when the task itself
creates, reviews, or explains public documentation, or when a change is
user-visible and its component's Completion Gate asks for the matching public page
to be updated. Find that page through the public index, `docs/en/README.md`, and
update its `docs/zh-TW/` twin too; a component's own public references (for example
`cli/docs/usage.md`) have a `.zh-TW.md` companion.

## Always Read

Before planning, editing, reviewing, or explaining repository work, read:

1. `docs/development/codebase-structure.md` — understand the repository structure, the components, and how each component maps to its top-level directory.
2. The "swallow 的設計主張" section at the top of `docs/development/architecture-spec.md` — the core idea, the current composition of core, providers, and plugins, the principles, and how to trade them off. Read only that section here; the rest of the spec is for work across components or bounded contexts (Reading by Task).

## Reading by Task

Beyond Always Read, read only what the task needs: find its row, read those documents, then add the
domain specs below for the areas it touches. The upper-level specs state their own readers in their §1,
and a row points to that scope rather than restating it. A task that fits several rows reads them all;
planning or reviewing a task reads what doing it would.

| Task | Also read |
| --- | --- |
| Fix a defect, wording, or behavior without changing who owns a fact, an API contract, or the data model | The domain spec of the area and the component `AGENTS.md`. |
| Explain how something works or why | The domain spec of the area and the glossary terms it uses (lookup: `docs/development/glossaries/README.md`); for the whole system (features, processes and their background work, collections and their writers), `docs/development/system-overview.md`. |
| Add or change a consumer feature (dashboard, cli) over existing API behavior | The API contract; the domain spec when the feature shows domain behavior. |
| Add or change a managed resource, a field, an action that changes the outside world, or who owns a fact | `docs/development/desired-state-model.md` (readers: its §1), the API contract, and the glossary authoring workflow (`docs/development/glossaries/README.md`). |
| Add an external system, provider adapter, Platform, GPU vendor, Managed Software kind, or module, or decide where code belongs | `docs/development/core-and-extensions.md` (readers: its §1) and `docs/development/module-authoring.md`. |
| Introduce or rename a domain term, type, enum value, or API field | The glossary authoring workflow (`docs/development/glossaries/README.md`). |
| Write or run tests, run the full checks, or start the development environment | `docs/development/development-and-testing.md`. |
| Work across components or bounded contexts | The rest of `docs/development/architecture-spec.md` (bounded contexts, context maps) and the maps in `docs/development/context-maps/`. |

## Domain Specs

Add the spec of each area the task touches:

- HTTP API routes, fields, statuses, or errors, whether added, changed, consumed, or explained:
  `api-server/docs/development/api-contracts/README.md`, which defines when its workflow applies.
- Reading, listing, or filtering Workflows through the API, the CLI, or a screen: the workflows
  contract, `api-server/docs/development/api-contracts/api-server/workflows.md`.
- How Workflows execute (start, resource ownership, Task retry, rerun, cancellation, superseding
  blocking work, lost executions, live task events, Job labels): `docs/development/workflow-execution.md`.
- Platforms (their spec and Change Plans, deploy and uninstall, Platform Claims, adding a platform type):
  `docs/development/platform-deployment.md`.
- Managed Software (its kinds, install and uninstall, Software Assignments including reading them,
  composing a kind into a platform deploy): `docs/development/software-deployment.md`.
- Host automation content and module declarations (playbooks, roles, `api-server/modules/manifest.json`,
  `api-server/modules/shared/`, `module.json`, module hooks, a module's dashboard screens, the module
  directory layout); installing packages inside a playbook belongs here:
  `docs/development/module-authoring.md`.
- OS provisioning (OS Images and their verification, Deployment Templates, OS deployment and its
  network intent, Release / Recover / Rescue, Zones and Pools, the Provisioner HTTP Proxy, Subnet DNS):
  `docs/development/os-provisioning.md`.
- Server Enrollment, hardware inspection, Power Configuration, Boot Media, Boot ISOs:
  `docs/development/server-enrollment-and-boot-media.md`.
- SSH keys (the Deployment Key, Access Keys), the automation login user, the Server Default User, Site
  automation credentials, authentication sessions, API keys: `docs/development/access-and-credentials.md`.
- The installation topology, `swallowctl`, release artifacts, the control-plane NTP service and
  post-provision time synchronization: `docs/development/installation.md`.

## Component Work

After completing the required root reading for the task, identify the affected
component, then read that component's local `AGENTS.md`:

- `api-server/AGENTS.md` — the Go backend / Data Center API Service.
- `dashboard/AGENTS.md` — the React + TypeScript frontend.
- `cli/AGENTS.md` — the Go operator CLI (`swallow` binary), an HTTP consumer of the api-server contract.

Component boundaries are logical: sharing one repository never means sharing a
component boundary. Cross-component interaction goes through the provider-owned
API contract and the shared `docs/`, not through another component's internals.
Both `dashboard` and `cli` are consumers of the `api-server` HTTP contract.

Component-local `AGENTS.md` files define that component's own required documents
using paths relative to that component. Each component uses **Clean
Architecture** as its internal design principle; its own architecture spec is
authoritative for layering, data flow, and implementation constraints.

## Architecture Decisions

`docs/decisions/` holds lightweight ADRs for decisions that constrain later work.
Read an ADR when you need to know *why* a boundary or interaction looks the way it
does. Add one only when the decision meets the threshold in
`docs/decisions/README.md` (何時新增 ADR); a single feature's design, including the
alternatives it rejected, belongs in its spec instead. Never fabricate a historical
rationale.

## Plans

Not every change needs a plan. When you do write an implementation plan, in a
tool's plan mode or as a planning step, also save it in this repository, so it is
not kept only in the tool's own directory (for example `~/.cursor/plans/`):

- Location: the root `docs/plans/manuscripts/`, even when the plan concerns one
  component. Never use a component-local plan tree or drop the `plans/` segment.
- Name: `YYYYMMDD-<short-topic>.md`, in Markdown.
- One plan per topic: list the file names in `docs/plans/manuscripts/` to find a
  plan on the same topic, and update that file instead of creating a duplicate.
  Open only that file.
- A plan that replaces or retires a mechanism lists, in its completion conditions,
  the documents that restate the mechanism, and adds the retired names to
  `RETIRED_TERMS` in `scripts/check-docs.py`.

Otherwise do not read `docs/plans/`. Plans are historical records, read only when
the user asks for planning history, decision archaeology, or plan consolidation.

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
