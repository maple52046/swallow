# Architecture and Development Governance

## 1. Purpose

Preserve the durable architecture, documentation-governance, composition-boundary, and development
workflow decisions produced by the architecture review and the pre-monitoring overhaul. This plan records
the target state and the remaining structural work without reproducing the implementation diary.

## 2. Source Scope

Consolidated from:

- `docs/plans/manuscripts/20261102-adr-revision-and-user-term.md`
- `docs/plans/manuscripts/20261102-architecture-review-v2.md`
- `docs/plans/manuscripts/20261103-pre-monitoring-overhaul.md`

Related monitoring, Platform, dashboard, artifact, and upload manuscripts were also reviewed and are
consolidated in sibling plans written at the same timestamp. Per the consolidation instruction,
`20261107-architecture-review-v3.md` was neither read as an input nor modified.

## 3. Consolidated Background

The architecture review began with two root defects: Workflow execution state had two sources of truth,
and cross-feature orchestration had no clear owner. Store-driven Workflow execution, desired-state
resources, Change Plans, provider interfaces, and module contracts resolved those foundations. The
pre-monitoring overhaul then removed API-owned background loops, moved observation work to the worker,
drove SSE from the change feed, and made security-critical dependencies construction-time requirements.

The remaining structural work is evolutionary: finish the second desired-state migration, move Workflow
composition out of `internal/app`, complete module ownership, make diagnostics and wait policy universal,
and establish automated contract checks.

## 4. Confirmed Decisions

- The store is the only owner of Workflow and Task state; Temporal is an execution adapter.
- A Workflow is a declarative Job/Task plan. The engine must not branch on product-specific kinds.
- Swallow owns desired state and identity mapping; external systems own observed facts.
- Destructive convergence uses a Change Plan; confirmed user writes may approve on submit unless the
  runtime safety policy requires separate review.
- API processes are stateless. Observation passes and controllers run in the worker, while artifact
  retention belongs to the executor.
- Security dependencies are mandatory constructor inputs; missing dependencies fail at startup.
- ADR bodies describe the current decision. Revisions append one concise dated history line. ADRs are
  merged only along a true revision chain, retaining the current ADR number and recording old-number
  mappings in the decisions index.
- Documentation calls people `user`. `operator` is reserved for product or Kubernetes operator concepts.
- New user-visible API behavior is contract-first; OpenAPI owns wire shape and Markdown owns behavioral
  semantics.
- There is no compatibility layer for unpublished APIs without external consumers; persisted data remains
  protected by migrations.

## 5. Architecture and Design Principles

- Core remains provider-, product-, Platform-, and GPU-vendor-neutral.
- Provider adapters describe how Swallow communicates with an external system; extension modules describe
  what Swallow manages.
- Every fact has one writer and is classified as desired, observed, or derived.
- Controllers converge from desired and observed state; events wake them but do not carry intent.
- Cross-document invariants are transactional, not repaired later by sweeps.
- Automation is idempotent ensure behavior.
- Unknown outcomes are not guessed: unavailable observations become stale, unsupported capabilities are
  explicit, and unprovable execution outcomes require user attention.
- Secrets enter Workflow history only by reference; durable diagnostics are redacted without discarding
  actionable causes.
- Component boundaries remain logical even inside the monorepo; dashboard and CLI consume only the
  api-server contract.

## 6. Functional Scope

Completed foundations include:

- Store-driven execution, dispatch fencing, typed completion behavior, and removal of the Mongo execution
  queue.
- Desired-state migration for provisioner-realized catalog resources and Platform.
- Change Plans, module contract v1, Platform/AMD/Software registration, and module UI for Platform and AMD.
- Change-feed SSE, worker-owned observation passes, and removal of inferred-intent background loops.
- Node Exporter as Managed Software, installed by OS deployment and continuously ensured by a controller.
- OpenAPI contract validation, route/contract parity checks, generated dashboard types, and partial adapter
  migration.

Remaining functional scope:

- Server desired-state migration for Power Configuration, Server Default User, Boot Media, OS deployment
  intent, Release/Recover, and eventual `reapply`.
- Power on/off through Workflow resource ownership.
- Declarative Workflow templates and validation-scenario catalogs.
- Platform and AMD Job composition and trusted variables moved into modules.
- Executable automation for currently blocked Platform spec changes.
- Software module UI migration and build-time module selection with a generated support matrix.
- Universal structured diagnostics and bounded waits outside current Workflow paths.

## 7. Constraints and Rules

- Do not change persisted Workflow kinds, completion types, Task IDs, trusted-variable names, or Platform
  document fields without a schema migration.
