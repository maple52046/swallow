# cli Agent Entry Guide

This file is the entry point for AI agents working in the `cli` component: the
`swallow` command-line client. All paths in this file are relative to
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

Before changing CLI code, or planning or reviewing such a change, read:

1. `../AGENTS.md` — the repository entry guide and its always-read documents.
2. `docs/development/architecture-spec.md` — the CLI's own Clean Architecture
   layering (command → client/output → config).

Explaining CLI behavior needs only the usage manual and the command code, even when you read code to
confirm a detail. Before adding or modifying code,
also read:

1. `docs/development/coding-style.md` — the CLI-specific additions. The CLI
   follows the Go coding style at `../api-server/docs/development/coding-style.md`
   in these sections: 格式與命名, 註解與文件 (except the Clean Architecture
   layer notes, which describe api-server's layers), 錯誤處理, Imports 與 Package
   組織, and 測試規範; add API 與語言使用 when the change starts goroutines.
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

Commands target Active routes only; when a route becomes Deprecated, the CLI
moves to its replacement rather than keeping a command for the alias.

## Domain Language

Command names, flags, and help text must match swallow's ubiquitous language
(repository glossary at `../docs/development/glossaries/`). A user-facing label
may read naturally, but the value sent through query parameters and request
bodies must be the domain value defined by the contract and glossary.

## Coding Style Completion Gate

For any Go addition or change, `docs/development/coding-style.md` and the Go
style sections it applies (listed under Required Reading) are a mandatory
completion gate, not background reading.
Before claiming CLI work is done, review the Go declarations you added or changed
(not untouched ones in the same file) for doc comments
that explain contract, caller obligations, error semantics, and boundaries — not
comments that merely restate an identifier. Passing `go build`/`go vet`/`gofmt`
is necessary but not sufficient.

Run before claiming done (the CLI's checks in
`../docs/development/development-and-testing.md` §6, which you need not read): `gofmt -l .`
(no output), `go vet ./...`, `go test ./...`, `go build ./...`. How CLI tests stand in for the api-server is in
`../docs/development/development-and-testing.md` §5.

## Public Documentation

A change that adds or changes a command, flag, or output updates `docs/usage.md`
and `docs/usage.zh-TW.md` in the same commit; a new command group also updates
`README.md`, `README.zh-TW.md`, and the public command index
`../docs/en/reference/cli.md` with its `../docs/zh-TW/reference/cli.md` twin, and
any public guide that says the CLI lacks the area. Today those are the Managed Software
guide ("does not yet expose a `software` command group"), the API integration reference
("Managed Software currently requires the Dashboard or HTTP API"), the introduction
("Managed Software currently uses the Dashboard or HTTP API"), and the command index
itself, each with its `docs/zh-TW/` twin; search `../docs/` for "Dashboard or HTTP API"
and the area's name to find others. When
the new commands cover only part of the area, rewrite those statements to say what is
still missing rather than deleting them. Run
`python3 scripts/check-docs.py` from the repository root after changing them.
Adding `cli` to the contract's Consumer Components belongs to the same change
(the contract README's rule for consumers).

The CLI does not cover every Active route yet: Managed Software (with registry
credentials and the Docker explorer), the Server Default User, the provisioner
HTTP proxy, and Subnet DNS have no commands, and the public pages above say so.
Adding a command for one of them is an ordinary consumer feature; update those
statements, and this paragraph, in the same commit.

## Plans

Plans follow the root `../AGENTS.md` (Plans): a plan you write is also saved in
the repository root `../docs/plans/manuscripts/` as `YYYYMMDD-<short-topic>.md`,
never in a component-local plan tree.

## Commit Messages

When asked to draft or validate a commit message, follow
`../docs/development/commit-spec.md`. Commit messages MUST be in English.
