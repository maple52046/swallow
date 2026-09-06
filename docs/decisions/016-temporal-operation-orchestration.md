# 016. Operation v3 is a Temporal-orchestrated multi-step workflow

- Status: Accepted; vocabulary and model refined by [ADR 017](017-workflow-job-task-runner-model.md)
- Date: 2026-09-05

> **Refined by [ADR 017](017-workflow-job-task-runner-model.md):** the domain vocabulary here
> (Operation / Step / executor) is renamed to Workflow / Task / Runner, a reusable **Job** layer is
> added, and execution is made convergent ("ensure" Tasks). The Temporal-engine decision in this
> ADR is unchanged.

## Context

[ADR 006](006-embedded-ansible-execution.md) made swallow own Ansible execution through an
embedded dispatcher that claimed a Mongo **site lease** (one run per site) and ran a single
manifest-listed playbook. It explicitly **deferred** a separate worker service, expecting to
add one only when HA arrived.

The Operation model then outgrew a single run. Real work — deploying an HA Kubernetes
Platform, reimaging a rack and returning it to service — is multi-phase and spans more than
one owner: provider actions (MAAS), host automation (Ansible), and swallow-internal steps
(membership, credential capture). Coordinating that durably requires step-level state,
dependencies, per-step retry, cancellation mid-run, resumability across process restarts,
and serialization per *resource* rather than per *site*. Building those on the embedded
dispatcher would mean hand-writing durable timers, signals, replay, and multi-resource
fencing — which is the "do not rebuild what a mature system already owns" line from
[ADR 001](001-system-ownership-boundaries.md).

The implementation adopted Temporal for this. This ADR records that decision and reconciles
it with ADR 006, which previously read as if the embedded dispatcher were the final answer.

## Decision

**Operation v3 is a durable multi-step workflow orchestrated by Temporal.**

- **Deterministic coordinator, I/O in activities.** `OperationWorkflowV1` decides ordering
  only; every side effect (lease acquire/renew/release, state and step projection writes,
  step execution) is a Temporal activity. The workflow never touches Mongo. Mongo holds the
  operator-facing query projection (`OperationV3`, its Steps, and the timeline); Temporal
  holds execution history.
- **Steps form a DAG.** Each `OperationStep` is carried out by exactly one typed executor —
  `internal`, `ansible`, or `maas`. The ADR 006 `ansible-runner` mechanism survives as the
  `ansible` step executor rather than as the whole engine. Ready steps run in bounded
  parallel batches (`OperationMaxParallelism`).
- **Serialization is per resource, with fencing.** A `ResourceLease` per Server or Platform,
  carrying a fencing token, serializes all mutations on that resource. This **replaces** the
  ADR 006 one-run-per-site lease as the mutation-serialization mechanism; an activity whose
  fencing token is stale fails non-retryably rather than mutating.
- **Secrets never enter workflow history.** An Operation carries opaque secret references;
  values live in an encrypted store and are resolved only for a run.
- **No automatic retry.** Execute activities are `MaxAttempts: 1`. A retryable failure pauses
  the Operation as `requires_attention` until an operator retries a specific Step, which
  increments that Step's attempt. Cross-operation retry still creates a new Operation linked
  by `retryOfOperationId`.
- **Idempotent start; permanent v2 read compatibility.** A stable Workflow ID plus
  reject-duplicate makes starting safe from both the API and worker processes. Historical v2
  single-run operations remain readable through a permanent compatibility projection.

### What ADR 006 keeps

These parts of ADR 006 are **not** superseded and remain in force: the release owns the
playbook manifest and dependency lock; arbitrary filesystem paths are rejected; site
credentials are encrypted and materialised only for a run under a private directory; SSH
host-key verification is mandatory; and `indeterminate` — "the outcome cannot be proven" — is
never reported as success or auto-retried.

### What ADR 006 loses

Superseded by this ADR: the embedded dispatcher as the execution engine, the Mongo
**site lease** as the serialization unit, and the "separate worker service: deferred" stance.

## Alternatives considered

- **Grow the embedded dispatcher into a DAG engine.** Rejected: it reimplements durable
  timers, signals, replay, and per-resource fencing that Temporal already provides — the
  exact reimplementation [ADR 001](001-system-ownership-boundaries.md) warns against.
- **A hand-rolled saga / job table in Mongo.** Rejected: the correctness surface (crash
  recovery, exactly-once side effects, cancellation) is large and easy to get subtly wrong.
- **Stay single-run per site (v2).** Rejected: multi-phase Platform deploys and mixed
  provider + Ansible + internal steps need step-level state, dependencies, and per-resource
  serialization that a single run cannot express.

## Reconciliation with ADR 006

ADR 006 rejected AWX partly to avoid "an orchestration platform larger than the control
plane it serves" and to preserve single-host and air-gapped installation. Adopting Temporal
deliberately re-opens that trade-off: the durability, replay, signalling, cancellation, and
fencing it provides are worth an added infrastructure component, whereas AWX also brought a
Kubernetes-operator production topology swallow could not accept. Temporal is self-hostable and
runs as a bundled service in the same Compose/native stack, so the single-host and air-gap
constraints from ADR 006 are preserved as **deployment requirements**: a release must bundle
Temporal (and its datastore) for offline installation.

## Consequences

- **Temporal is now a required runtime dependency** of the swallow stack: a Temporal server
  (namespace + task queue) and a worker process (`RunWorker`). Deployments must run it, and
  air-gapped releases must bundle it.
- The [Operation](../development/glossaries/terms/operation.md) glossary term and the
  `api-server` operations API contract describe the v3 multi-step model; v2 remains a
  permanent read-compatibility projection.
- Operators get durable, observable, resumable multi-step operations with per-step retry and
  mid-run cancellation.
- The cost is exactly the concern ADR 006 raised — one more infrastructure component and its
  operational surface — accepted here for the correctness it buys.

## Current status

Implemented: `OperationWorkflowV1`, the Temporal worker and start reconciler, per-resource
lease fencing, the `internal` / `ansible` / `maas` step executors, and the v2 compatibility
projection.
