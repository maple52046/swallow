# 021. Platform-type-specific management views and a Slurm-native cluster read

- Status: Accepted
- Date: 2026-09-09
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §6.4 讀取與管理視圖（type-specific，ADR 021）

## Context

swallow manages more than one kind of Platform (Kubernetes/k0s and Slurm). The dashboard
platform detail page grew up around Kubernetes and modelled every platform the same k0s way:
one summary and one member table with `control-plane`/`worker` roles and a single `State`
column. Slurm is structurally different:

- `slurmctld` controllers are not scheduler nodes, so slurmrestd never reports them as members;
  they were missing or synthesised from the deployment intent.
- A compute node's `role` is really its Slurm partition, and its `state` is a Slurm scheduler
  state (`idle`/`allocated`/`mixed`/`down`/`drain`), not a Kubernetes `Ready` condition.
- The `State` column had come to conflate three different axes: the platform's scheduler /
  membership state, the host power state, and the monitoring health axis. A running Slurm
  controller with no exporter read as "down" (health), which is misleading.

Monitoring is not fully integrated yet, so health cannot be shown accurately, and forcing
Slurm through the Kubernetes shape produced confusing, incorrect information.

## Decision

- **Platforms share a shell but own type-specific views.** The platform detail page is a shared
  shell (header, lifecycle notice, sync, lifecycle actions, related operations) that renders a
  per-type body. Shared behaviour is composed from shared helpers; different platform types may
  have similar functions, but neither inherits the other's vocabulary or state model.
- **Add a Slurm-native cluster read, on demand and read-only.** A new
  `GET /api/v1/platforms/{id}/slurm` reads live controllers, partitions, and compute node
  scheduler state from slurmrestd. It performs no writes: it does not touch the membership axis or
  the sync counters, and it never runs on the reconcile interval.
- **Keep the three data sources separate.** Deployment intent (operation history) says what was
  requested; membership sync writes the `Server.membership` axis (compute only for Slurm); the live
  read describes the running cluster. The dashboard degrades from the live read to
  intent-plus-membership when slurmrestd is not recorded or unreachable.
- **Health is a distinct axis, temporarily not shown.** The member/node `State` column carries
  only scheduler state and controller RPC status; it no longer borrows host power or monitoring
  health. Health is to be surfaced in its own column once monitoring is integrated.

## Alternatives considered

- **Keep one unified k0s-shaped view with conditionals:** rejected — it forced Slurm into
  Kubernetes vocabulary, hid controllers, mislabelled partitions as roles, and conflated axes.
  Type-specific views remove the conditionals from the leaf components and let each type evolve.
- **Fully separate detail routes per type:** rejected — the shell (lifecycle, sync, delete,
  repair, related operations) is genuinely shared, and one route with a type-switched body keeps
  that shared surface DRY.
- **Surface Slurm controllers/partitions through the existing membership axis:** rejected —
  slurmrestd does not report controllers as members, and forcing the membership sync to invent
  them would fight the sync (which clears anything slurmrestd does not report) and mix intent
  with observation. A dedicated on-demand read keeps the axes clean.
- **Show host power or monitoring health as the node/controller State now:** rejected — that is
  the conflation this ADR removes. Controller liveness comes from slurmrestd ping; node state is
  the Slurm scheduler state; health waits for real monitoring integration.

## Consequences

- The Slurm view shows real controllers (primary/backup and ping status), partitions, and
  per-node Slurm state with CPU/memory/GRES, aligned with the Slurm validation knowledge.
- The membership sync, the deployment intent projection, and the member list are unchanged; the
  live read is additive.
- Without slurmrestd, the Slurm view degrades to the recorded controller intent plus the
  last-synced compute membership, without a false-alarm "down".
- Health is intentionally missing from the view until monitoring is integrated; it must be added
  as its own axis, not folded back into scheduler state.
