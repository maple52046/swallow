# Operation

## Definition

An operator's **intent**, executed by an external automation controller.

An operation is how anything slow and failure-prone happens across many servers:
installing GPU drivers, deploying Kubernetes, configuring Slurm. swallow records what someone
wanted and a reference to the job doing it. It stores no logs, implements no retry, and
enforces no idempotency — those belong to Ansible and AWX, which solved them.

See [decision 004](../decisions/004-automation-via-awx.md).

## Key Fields

- `operationId` — swallow-issued, opaque
- `kind` — `install-gpu-driver`, `deploy-kubernetes`, `configure-slurm`, or `custom`
- `intent` — the operator's own description of why. The controller records that a job ran;
  only this records what it was for
- `siteId` — every target belongs to it
- `clusterId` — when the operation is about a cluster
- `targetServerIds` — **frozen at creation**
- `automation` — the reference to, and mirror of, the controller's job
- `requestedBy`, `requestedAt`

## Why targets are frozen

An operation that re-resolved its targets from a live query would silently change scope
between request and execution. A server joining a cluster mid-run would be swept into work
nobody asked for.

## The automation mirror

`automation.status` is a **mirror** of the controller's job, and carries `observedAt`
saying when it was last read. swallow never infers that a job finished because time passed.

Values: `pending | running | succeeded | failed | canceled | error | indeterminate`

Every pre-run controller state collapses into `pending`: the difference between queued and
waiting for a capacity slot is the controller's business.

`indeterminate` means the controller no longer has the job. It is terminal, but it is
deliberately **not** `failed` — absence is not an outcome. The same principle appears in
[Server](server.md#unknown-is-not-a-status-value): "we do not know" must never be
presentable as "we know it went badly".

Status is kept current two ways, because neither alone is sufficient. Controller webhooks
are prompt but lossy — one lost while swallow restarts is gone — so a poller over
non-terminal operations is the backstop. Webhooks make it feel live; polling makes it
correct.

A webhook is treated as a **hint to go and look**, not as a source of truth: the status
swallow records always comes from a read it made itself.

## Targeting

The automation controller pulls its inventory from swallow rather than being pushed into.
Hosts are keyed by `serverId`, so an operation's target list needs no translation and a
playbook reports back in swallow's own identifiers.

Pushing inventory the other way was rejected: it would make swallow responsible for keeping
two databases consistent, which is a second reconciliation loop for no gain.

## What swallow refuses

Delegating execution does not delegate judgement. An operation is refused when:

- **It contradicts a cluster's policy** — installing GPU drivers where the
  [GPU operator owns them](cluster.md#gpu-stack-owner).
- **A target is already in an unfinished operation.** The controller will not stop this,
  because it sees two unrelated jobs.
- **A target is in the wrong provisioning state.** Post-install automation against a
  machine mid-deployment fails slowly and confusingly.
- **Targets span more than one site.** An operation runs through one site's controller.
- **The job template does not exist.** Reported at creation, not by a job that never starts.

These checks are the reason an operation record exists at all, rather than swallow being a
button that launches jobs. **The controller knows a job ran; only swallow knows whether it
should have.**

If the controller cannot be reached at creation, nothing is created. A half-state to
reconcile later is worse than a failed request an operator can retry.

## Automation content

Playbooks, roles, and scripts live in git and are referenced by job template name. swallow
stores none of it.

This is why there is no provisioning profile: an image plus packages plus scripts, stored
in swallow, would make it an owner of automation content, which
[decision 001](../decisions/001-system-ownership-boundaries.md) forbids.

Which template implements which kind is a property of the installation, configured as a
setting on the automation [Integration](site.md#integration).

## Relationships

- An operation targets one or more [Servers](server.md), all at one [Site](site.md).
- An operation may concern a [Cluster](cluster.md).
- An operation executes through one automation [Integration](site.md#integration).

## Out of Scope

- **Logs.** Proxied from the controller on demand, never stored.
- **Retry, idempotency, concurrency limits, rollback.** Ansible and AWX.
- **Scheduling.** An operation runs when it is created.
- **A deployment's progress.** An OS deployment is the provisioner's work, tracked by the
  [provisioning axis](server.md#the-three-status-axes), not by an operation.

## Related Concepts

- [Server](server.md) — what operations target.
- [Cluster](cluster.md) — what several operation kinds are about.
- [decision 004](../decisions/004-automation-via-awx.md) — the full reasoning, including
  why AWX rather than CI or a home-grown runner.
