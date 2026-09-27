# Swallow CLI Agent Entry Guide

This file is the entry point for AI agents working in the `cli` component: the
`swallow` operator command-line client. All paths in this file are relative to
this component's root (`cli/`).

## Component Boundary

This directory is the `cli` component: an HTTP **consumer** of the `api-server`
published contract, at the same level as `dashboard`. It builds the `swallow`
binary (distinct from the `api-server` service binary `swallow-api`).

Keep the boundary clear even inside the monorepo:

- The CLI integrates only through the provider-owned API contract under
  `../api-server/docs/development/api-contracts/`. It must not import
  `api-server` internal packages or replicate its domain models.
- The CLI is a conformist consumer (see the repository architecture spec): it
  follows the contract as published. Request bodies for complex endpoints are
  passed through as JSON/YAML files so payload fields track the contract rather
  than a hand-copied struct that could drift.

This component is a separate Go module (`github.com/maple52046/swallow/cli`); it
does not share `api-server`'s module.

## Required Reading

Before planning, reviewing, or explaining CLI work, read:

1. `../AGENTS.md` — the repository entry guide and its always-read documents.
2. `docs/development/architecture-spec.md` — the CLI's own Clean Architecture
   layering (command → client/output → config).

Before adding or modifying any code, also read:

1. `docs/development/coding-style.md` — the CLI follows the swallow Go coding
   style; this file records the small CLI-specific additions.
2. The relevant Active contract under
   `../api-server/docs/development/api-contracts/api-server/` for any command you
   add or change. The contract is the source of truth for paths, methods,
   fields, status codes, and auth.


## API Consumption

The CLI consumes APIs owned by `api-server`; it owns no API contracts. Do not
infer API behavior from `api-server` implementation files, and do not build a
command against an endpoint whose contract is only Planned. If a needed contract
is missing or ambiguous, raise it against the provider's contract instead of
coding around it.

The CLI intentionally does **not** expose deprecated compatibility aliases
(`/operations`, `/clusters`, single-server `deploy`/`release`) as first-class
commands; it targets the canonical Active surface.

## Domain Language

Command names, flags, and help text must match swallow's ubiquitous language
(repository glossary at `../docs/development/glossaries/`). A user-facing label
may read naturally, but the value sent through query parameters and request
bodies must be the domain value defined by the contract and glossary.

## Coding Style Completion Gate

For any Go addition or change, `docs/development/coding-style.md` and the
repository Go style are a mandatory completion gate, not background reading.
Before claiming CLI work is done, review every changed Go file for doc comments
that explain contract, caller obligations, error semantics, and boundaries — not
comments that merely restate an identifier. Passing `go build`/`go vet`/`gofmt`
is necessary but not sufficient.

Run before claiming done: `gofmt -l .` (no output), `go vet ./...`,
`go build ./...`, `go test ./...`.

## Plans

When creating or updating an implementation plan for this project, store the
manuscript under the repository root `../docs/plans/manuscripts/` as
`YYYYMMDD-<short-topic>.md` (see the repository plan convention), not in a
component-local directory.

## Commit Messages

When asked to draft or validate a commit message, follow
`../docs/development/commit-spec.md`. Commit messages MUST be in English.
