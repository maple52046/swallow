# api-server Agent Entry Guide

This file is the entry point for AI agents working in swallow's backend, the
`api-server` component. All paths in this file are relative to this component's
root (`api-server/`).

## Required Reading

Before changing api-server Go code (or reviewing a code diff), read both; planning a change, or reviewing a
design without code, reads only the first:

1. `docs/development/architecture-spec.md` — the Clean Architecture boundaries: Dependency Rule, Prohibited
   Designs, Completion Gate; Layout when the change adds a file or package; and the sections for what the
   change touches (Provider Ownership, Provider Adapter Boundary, Durable Workflow Boundary, Store and
   Transaction Boundary, Configuration Boundary, Error Boundary; a repository query reads Data Crossing
   Boundaries and Store and Transaction Boundary). Read "2026-10-31 搬移前的路徑" only when you meet a path
   from before that move.
2. `docs/development/coding-style.md` — Go rules. Every change reads 核心原則, 格式與命名, 註解與文件, and Agent
   執行規則; add 錯誤處理, API 與語言使用, Imports 與 Package 組織, or 測試規範 when the change touches that.

Explaining api-server behavior needs neither, even when you read code to confirm a detail.

A change confined to host automation content under `modules/` (playbooks, roles, the playbook manifest, a
module's layout) reads `../docs/development/module-authoring.md` instead; for the Go content tests it adds,
coding-style's 測試規範 and 註解與文件 are the completion gate. A module's Go hooks are Go code and follow the
list above.

When the change adds or changes Go tests other than host automation content tests, read
`../docs/development/development-and-testing.md` §3 for
where each kind of test lives, the shared fakes, and the test environment variables (host automation
content tests are in module-authoring §6); its §6 has the complete check commands, including the Mongo
integration tests.

## Coding Style Completion Gate

For any Go code addition or modification, `docs/development/coding-style.md` is
not just background reading. It is a mandatory completion gate.

Before claiming that Go work is done, agents must perform a manual coding-style
review of the Go code they changed. Passing `go test`, `go build`, `gofmt`,
`go vet`, lints, or IDE diagnostics is not sufficient.

The gate covers the declarations a change adds or modifies. Existing declarations
in the same file that the change does not touch are out of scope; fill their gaps
in a separate commit rather than mixing them into this one.

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

If any changed declaration fails this documentation gate, the task is not complete.
Fix the comments immediately before moving to the next milestone, to-do item, or
final response.

For large implementations that add or change multiple packages, agents must do a
dedicated final comment pass after the functional code works. The final response
must not describe the task as complete unless this pass has been performed.

## API Work

This component is swallow's only API-owning component. When the change adds, changes, deprecates, or
removes a route, query parameter, field, status, or error (the contract workflow), read and maintain:

1. `docs/development/api-contracts/README.md` — the API contract workflow.
2. `docs/development/api-contracts/api-server/outline.md` — the contract index, unless a spec or the
   root `AGENTS.md` already names the contract.
3. The relevant contract, and in it the sections for the routes, parameters, and fields involved.

If the task changes an API owned by swallow, update or create the provider-owned
API contract before or together with implementation. If the relevant API is only
listed as planned, create or promote the contract before implementation or
integration.

The contract has two forms (see the OpenAPI section of the contract README): the
OpenAPI fragment under `docs/development/api-contracts/openapi/areas/` owns the
wire shape, and the Markdown contract owns behavior. A route or wire-shape change
updates the fragment, regenerates `openapi.yaml`
(`UPDATE_OPENAPI=1 go test ./internal/shared/apicontract`), and regenerates the
dashboard types (`npm run contract:generate` in `../dashboard`). New HTTP test apps
that serve real routes install `apicontract.ResponseValidator(t)` first.

Do not infer routes, fields, status codes, error formats, authentication,
authorization, or behavior semantics from code. The contract is the source of
truth, and implementation must not define API behavior that is absent from it.

## Component Boundary

This directory is the `api-server` component: the Data Center API
Service. One binary runs the HTTP API (which owns intent, policy, and identity
mapping), the worker (desired-state controllers and the Workflow driver), the
Ansible executor, and migrations.

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

Plans follow the root `../AGENTS.md` (Plans): a plan you write is also saved in
the repository root `../docs/plans/manuscripts/` as `YYYYMMDD-<short-topic>.md`,
never in a component-local plan tree.

## Commit Messages

When asked to draft or validate a commit message, read and follow the repository
commit spec, `../docs/development/commit-spec.md`.

Commit messages MUST be written in English, regardless of the chat language.

## Skills

Task-specific operating guides live in the repository's `../skills/`. When a task
may benefit from one, read `../skills/README.md` and follow its lookup workflow
before acting. Skills never replace this file or the documents it requires.
