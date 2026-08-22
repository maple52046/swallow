# 004 — Automation via AWX

## Decision

**AWX owns every long-running run. swallow owns the intent that started it.**

An `Operation` in swallow is: what an operator wanted, which servers it targets, which AWX job
template executes it, the AWX job ID, and a mirror of that job's status. swallow stores no
logs, implements no retry, and enforces no idempotency — those are Ansible's and AWX's
problem, already solved.

Targeting flows the other way from what would be expected: **swallow does not write
inventory into AWX**. swallow serves a dynamic inventory endpoint that AWX pulls from.

## Context

Every one of the three platform features needs the same thing: run something slow and
failure-prone across many hosts, and be able to say afterwards what happened. Installing
GPU drivers after an OS deployment, deploying Kubernetes, configuring Slurm — all the
same shape.

The first iteration had a stub of this on the frontend: a `ProvisioningJob` with
`progress` and `logs`, and a `Mission`/`Run` model that every action funnelled into.
Nothing implemented it, and the backend has no execution capability at all — no task
queue, no worker, no SSH, no scheduler. The `Run` model was then deprecated, which left
every action button pointing at a concept that no longer existed.

Building it in swallow means owning idempotency, retry, concurrency limits, rollback, log
durability, and secret handling for host access. That is Ansible's and AWX's entire
problem domain. AWX has a REST API with job templates, job status, per-event logs, and
inventory management, and Ansible modules are idempotent by design — which is precisely
the property that makes "run it again" a safe answer to a partial failure.

## What swallow Stores

```
Operation
  id
  kind                 install-gpu-driver | deploy-kubernetes | configure-slurm | ...
  intent               operator-supplied description of why
  targetServerIds[]      resolved at creation time, never re-resolved
  siteId
  clusterId?           when the operation is about a cluster
  awx: {
    jobTemplateId
    jobId              null until AWX accepts it
    status             mirrored from AWX
    startedAt, finishedAt
    observedAt         when the mirror was last refreshed
  }
  requestedBy, requestedAt
```

`targetServerIds` is frozen at creation. An operation that re-resolved its targets from a
live query would silently change scope between request and execution — a server joining a
cluster mid-run would be swept into an operation nobody asked for.

Status is a **mirror**, and it says when it was last observed. swallow never infers that a
job finished because time passed.

## Inventory Direction: Pull, Not Push

AWX pulls from swallow, using swallow's dynamic inventory endpoint as an inventory source.

Pushing — swallow creating and updating AWX hosts and groups through the AWX API — was the
obvious approach and is rejected. It makes swallow responsible for keeping two databases
consistent, which means reconciling AWX inventory against the server projection, handling
partial failures mid-push, and deciding what to do about hosts in AWX that swallow did not
create. That is a second reconciliation loop for no gain.

Pulling means the server projection is the only source of targeting truth. AWX asks at job
launch time and gets the current answer. There is nothing to synchronise and nothing to
drift.

The same projection also serves the Prometheus `http_sd` endpoint
([003](003-metrics-label-contract.md)). One projection, two consumers, both pulling.

Groups exposed to AWX are derived from swallow data that automation needs to branch on:
site, cluster, cluster role, GPU vendor, and provisioning state. Host variables carry
`server_id` so that a playbook can report back in swallow's own identifiers.

## Status and Logs

**Status** is refreshed two ways, because neither alone is sufficient. AWX notification
webhooks give prompt updates but are lossy — a webhook lost while swallow is restarting is
gone. A poller over operations that are not in a terminal state is the backstop. Webhooks
make it feel live; polling makes it correct.

**Logs** are proxied on demand from AWX, never copied. An operation record holds the job
reference; asking swallow for logs makes swallow ask AWX. Copying them would mean swallow owning
log retention for content that AWX already retains, and the copy would be incomplete for
any job still running.

## What swallow Must Still Guard

Delegating execution does not delegate judgement. swallow refuses an operation when:

- **It contradicts policy.** A driver installation targeting servers in a `gpu-operator`
  cluster ([003](003-metrics-label-contract.md)).
- **Its targets overlap an operation already in flight.** Two concurrent reimages of the
  same server is not something AWX will stop, because AWX sees two unrelated jobs.
