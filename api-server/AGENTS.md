# Swallow Agent Entry Guide

This file is the entry point for AI agents working in the swallow backend, the
`api-server` component. All paths in this file are relative to this component's
root (`api-server/`).

## Required Reading

Before planning, reviewing, or explaining swallow work, read:

1. `docs/development/architecture-spec.md` — understand the swallow Clean Architecture boundaries.

Before adding or modifying any code, read:

1. `docs/development/architecture-spec.md` — understand that swallow uses Clean Architecture as its design principle.
2. `docs/development/coding-style.md` — follow Go naming, comments, errors, concurrency, and testing rules.

## Coding Style Completion Gate

For any Go code addition or modification, `docs/development/coding-style.md` is
not just background reading. It is a mandatory completion gate.

Before claiming that Go work is done, agents must perform a manual coding-style
review of every changed Go file. Passing `go test`, `go build`, `gofmt`,
`go vet`, lints, or IDE diagnostics is not sufficient.

The review must specifically verify high-maintenance documentation quality:

1. Every new or changed exported package, type, interface, function, method,
   const, or var has a doc comment that explains contract, boundary, caller
   obligations, error semantics, lifecycle, concurrency, security, or
   compatibility where relevant.
2. Important unexported boundaries also have valuable comments. This includes
   use cases, repository interfaces, Fiber adapters, config loaders, Mongo
   adapters, Temporal workers and activities, Ansible execution, token/credential
   code, middleware, long-running loops, goroutine lifecycles, stream handling,
   and cross-component integration points.
3. Comments that only restate an identifier, type, or obvious code behavior are
   treated as missing comments and must be rewritten before continuing.
4. Code involving JWTs, passwords or credential storage, admin authorization,
   Mongo writes and uniqueness constraints, config precedence and defaults,
   request IDs, goroutines, context cancellation, stream ownership, reconnect
   backoff, or resource cleanup must document the safety assumptions and
   maintenance constraints.
5. New packages must have package-level comments that explain ownership,
   component boundary, and what must not be placed in that package.

If any changed Go file fails this documentation gate, the task is not complete.
Fix the comments immediately before moving to the next milestone, to-do item, or
final response.

For large implementations that add or change multiple packages, agents must do a
dedicated final comment pass after the functional code works. The final response
must not describe the task as complete unless this pass has been performed.

## API Work

This component is swallow's only API-owning component. When the task reads,
creates, modifies, deprecates, deletes, consumes, or validates API behavior,
read and maintain:

1. `docs/development/api-contracts/README.md` — understand the swallow API contract workflow.
2. `docs/development/api-contracts/outline.md` — locate and maintain the API contract index.
3. The relevant component outline and API contract document listed by the outline.

If the task changes an API owned by swallow, update or create the provider-owned
API contract before or together with implementation. If the relevant API is only
listed as planned, create or promote the contract before implementation or
integration.

Do not infer routes, fields, status codes, error formats, authentication,
authorization, or behavior semantics from code. The contract is the source of
truth, and implementation must not define API behavior that is absent from it.

## Component Boundary

This directory is the `api-server` component: the Data Center API
Service — an HTTP REST API plus the background reconcile / poll loops that own
intent, policy, and identity mapping.

Keep the component boundary clear even inside the monorepo: `dashboard` and
`cli` consume the `api-server` HTTP API through its published contract and must
not reach into `api-server` internals; `api-server` owns the API contracts.

## Domain Language

Names in code must match swallow's ubiquitous language. This component's domain
terminology is owned by swallow's glossary at the repository root
(`docs/development/glossaries/`), not by this component. When a task introduces or
changes a domain term, entity, status value, or data model concept, confirm the
term against swallow's glossary first; if the term is missing or ambiguous,
resolve it there before implementing.

## Plans

When creating or updating an implementation plan for this project, store the
manuscript under the repository root `../docs/plans/manuscripts/` as
`YYYYMMDD-<short-topic>.md`, and update an existing plan for the same topic
instead of creating a duplicate. Do not create a component-local plan tree.

Do not proactively read files directly under `../docs/plans/`. They are historical
records, and should only be read when the user asks for planning history or plan
consolidation.

## Commit Messages

When asked to draft or validate a commit message, read and follow:

- `docs/development/commit-spec.md`

Commit messages MUST be written in English, regardless of the chat language.

## Skills

Task-specific operating guides live under `skills/`. When a task may benefit from
one, read `skills/README.md` and follow its lookup workflow before acting.
Skills never replace this file or the documents it requires.
