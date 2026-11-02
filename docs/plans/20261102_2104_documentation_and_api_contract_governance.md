# Documentation, ADR, and API Contract Governance

## Purpose

Consolidate the 2026-10-30 to 2026-11-02 manuscripts that reorganized how swallow
records decisions, contracts and guidance: ADRs reduced to decision cores with
current behavior moved into specs, machine-readable API contracts (OpenAPI) with
provider and consumer checks, consistent API error semantics, a single statement
of swallow's design principles, and a reading-guide overhaul validated by
reading simulations so agents and developers read what a task needs.

The work is implemented. This plan keeps the rules it established, the
acceptance results, and the remaining gaps. The live tracker of unfinished items
is `docs/plans/manuscripts/20261102-architecture-review-v2.md`.

## Source Scope

This consolidation is one of three produced together from 14 draft manuscripts
in `docs/plans/manuscripts/` (excluding the consolidation specification
`README.md` and the live tracker `20261102-architecture-review-v2.md`). This
plan covers 5 of them:

- Shared with the other two plans: `20261030-architecture-review.md` (AR-08,
  AR-11, AR-13, AR-14, DF-22 to DF-37, and the reading-simulation appendix).
- `20261030-pre-revision-cleanup.md`: ADR consolidation (AR-11) and OpenAPI
  contracts (AR-08).
- `20261031-df-api-error-semantics.md`: API error and input semantics (DF-02,
  DF-04, DF-05, DF-13, DF-14, DF-18, DF-21).
- `20261101-design-principles.md`: one statement of swallow's design principles
  (DF-23).
- `20261102-doc-navigation.md`: reading guidance and rule fixes (DF-26 to
  DF-37).

Companion plans: `20261102_2104_store_driven_execution_and_desired_state_migration.md`
and `20261102_2104_composable_core_and_extension_modules.md`.

## Consolidated Background

By 2026-10-30 there were 66 ADRs in ten weeks, with chains of ADRs revising each
other; many recorded single-feature designs rather than architecture, so
understanding a subsystem meant stacking several ADRs. API contracts were about
9,000 lines of Markdown with no schema; whether provider, contract and dashboard
agreed could only be checked by reading. Comparing contracts with code turned up
many mismatches (authorization wording, response shapes, undocumented fields and
routes, status codes) and a list of suspected defects.

On 2026-11-01 a reading simulation (one agent setting tasks and evaluating;
fresh agents following the guidance literally from the root `AGENTS.md`) showed
agents read 2.5 to 8 times what a task needed: always-read lists and long specs
were read whole, component guides and coding styles were pulled into every task,
contract lookups went through six redirect layers, index summaries restated
ownership and drifted, the same rule existed in several copies, specs retold
shared mechanisms and kept the old model after migrations, and "how to add X"
lived only in code. A human onboarding simulation found no system overview.

## Confirmed Decisions

### ADRs (AR-11)

- An ADR answers "why": Context, Decision, Alternatives considered,
  Consequences. Accepted ADRs are frozen; the only allowed edits are the Status
  line (`Superseded by NNN`, `Refined by NNN`) and the current-spec link.
- Current behavior lives in specs (`docs/development/`, glossary, API contracts);
  behavior changes update specs; a new ADR is written only when the decision
  itself changes.
- ADRs are never deleted or renumbered (hundreds of "decision NNN" references in
  code and documents stay valid); reduced originals stay in git history; no
  rationale is invented; every alternative considered is kept.
- Feature ADRs were reduced and their details moved into specs, several of which
  were created for this: `os-provisioning.md`, `server-enrollment-and-boot-media.md`,
  `workflow-execution.md`, `access-and-credentials.md`, `installation.md`.
  Architecture ADRs were kept verbatim.
- Functional revision chains are resolved in the target spec, with every ADR in
  the chain pointing to the same section: addressing 012→018→058, recovery
  020→033→036→059→060→061, SSH keys 039→041→045, Boot Media 047→049→057,
  installation 040→050→051→052. Relationships are recorded in the index's
  relationship column.
- `docs/decisions/README.md` states when to add an ADR (changing a component
  boundary or who owns a fact, a rule all later features must follow, adopting or
  rejecting a major dependency or runtime topology; single-feature designs belong
  in specs) and how to cite one ("decision NNN", with a link and short title at
  the first mention in each document).

### Machine-readable API contracts (AR-08)

- OpenAPI 3.0.3, hand-written and contract-first, is the authority for wire shape
  (paths, methods, parameters, request and response schemas, status codes, error
  codes, authentication); Markdown contracts keep behavioral semantics. Schemas
  are not generated from code.
