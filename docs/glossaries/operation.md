# Operation

## Definition

An operator's **intent**, executed by swallow itself.

An operation is how anything slow and failure-prone happens across many servers:
installing GPU drivers, deploying Kubernetes, configuring Slurm. swallow records what
someone wanted, and owns the run that carries it out — an `ansible-runner` process
started by an embedded dispatcher against a playbook from the release bundle.

See [decision 006](../decisions/006-embedded-ansible-execution.md). An earlier design
delegated execution to AWX; that model, and its documentation, have been removed.

## Key Fields

- `operationId` — swallow-issued, opaque
- `kind` — `install-gpu-driver`, `deploy-kubernetes`, `configure-slurm`, or `custom`
- `intent` — the operator's own description of why. The run records that work happened;
  only this records what it was for
- `siteId` — every target belongs to it
- `clusterId` — when the operation is about a cluster
- `targetServerIds` — **frozen at creation**
- `execution` — the run swallow owns: its `runId`, the playbook, status, and timings
- `retryOfOperationId` — the operation this one was created to retry, when it was
- `requestedBy`, `requestedAt`

## Why targets are frozen

An operation that re-resolved its targets from a live query would silently change scope
between request and execution. A server joining a cluster mid-run would be swept into work
nobody asked for.

## Execution

Every accepted operation is persisted as `pending` **before** anything runs. A dispatcher
then claims a MongoDB lease and starts the run. Acceptance means the intent is durable, not
that execution has begun.

Values: `pending | running | succeeded | failed | canceled | error | indeterminate`

`indeterminate` means the API or host stopped while a run held a lease, so the run's
outcome cannot be proven. It is terminal, but deliberately **not** `failed` — absence of
evidence is not an outcome. The same principle appears in
[Server](server.md#unknown-is-not-a-status-value): "we do not know" must never be
presentable as "we know it went badly".

One operation per site runs at a time, enforced by a site lease. Operations at different
sites run concurrently.

Run output is retained locally as job artifacts, and read back through the operation
rather than proxied from anywhere. Credentials are materialised only for the duration of a
run, in a private directory, and never appear in output.

## Progress

Beyond a status, a run emits per-task and per-host events. A multi-phase operation such as
an HA cluster deployment runs for several minutes across bootstrap, join, and verification
phases, and a single status word cannot say which phase is running or which host is
failing. The events are derived from the runner's own record of the run; swallow adds no
progress estimate of its own.

## Retry

A failed operation is **never retried automatically**. An operator may ask for a retry,
which creates a **new** operation with the same kind, targets, and variables, carrying
`retryOfOperationId` back to the original. The original's record and artifacts are kept:
the history of what was attempted is part of what an operation is for.

Rerunning is safe because playbooks are written to be idempotent — a host that already
reached the desired state is left alone. That property belongs to the playbook, not to
swallow, and a playbook that lacks it must not be mapped to a retryable kind.

## Targeting

The runner takes its inventory from swallow's own projection of servers. Hosts are keyed by
`serverId`, so an operation's target list needs no translation and a playbook reports back
in swallow's own identifiers. The same projection is published for external tools and
diagnostics.

Pushing inventory into a separate targeting database was rejected: it would make swallow
responsible for keeping two databases consistent, which is a second reconciliation loop for
no gain.

## What swallow refuses

Owning execution does not mean running whatever is asked. An operation is refused when:

- **It contradicts a cluster's policy** — installing GPU drivers where the
  [GPU operator owns them](cluster.md#gpu-stack-owner).
- **A target is already in an unfinished operation.** Two runs against one host would
  interleave.
- **A target is in the wrong provisioning state.** Post-install automation against a
  machine mid-deployment fails slowly and confusingly.
- **Targets span more than one site.** A run executes under one site's automation
  configuration and one site lease.
- **The playbook is not in the release manifest.** Reported at creation, not by a run that
  never starts.
- **The site's automation is disabled or has no credential.** There is nothing to execute
  with.

These checks are the reason an operation record exists at all, rather than swallow being a
button that starts playbooks. **A run knows work happened; only swallow knows whether it
should have.**

## Automation content

Playbooks and roles ship in the release bundle and are referenced by manifest name. swallow
stores none of their content, and a path outside the manifest is refused — configuration
must not be able to select unreviewed code.

This is why there is no provisioning profile: an image plus packages plus scripts, stored
in swallow, would make it an owner of automation content, which
[decision 001](../decisions/001-system-ownership-boundaries.md) forbids.

Which playbook implements which kind is a property of the installation, configured as part
of a site's automation configuration.

## Relationships

- An operation targets one or more [Servers](server.md), all at one [Site](site.md).
- An operation may concern a [Cluster](cluster.md), including the operation that builds
  one.
- An operation runs under one site's automation configuration.
- An operation may be the retry of one earlier operation.

## Out of Scope

- **Idempotency and rollback.** Properties of the playbook.
- **Automatic retry.** Explicitly refused; see above.
- **Scheduling.** An operation is dispatched as soon as its site is free.
- **A deployment's progress.** An OS deployment is the provisioner's work, tracked by the
  [provisioning axis](server.md#the-three-status-axes), not by an operation.

## Related Concepts

- [Server](server.md) — what operations target.
- [Cluster](cluster.md) — what several operation kinds are about, and what one of them
  builds.
- [decision 006](../decisions/006-embedded-ansible-execution.md) — why swallow owns
  execution rather than delegating it.
- [decision 007](../decisions/007-cluster-deployment-ownership.md) — how the cluster
  deployment operation differs from the others.