- Module hooks perform no I/O inside transactions and modules do not import each other.
- The API, worker, and executor must construct the same module registry and plan catalog.
- Existing un-migrated behavior remains authoritative until its feature is migrated; update the feature
  spec in the same change.
- Power Configuration migration must first resolve decision 054 versus the sealed-secret desired-state
  rule, including write-only password comparison and adoption.
- A Workflow stopped for attention retains resource ownership; desired-state migration must avoid a hold
  deadlock when a user changes Power Configuration during that wait.
- Contract changes update Markdown, OpenAPI, generated dashboard types, and consumers together.
- Development specifications remain Traditional Chinese until external module contributors create a
  demonstrated need for English versions.
- Repository checks and component completion gates remain mandatory; do not treat passing compilers as a
  substitute for coding-style, accessibility, or documentation review.

## 8. Data Model and Format Notes

- Server spec currently owns manual tags and Zone/Pool placement; the second migration adds further fields
  only after each is classified.
- Workflow diagnostics use stable code, stage, target, endpoint, operation, failure class, retryability,
  cause, remediation, observation, attempts, elapsed time, budget, provider events, and correlation IDs.
- Intent snapshots must move from sensitive-key blacklists to producer-owned allowlists or typed public
  projections.
- Module manifests can declare capabilities, routes, playbooks, Task/Workflow kinds, completions, and
  artifacts. Platform schemas, Workflow templates, validation catalogs, compatibility declarations, and
  broader Software schemas remain future contract additions.
- ADR revision history is append-only and appears at the end of each ADR.

## 9. CLI / API / Config Notes

- Power action routes still call the provider synchronously; future behavior requires contract changes for
  Workflow-backed actions.
- `reapply` has no route or semantics yet and requires a contract decision.
- Dashboard adapters should use generated OpenAPI request/response types and map them into domain types;
  hand-written transport types are transitional.
- Contract checks exist as local commands but the repository has no CI pipeline.
- The dev MongoDB container still needs a higher `nofile` limit and cleanup of interrupted test databases.
- Configuration and environment names introduced by migrations must remain provider-neutral and be
  documented in installation and testing guidance.

## 10. Implementation Plan

1. Before validation work, replace remaining functional `Attach*` wiring with constructor inputs, add a
   constructor for `providerStepExecutor`, remove dead wiring, and add assembly tests for each process role.
2. Raise the dev MongoDB `nofile` limit, consolidate Mongo test cleanup, and document the test behavior.
3. Implement declarative Workflow templates as validation's first feature. Update decision 075 and let
   validation scenarios be the first native template users.
4. Move existing Platform/AMD launch composition into module-owned definitions after the template format
   is stable.
5. Complete the second Server desired-state migration after resolving power credential ownership, Server
   Lock ownership, power action semantics, incremental field adoption, and the `reapply` API.
6. Migrate remaining dashboard API adapters to generated types, together with nullable-field protection.
7. Add CI for provider contract checks, dashboard `contract:check`, layout tests, and the complete
   component gates.
8. Finish Platform automation, Software module UI, configurable distribution assembly, and the support
   matrix.
9. Extend durable diagnostics and wait classification to background controllers, API error detail, Ansible
   failures, and every long-running wait.

## 11. Non-goals

- Replacing Temporal merely because it is an external engine.
- Reintroducing deprecated v2 API aliases or inferred-intent repair loops.
- Moving provider-specific models into core.
- Implementing externally distributed or dynamically loaded modules before their stated customer-driven
  activation conditions.
- Migrating MongoDB to PostgreSQL ahead of higher-priority structure work.
- Adding a dashboard unit-test runner without revisiting the explicit Playwright-only decision.

## 12. Open Questions

- Are power on/off actions or desired power state?
- Is Server Lock Swallow-owned desired hold state, provider-owned observed state, or both?
- What is the contract for `reapply`?
- How are write-only BMC credentials sealed, adopted, compared for drift, and omitted on update?
- Should Markdown field tables remain beside OpenAPI, and if not, what is the migration cadence?
- Should generic GPU charts precede or follow validation?
- Should the Server fleet row-expansion surface remain, move its unique identity/freshness fields to the
  detail page, or be removed?
- When should PostgreSQL store migration, install-time module lifecycle, and externally owned modules
  activate?

## 13. Future Work

- PostgreSQL as the Swallow store after BSON and Mongo semantics are removed from core boundaries.
- Per-installation module enable/disable/update packages when release cadence diverges.
- Signed external modules and out-of-process hooks when modules are independently owned.
- Build-selected distributions and a lab-backed support matrix.
- Provider adapter conformance suites.
- Restore, upgrade, and API-replacement rehearsals on the testing stack before the first formal release.
