# 069. swallow owns desired state; providers converge to it

- Status: Accepted; Refined by [071](071-provisioner-realized-desired-state.md) (spec/status API shape, scheduled auto-correction of non-destructive provisioner-realized fields, adoption at first observation)
- Date: 2026-10-30
- 現行規格：[desired-state-model.md](../development/desired-state-model.md)

## Context

[ADR 001](001-system-ownership-boundaries.md) decided ownership per capability: swallow owns
intent, policy, and identity mapping, and integrates a system that already owns everything else,
mirroring its facts. As provisioning, Workflows, and Platform deployment deepened, that rule left
the same question open in several places — which side is the truth — and each place answered it
with extra machinery:

- **Execution state has two sources of truth.** [ADR 016](016-temporal-operation-orchestration.md)
  made Temporal hold execution history and MongoDB a query projection. In practice MongoDB is also
  where a Workflow starts (written as `startState=pending` and picked up by a Starter loop), the
  Ansible work queue, and the lease store. Keeping both sides consistent takes a Starter and a
  lost-execution reconciler in two processes, a deployment projection repair sweep, and three
  separate mechanisms so that cancelling never rolls back an OS deployment that already succeeded.
- **Truth depends on the configured provider.** [ADR 031](031-provider-capability-first-with-swallow-owned-fallback.md)
  made a fact owned by the provisioner when it is capable and by swallow when it is not. Every such
  feature carries two paths, one of them inert while MAAS is the only provisioner, and a reader has
  to know which provider a Site uses to know which side is authoritative.
- **Intent is not recorded where a provider is capable.** On the capable path swallow writes to the
  provider and mirrors the result back, keeping no copy of what the operator asked for. A change made
  directly in the provider (for example in the MAAS UI) silently becomes the new value; swallow
  cannot report it as drift.
- **No transactions.** MongoDB runs standalone in every environment. Cross-document invariants, such
  as a Platform and the Platform Claims on its Servers, rely on hand-written compensation and a
  repair pass at API start.

The product direction decided alongside this ADR ([ADR 070](070-core-providers-and-extensions.md))
is a generic core whose external systems are replaceable adapters. That only works if the core,
not any one adapter, holds the truth.

## Decision

1. **swallow's store is the single source of truth for desired state and execution state.** The
   desired state (spec) of every managed resource, and every Workflow's state, is owned by swallow.
   Providers realize the desired state. What swallow reads back from a provider is observed state
   (status), carrying its source and observation time. A provider never writes desired state.
2. **What swallow never owns is unchanged.** Metrics, alert evaluation and firing state, workload
   scheduling, and resources inside a managed Platform (Kubernetes objects, Slurm jobs) stay with
   their systems, as in ADR 001.
3. **Convergence is hybrid.**
   - Non-destructive differences between desired and observed state are converged continuously by
     controllers.
   - Destructive changes — reinstalling an OS, releasing a Server, uninstalling a Platform, changing
     a node's role — first produce a Change Plan preview. An operator approves it, it is checked
     again against current state, and it runs as a Workflow.
   - Drift (observed state changed outside swallow) is reported, not corrected, unless a field
     explicitly opts into auto-correction.
   - Desired state is written through the API, which the dashboard and CLI use. GitOps may later be
     added as an import/export channel, not as a second write path.
4. **Workflow semantics belong to swallow; the executor only executes.** swallow defines the
   Workflow model (Task DAG, attempts, retry, cancellation, waiting for an operator) and stores its
   state. Temporal executes it. If an execution's history is lost, swallow rebuilds or re-dispatches
   it from its own store.
5. **Transactions and change notification are platform requirements.** MongoDB runs as a
   single-node replica set, and every process checks this at startup. A cross-document invariant is
   kept inside one transaction, never by compensation or a repair sweep. Store rules are stated as
   ports (a transaction port and a change feed port); database-specific types and semantics stay
   inside infrastructure adapters.

## Alternatives considered

- **Keep the current ownership rules (ADR 001 with ADR 031).** Rejected: every feature keeps two
  paths and its repair loops, and who owns a fact keeps varying with the provider.
- **Pure continuous reconciliation, correcting all drift automatically.** Rejected: in a physical
  data center an automatic correction can reinstall a machine or power off a node that an operator
  changed on purpose.
- **Pure plan/apply for every change.** Rejected: nothing heals itself, and even a tag change would
  need a plan and an approval.
- **GitOps as the write path now.** Rejected for now: two write sources (API and git), secrets in a
  repository, and offline sites. It stays possible as an import/export channel.
- **Make Temporal the single source of execution state.** Rejected: what an operator can see would
  depend on Temporal's history retention and on restoring Temporal together with swallow's store, and
  the Workflow semantics would be tied to one executor.
- **Replace Temporal with swallow's own durable execution.** Rejected: ADR 016 already declined to
  rebuild durable timers, replay, and fencing. Temporal stays, limited to executing.
- **Move the store to PostgreSQL now.** Deferred, not rejected. It would let swallow share one
  database engine with Temporal and MAAS, but it rewrites 33 source files and about 30 collections
  for no change in behavior. Stating store rules as ports keeps the move open; it needs its own ADR.

## Consequences

- Every managed resource separates spec from status and reports conditions; every new field states
  whether it is desired, observed, or derived.
- ADR 031 no longer applies. A provider that cannot realize a desired field reports it as not
  realized instead of swallow building a same-shaped fallback, and the read-time overlays of
  [ADR 025](025-provider-data-overlay.md) retire as their facts become desired fields.
- ADR 017 rejected a plan/apply phase in favor of idempotent "ensure" Tasks. Ensure Tasks remain;
  Change Plans are added for destructive changes only.
- The repair loops — projection repair, the cancel-invariant sweeps, Platform Claim reconciliation —
  become removable as features move to the model. Until a feature is migrated it keeps its current
  behavior; the migration is tracked outside the ADRs.
- Every installation, and the development and testing stacks, must run MongoDB as a replica set,
  which needs an upgrade path for existing installations.
- Cost: a controller framework, a Change Plan model, and a staged migration of existing features.
