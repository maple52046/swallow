# 026. Slurm deployment uses one optional system-wide minimum resource policy

- Status: Accepted
- Date: 2026-09-13

## Context

Ephemeral Slurm deployment expands an OS image into a memory-backed root filesystem. The
current lab image expands beyond the practical capacity of the existing 16 GiB VMs, but the
deployment wizard has allowed those Servers to be selected and the backend has accepted them.
UI-only filtering would still allow API callers or stale browser sessions to submit an unsafe
selection.

The resource floor is operational policy rather than per-deployment intent: administrators need
one current eligibility rule that every Slurm deployment observes. CPU, memory, and storage are
all relevant hardware facts, and using different floors for controller, compute, and login roles
would add role-specific policy before there is evidence that those distinctions are stable.

## Decision

Swallow owns one optional, system-wide Slurm **Minimum Resource Requirement**. It contains CPU
cores, memory in MiB, and storage in GB. When enabled, all three values are positive and every
Server assigned as a Slurm controller, compute node, or login node must meet or exceed every
dimension. Equality passes; an unknown or zero observed value fails a positive threshold.

The Platform Management context stores the current policy using the Platform type as the unique
identity. No record means disabled, so no migration is required. The canonical Platforms API is
the only management surface. The Dashboard shows ineligible Servers and their shortfalls but
disables all Slurm role controls. The deploy use case reads the policy once during Slurm
preflight and repeats the authoritative validation before it creates a Platform or Workflow.
Failure to read the policy fails closed. Kubernetes does not read or apply the Slurm policy.

The policy is an eligibility floor only. It neither reserves resources nor changes Slurm
scheduler capacity, and it is not automatically inferred from an image. Passing it therefore
does not promise that an arbitrary image or deployment will fit.

## Alternatives considered

- **Put the requirement in each deployment request.** Rejected because callers could omit or
  weaken a system safety policy, and repeated values would make audit and UI behavior ambiguous.
- **Define separate floors for controller, compute, and login roles.** Deferred because the
  first use case needs one deployment-safety floor and there is no validated role-specific sizing
  model yet.
- **Filter only in the Dashboard.** Rejected because direct API callers and stale sessions could
  bypass it.
- **Infer memory from the selected OS image.** Rejected because provider image metadata does not
  reliably express expanded runtime footprint, overlay size, or workload headroom.

## Consequences

- Administrators can enable, update, or disable one Slurm policy without changing deployment
  request bodies.
- Dashboard eligibility and backend enforcement share the same dimensions and comparison rule,
  while the backend remains authoritative under concurrent policy changes.
- Inventory facts that are absent or zero become explicitly ineligible while the corresponding
  positive floor is active.
- The first policy is intentionally coarse; a future role-aware or image-aware model requires a
  new decision and API evolution rather than overloading these values.

## Current status

Implemented for Slurm across the Platform Management domain, Mongo repository, canonical API,
deployment preflight, settings page, and Slurm node-selection UI. The lab policy is configured at
4 CPU cores, 24576 MiB memory, and 80 GB storage for the ephemeral HA acceptance deployment.