- **A target is in the wrong provisioning state.** Configuring Slurm on a server that is
  mid-deployment will fail slowly and confusingly; refusing is faster and clearer.

These checks are the reason an `Operation` record exists at all rather than swallow being a
thin button that launches AWX jobs. AWX knows a job ran; only swallow knows whether it
should have.

### Open: operations targeting an ephemerally deployed server

A server whose OS runs from memory
([provisioning glossary](../glossaries/provisioning.md#ephemeral-deployment)) loses its
whole root filesystem on reboot. An operation that installs anything on it will report
success honestly — the playbook did run and did succeed — and then silently un-happen.

That is the same shape as the `gpuStackOwner` conflict: work swallow believes it has done
that the world does not agree with. The difference is only in timing, which arguably makes
it worse, because the operation record stays green.

The `provisioning.ephemeral` fact is now available to the guard, but no check uses it yet.
The question deferred is which of these it should be: refusing such operations outright,
warning and recording the choice, or treating short-lived by design as a legitimate case
that needs no comment. Deciding it needs a real ephemeral fleet to reason about, rather
than a guess about how they will be used.

## Failure Modes

| Situation | Behaviour |
|-----------|-----------|
| AWX unreachable at creation | Operation is not created. No half-state that has to be reconciled later |
| AWX unreachable while mirroring | Status keeps its last value with a stale `observedAt`. Never inferred as failed |
| Job template missing | Rejected at creation with the template name, not at execution |
| Job vanished from AWX | Operation marked indeterminate, not failed. Absence is not an outcome |
| swallow restarts mid-operation | Nothing is lost. State lives in AWX; the poller picks the operation up again |

The pattern is the same throughout: swallow never converts "I do not know" into an outcome.
This mirrors the staleness rule in [002](002-server-identity.md).

## Consequences

- swallow implements an AWX client (launch, status, logs, template lookup), an `Operation`
  context, a dynamic inventory endpoint, and a status poller.
- The poller is swallow's second background loop, alongside the provider reconciler. Both
  are periodic readers of external state, which is the only kind of background work this
  architecture needs.
- Playbook content lives in git and is referenced by AWX job template. swallow stores no
  automation content, which retires the `ProvisioningProfile` concept — packages and
  scripts belong in a role, not in a platform database.
- Which job templates exist becomes part of deployment configuration. swallow maps its
  operation kinds to template identifiers; it does not create templates.
- Deploying Kubernetes and configuring Slurm are operation kinds, not bespoke subsystems.
  Feature 3 is largely a matter of naming the right templates.

## Rejected Alternatives

**swallow runs `ansible-playbook` itself and captures the output.** No external dependency,
full control of the interface, and easy to start. Rejected because everything hard about
running automation is in the parts this skips: durable state across a swallow restart,
concurrency limits, log retention, credential handling for host access, and a place to
look when a run half-succeeded. It reproduces AWX badly, and it reproduces exactly the
gap that made the first iteration unrealistic.

**A CI/CD system — GitLab CI, Jenkins, GitHub Actions — as the run owner.** Genuinely
attractive: playbooks and inventory in git with review and audit for free, and pipeline
APIs expose status and logs much like AWX. Rejected because CI is built around commits,
not around a target set chosen at runtime. Passing "these 12 server IDs" into a pipeline
means encoding the target set in pipeline variables and losing the inventory model
entirely, and CI has no concept of an inventory to pull. It remains the best fallback if
AWX becomes untenable.

**Argo Workflows or Tekton.** Strong workflow engines with good status and log APIs.
Rejected on bootstrapping: they run on Kubernetes, and deploying the first Kubernetes
cluster is one of the operations that needs an engine. A dependency cycle at the exact
moment the platform is most needed.

**Push inventory into AWX from swallow.** Covered above: a second database to keep
consistent, for nothing.

**Copy AWX logs into swallow for a unified API and retention.** One place to search, and
logs survive AWX being rebuilt. Rejected because it duplicates retention that AWX already
provides, cannot be complete for a running job, and makes swallow's storage grow with
automation verbosity — a chatty playbook should not cost swallow disk.

## Related

- [001 — System Ownership Boundaries](001-system-ownership-boundaries.md)
- [002 — Server Identity](002-server-identity.md)
- [003 — Metrics Label Contract](003-metrics-label-contract.md)
