# Agent Entry Guide

This file is the entry point for AI agents working in this repository.

## Start Here

Always read this root `AGENTS.md` first for repository work.

After this file, choose the minimum required documents for the task. Do not skip
the root codebase structure document; it explains how components map to
top-level directories.

swallow's core is generic; composed with providers and plugins, it becomes
out-of-the-box datacenter management. The design principles in the root
architecture spec state this idea and the principles that follow from it; read
them for design, review, and placement work (Reading by Task).

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
user-visible: a screen, a command, or API behavior an operator or integrator
relies on (making code do what a contract already documents is not). Find the page that covers it
through the public index, `docs/en/README.md`, or by searching `docs/en/` for the feature's name,
and update its `docs/zh-TW/` twin too; when no page covers the behavior, add none. A component's
own public references (for example `cli/docs/usage.md`) have a `.zh-TW.md` companion.

## Always Read

Before planning, editing, reviewing, or explaining repository work, read:

1. `docs/development/codebase-structure.md` — its Components and Code Lookup Workflow sections: the components and how each maps to its top-level directory. Add its core, provider adapter, and module location section when deciding where new code goes in `api-server`, and read the rest only when the task changes that structure.

## Reading by Task

Beyond Always Read, read only what the task needs: find its row, read those documents, then add the
domain specs below for the areas it touches. The upper-level specs state their own readers in their §1,
and a row points to that scope rather than restating it: read the §1 to decide whether the spec applies, and
when it does, read the sections the task needs. A task that fits several rows reads them all; planning or
reviewing a task reads what doing it would, except a component's coding style, which waits until code is
written or a code diff reviewed; reviewing a change described without a diff is a design review.

Domain specs and contracts are long. Read a spec's reader section (its §1, or the unnumbered reader
section before it) and its headings, then only the sections the task needs; in a contract, read the
sections for the routes, parameters, and fields involved, even when the contract describes one route. A link or guide that names a section (§4.6) needs only that section,
not the spec's reader section.

| Task | Also read |
| --- | --- |
| Design new behavior or choose between approaches; review a change; add a managed resource, module, or provider adapter, or change what one owns or declares; decide where code belongs | The "swallow 的設計主張" section at the top of `docs/development/architecture-spec.md`: the core idea, the current composition, the principles, and how to trade them off. Only that section; the rest of the spec is the last row's. |
| Fix a defect, wording, or behavior without changing who owns a fact, an API contract, or the data model | The domain spec of the area (an area without one uses its API contract) and the `AGENTS.md` of the component that owns the behavior. For a defect seen on a screen or in a command, find that component first: compare what the API returns (or the code behind it) with the contract; if the API breaks the contract the defect is in `api-server`, otherwise in the consumer. |
| Explain how something works or why | The domain spec of each area involved; a glossary term only when the spec does not define it well enough (open a linked term directly; otherwise `docs/development/glossaries/README.md`); for the whole system (features, processes and their background work, collections and their writers), `docs/development/system-overview.md`. Reading code to confirm a detail needs no component guide: find it with the code map in `system-overview.md` or the spec's code table. |
| Add or change a consumer feature (dashboard, cli) over existing API behavior | The API contract; the domain spec when the feature shows domain behavior. |
| Add or change API behavior (a route, query parameter, field, status, or error) | The contract workflow (`api-server/docs/development/api-contracts/README.md`) and the contract. Regenerating the dashboard's types (`dashboard/src/infrastructure/api/generated/openapi.ts`, then `npm run contract:check`) is part of this and needs no dashboard guide; add the consumer row only for a consumer whose own code the change also changes. |
| Add or change a managed resource, a field of its spec or status, a kind of action that changes the outside world, or who owns a fact (a new Managed Software kind reuses install and uninstall and is not this row) | `docs/development/desired-state-model.md` (readers: its §1); the API contract when the change reaches the API; the glossary authoring workflow when it introduces or changes a term. |
| Add an external system, provider adapter, Platform, Managed Software kind, or module, or GPU-vendor-specific behavior (a new vendor or a new feature of an existing one) | `docs/development/core-and-extensions.md` (readers: its §1) and `docs/development/module-authoring.md`. |
| Decide where new code belongs (core, provider adapter, or module) | `docs/development/core-and-extensions.md` §1, and §3.6 while its transitional rule applies. |
| Introduce or rename a domain term, a type, or an enum value the glossary owns | The glossary authoring workflow (`docs/development/glossaries/README.md`). A query parameter or response field that uses existing terms is an API change, not a glossary change; Managed Software kinds and the Workflow kinds a module declares are module-owned, not glossary values. |
| Write or run tests, run the full checks, or start the development environment | `docs/development/development-and-testing.md`; content tests of playbooks and roles are in `docs/development/module-authoring.md` §6 instead. |
| Change how components or bounded contexts depend on each other (a new cross-context dependency, a context map, a component boundary) | The rest of `docs/development/architecture-spec.md` (bounded contexts, context maps) and the map in `docs/development/context-maps/` for the contexts involved. An ordinary contract change with its provider and consumer is not this row. |

## Domain Specs

Add the spec of each area the task touches:

- Adding, changing, deprecating, or removing an HTTP route, query parameter, field, status, or error:
  `api-server/docs/development/api-contracts/README.md`, which defines the workflow. Consuming or
  explaining existing API behavior needs only the contract itself; find it in
  `api-server/docs/development/api-contracts/api-server/outline.md` unless a spec already links it.
- Reading, listing, or filtering Workflows through the API, the CLI, or a screen: the workflows
  contract, `api-server/docs/development/api-contracts/api-server/workflows.md`.
- How Workflows execute (start, resource ownership, Task retry, rerun, cancellation, superseding
  blocking work, lost executions, live task events, Job labels): `docs/development/workflow-execution.md`.
- Platforms (their spec and Change Plans, deploy and uninstall, Platform Claims, adding a platform type):
  `docs/development/platform-deployment.md`. A change confined to a Platform module's playbooks or roles
  reads module-authoring (below) and, in this spec, §2.3 (OS-specific behavior) and §5.
- Managed Software (its kinds, install and uninstall, how Software Assignments change state, composing a
  kind into a platform deploy): `docs/development/software-deployment.md`. Listing or showing Software
  Assignments through the API, the CLI, or a screen needs only the software contract,
  `api-server/docs/development/api-contracts/api-server/software.md`.
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

Areas without a domain spec use their contract as the spec: the Server inventory (listing,
searching, the Server projection) uses `servers-list.md`; alerts use `monitoring-alerts.md` and
metrics use `server-metrics.md`, all under
`api-server/docs/development/api-contracts/api-server/`.

## Component Work

After completing the required root reading for the task, identify the component
whose code the task changes or whose code diff it reviews, then read that
component's local `AGENTS.md`. Reading code only to confirm a detail, or editing a document under the
component's directory (an API contract, for example), does not count:

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
- One plan per topic: list the file names in `docs/plans/manuscripts/` (listing
  names is not reading plans) to find a plan on the same topic, and update that
  file instead of creating a duplicate. Open only that file.
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

Task-specific operating guides live under `skills/`. When the task acts on real
machines or drives the running development stack (automated tests against its
MongoDB do not count), writes a commit, or consolidates plans, read
`skills/README.md` and follow its lookup workflow before acting.
Skills never replace this file or the documents it requires; satisfy those
reading requirements first, then consult the relevant skill.