- Where contract and implementation disagreed, the first round described current
  behavior and fixed the Markdown; suspected implementation defects were recorded
  for separate plans.
- Layout: fragments in `api-server/docs/development/api-contracts/openapi/areas/`
  plus `common.yaml`, merged into a committed `openapi.yaml` by
  `internal/shared/apicontract` (duplicate names fail).
- Checks: the merged document validates (`TestContractIsValid`); the bundle is
  current (`TestBundledFileIsCurrent`, regenerated with
  `UPDATE_OPENAPI=1 go test ./internal/shared/apicontract`); registered routes and
  documented operations match both ways
  (`TestRegisteredRoutesMatchTheContract`); delivery tests validate responses
  (`apicontract.ResponseValidator`).
- The dashboard generates types from `openapi.yaml`
  (`dashboard/src/infrastructure/api/generated/openapi.ts`, `npm run
  contract:generate`), checks freshness (`npm run contract:check`), and proves its
  hand-written types accept the generated responses at compile time; known
  mismatches are listed in `contractCompatibility.ts` (`KnownContractMismatches`)
  and fail to compile once fixed.
- Planned contracts are not in OpenAPI; the CLI forwards JSON/YAML unchanged.

### API error and input semantics

- State constraints are always 409 (`conflict`), including a target not in the
  required state.
- Unsupported capabilities are always 400, including network configuration.
- Request errors are 400, including targets spanning Sites.
- Unknown upstream or lock state is 503.
- The Kubernetes explorer returns 404 `not_found` and 409 `conflict` for the
  cluster's not-found and already-exists answers.
- Alert acknowledge: the path fingerprint must be an alert Alertmanager currently
  reports on that Site (else 404); `matchers` are optional and must be labels of
  that alert (else 400).
- Overview `operations.active` counts every non-terminal state.
- Platform deploy reads `deployTarget` first; unknown values are 400.
- Changing an `error.code` mapping is a breaking change; such commits carry
  `BREAKING CHANGE`.

### Design principles (DF-23)

- One authoritative statement: "swallow 的設計主張" at the top of the root
  `docs/development/architecture-spec.md`, shared by developers and agents;
  CONTRIBUTING, README and the introduction link inward; `AGENTS.md` only routes to
  it.
- The idea, in the user's words: swallow's core is generic and, composed with
  providers (provider adapters) and plugins (extension modules), becomes
  out-of-the-box datacenter management; integrated platforms must be done
  correctly and deeply; changing target platform or GPU vendor must not require
  redesigning core.
- User decisions: the Core Domain is intent and convergence (plugins are
  supporting domains); the trade-off order is: do not destroy data or workloads,
  then single truth and correctness, then core generality, then compatibility and
  convenience.
- Development specs stay in Traditional Chinese.

### Reading guidance (DF-26 to DF-37)

- The plan rule's purpose is where plans an agent tool writes are stored (root
  `docs/plans/manuscripts/YYYYMMDD-<topic>.md`), not a requirement to plan every
  change; listing file names to find a plan on the same topic is allowed.
- Root `AGENTS.md` routes by task type: Always Read is only the codebase
  structure's Components and Code Lookup sections; a Reading by Task table names
  what each kind of task reads; long specs and contracts are read by reader
  section, headings and the sections involved (contract opening sections plus the
  routes, parameters and fields involved; response examples only when used); a
  link that names a section needs only that section. Areas without a domain spec
  use their contract.
- The design principles are read for design, review and placement work (user
  decision), not for every task.
- Component guides apply when a task changes that component's code or reviews a
  diff of it; planning reads what doing would, except coding style, which waits
  until code is written; explaining reads neither.
- Rules have one authoritative copy (commit spec, skills with IDE wrappers,
  component rules, Managed Software kind lists, plan rule, completion gates).
- Committed files contain no AR/DF tracking codes; lab hostnames and addresses
  are placeholders and lab rules live in `skills/lab-verification/`; the product
  name is lowercase swallow in documents and comments (dashboard text, API
  messages and the `Managed by Swallow` markers installed on hosts keep their
  capitalization).
- `scripts/check-docs.py` checks links, backtick repository paths in current
  specs, retired terms, and tracking codes.
- New development documents: `docs/development/system-overview.md` (feature map,
  processes and background work, Platform deployment sequence, collections and
  writers, starter terms, code map) and `docs/development/development-and-testing.md`
  (dev stack, test locations and fakes, environment variables, full checks).
- Dashboard errors show request IDs through a shared failure port and component.
- Acceptance: nine of ten agent simulations read within 1.5 times what they
  needed; the small defect-fix task stayed above it (its necessary reading was
  about 430 lines, 186 of them the root `AGENTS.md`), and the user accepted DF-26
  as mostly complete. The human onboarding goals are answerable from the system
  overview plus at most three specs.

