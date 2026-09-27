# Dashboard Agent Entry Guide

This file is the entry point for AI agents working in the dashboard codebase.
All paths in this file are relative to the dashboard project root.

## Required Reading

Before planning, reviewing, or explaining dashboard work, read:

1. `docs/development/architecture-spec.md` — understand the dashboard Clean Architecture boundaries.

Before adding or modifying any code, read:

1. `docs/development/architecture-spec.md` — understand that dashboard uses Clean Architecture as its design principle.
2. `docs/development/coding-style.md` — follow TypeScript, React, accessibility, comments, and testing rules.

## Coding Style Completion Gate

For any TypeScript, React, styling, route, hook, adapter, or support code
addition or modification, `docs/development/coding-style.md` is not just
background reading. It is a mandatory completion gate.

Before claiming that dashboard work is done, agents must perform a manual
coding-style review of every changed source file. Passing `npm run lint`,
`npm run build`, TypeScript, or IDE diagnostics is not sufficient.

The review must specifically verify high-maintenance documentation, UI, and
accessibility quality:

1. Every new or changed exported component, hook, type, interface, constant,
   helper, use case, port, adapter, provider, or route-level boundary has JSDoc
   that explains usage context, data ownership, props or return semantics, UI
   state contract, side-effect lifecycle, accessibility intent, security, or
   compatibility where relevant.
2. Important unexported boundaries also have valuable comments. This includes
   page components, route guards, app shell wiring, custom hooks, API adapters,
   DTO mappers, storage adapters, permission helpers, form mappers, presenters,
   and cross-layer integration points.
3. Comments or JSDoc that only restate an identifier, prop type, component name,
   or obvious JSX behavior are treated as missing comments and must be rewritten
   before continuing.
4. Code involving session or token storage, current-user and admin visibility,
   backend DTO mapping, browser storage, network effects, route protection, form
   validation, loading/error/empty state, accessibility workarounds, or
   theme/i18n boundaries must document the safety assumptions and maintenance
   constraints.
5. Changed React UI must preserve accessible labels, semantic controls, keyboard
   usability, meaningful status and error messaging, and real-data behavior.
   Status must never be conveyed by colour alone.
6. Shared features must use a single shared, composable component reused across
   pages (see "元件共用與組合（DRY）" in `docs/development/coding-style.md`). A
   composed card or section must not be copy-pasted across pages, and adding a
   field or sub-section to a shared feature must be done on the shared component
   so every page stays consistent. Duplicating a composed block instead of
   reusing or extending the shared component fails this gate.
7. Layer direction must hold: nothing under `src/domain/` or `src/application/`
   may import React, the router, an HTTP client, storage, or a UI library, and
   nothing under `src/presentation/` may import `@/infrastructure/**`.

If any changed source file fails this documentation, accessibility, real-data, or
layering gate, the task is not complete. Fix the issue immediately before moving
to the next milestone, to-do item, or final response.

For large implementations that add or change multiple modules, agents must do a
dedicated final JSDoc / accessibility / state pass after the functional code
works. The final response must not describe the task as complete unless this pass
has been performed.

## API Consumption

Dashboard consumes APIs owned by provider components; it owns no API contracts.
When the task creates, modifies, consumes, or validates backend integration, use
the provider component's API contract as the source of truth, and read it before
implementing.

Dashboard must not infer API behavior from provider implementation details, and
must not integrate against an endpoint whose contract is only Planned. If a
needed contract is missing, ambiguous, or contradicts what the UI needs, raise it
against the provider's contract instead of coding around it.

The production container binds real HTTP adapters only. Deterministic API fixtures
live in Playwright tests for verification; they are test drivers, never an
implementation path or de-facto definition of API behavior.

## Domain Language

Names in code must match swallow's ubiquitous language. This component's domain
terminology is owned by swallow's glossary at the repository root
(`docs/development/glossaries/`), not by this component. When a task introduces or
changes a domain term, entity, status value, or data model concept, confirm the
term against swallow's glossary first; if the term is missing or ambiguous,
resolve it there before implementing.

A UI label may differ from a domain value — for example status `maintain` is
displayed as "Maintenance" — but the domain value is what flows through filters,
API payloads, and persistence.

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