## Architecture and Design Principles

- Agent-oriented documents (AGENTS files, development specs, contracts,
  glossary, ADRs, skills) are single-source and never link to public documents;
  public documents may link inward.
- Specs describe current behavior; ADRs keep the why; plans are history and are
  read only for planning history or consolidation.
- Contracts are owned by the provider; consumers read the contract, never the
  provider's internals.
- Guidance states its readers and scope so a task reads only what it needs.

## Functional Scope

Delivered:

- Reduced ADRs and five new feature specs; the ADR README threshold, format and
  citation rule.
- OpenAPI for every Active contract with provider and dashboard checks.
- Corrected error-code mappings and input validation.
- The design-principles section and inward links from public entry points.
- Task-based reading guidance, component guides scoped to code work, the system
  overview, the development and testing guide, documentation checks, dashboard
  request IDs, and the retired Operation/Step wording replaced in specs and
  contracts.

## Constraints and Rules

- ADRs are frozen after acceptance; never renumber or delete; never fabricate
  history.
- A change to a route, query parameter, field, status or error follows the
  contract workflow (`api-server/docs/development/api-contracts/README.md`):
  update the fragment, regenerate `openapi.yaml`, regenerate dashboard types, and
  update the Markdown contract when behavior changes.
- Development specs never link to `docs/en/`, `docs/zh-TW/` or `.zh-TW.md` pages.
- Retired terms and tracking codes must not appear in current specs; vendor-term
  counts in core may only fall.
- Every documentation commit passes `python3 scripts/check-docs.py`.

## Data Model and Format Notes

- Reduced ADR format: title, Status, Date, current spec, then Context, Decision,
  Alternatives considered, Consequences; a revision note when an amendment changed
  the decision.
- OpenAPI areas: system, auth, sites, servers, provisioning, platforms, workflows,
  software, monitoring (plus areas added later, such as change plans and modules).
- Contract opening sections: status, owner and consumer components, purpose,
  related glossary terms, endpoint list.

## CLI / API / Config Notes

- `UPDATE_OPENAPI=1 go test ./internal/shared/apicontract`; `npm run
  contract:generate`; `npm run contract:check`; `python3 scripts/check-docs.py`.
- The full check commands per component are in
  `docs/development/development-and-testing.md` §6; the repository has no CI.

## Implementation Plan

Implemented:

- ADR consolidation and OpenAPI contracts: two commits in the pre-revision
  cleanup round (2026-10-30).
- Error semantics: `7a80dde`, `30f0972`, `c49e1d2` (breaking), `2b4c979`
  (breaking), `434486d`.
- Design principles: `3ec78b6`, `866b832`.
- Reading guidance: `189ecde`, `414c7b6`, `5221f18`, `6f77ee0`, `3adc861`,
  `b30776a`, `771c672`, `7fbd8ae`, `60910b2`, `af6577b`, `19b60e7`, `f383938`,
  `7089d03`.

Remaining (tracked in the v2 tracker): AR-08 (CI for contract checks; deciding
whether to drop Markdown field tables that duplicate OpenAPI), AR-11 (the
architecture ADR revision chains), AR-13 and AR-14 (dashboard pages and generated
transport types, with DF-16 and DF-49), DF-15 (wire-shape issues), DF-24
(functional specs for monitoring and Site/Integration), DF-52 (public pages that
still describe the old model).

## Non-goals

- CI and a PR template (not done in these rounds).
- Rewriting dashboard adapters to generated types in the contract round (AR-14).
- Changing implementation behavior during the contract round.
- An English version of the development specs for now.

## Open Questions

- When and how to remove Markdown field tables that duplicate OpenAPI.
- How to consolidate the architecture ADR revision chains while ADRs stay frozen
  (for example marking the currently effective ADR of each chain in the index, or
  a consolidating ADR).
- Whether the design principles and the module authoring guide need English
  versions when external module contributors join.
- Alert filtering by Platform: match the alert's `platformId` label or the
  Platform's membership (part of DF-24).

## Future Work

- DF-24: functional specs for monitoring relationships and Site/Integration
  lifecycle, with `AGENTS.md` routing and the `Alert` glossary term.
- AR-08 CI; AR-11 chain consolidation; AR-13, AR-14.
- Rerun the reading simulations when the guidance changes substantially; the
  remaining overrun on small defect fixes comes from mandatory guardrail sections
  and the fixed cost of the root `AGENTS.md`, which could be split further if
  needed.
