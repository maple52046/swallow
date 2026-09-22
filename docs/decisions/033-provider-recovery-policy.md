# 033. Swallow-owned provider recovery policy

- Status: Accepted
- Date: 2026-09-20

## Context

A Server whose provisioner reports `failed` (for example MAAS `FAILED_DEPLOYMENT`
or `FAILED_TESTING`), `broken`, or `rescue` had no coherent recovery path in
Swallow. The three commands an operator reached for each dead-ended:

- Mark fixed returned `MAAS refused the request: Can't mark a non-broken node as
  'Ready'`, because MAAS `mark_fixed` only accepts a genuinely Broken node while
  the dashboard collapsed `failed` and `broken` into one red "Failed" badge.
- Durable Release returned `Server ... is not deployed`, because
  `LaunchRelease` hardcoded a `deployed`-only precondition even though MAAS
  allows Release from a failed deployment.
- Rescue mode was treated as a recovery button, but exiting rescue returns the
  Machine to whatever state it had before, so a `failed` Machine looked "stuck".

The root cause was that Swallow was a near passthrough: it forwarded operator
intent to the provisioner and surfaced the provider's own rejection text as the
only feedback, instead of owning a policy for what recovery means and which
provider primitive realizes it.

## Decision

Swallow owns a single **provider recovery policy**. The policy, not the
provisioner's rejection text, decides which recovery intent is allowed from each
normalized `provisioning.state`, and which provider primitive sequence realizes
it. The provisioner executes the chosen primitive; it is no longer the first
gate an operator meets.

- **Release** is allowed from `deployed`, `failed`, `broken`, and `rescue`, and
  converges the Machine to `ready`. This relaxes the former `deployed`-only rule
  to match what the provider already supports.
- **Recover** ("Return to Ready") is a new Swallow-owned durable Operation that
  chooses the primitive by state: `broken` -> `mark_fixed`; `rescue` ->
  `exit_rescue_mode`, then Release if the Machine is still not `ready`; `failed`
  -> Release. Its success condition is observing `ready`. A Machine that will not
  leave rescue (the provider keeps reporting a failed exit, or hangs in the
  transition past a grace period) is escalated to `mark_broken` and then
  `mark_fixed`, the MAAS-idiomatic way to unstick a rescue without a disk erase;
  the rescue-transition states (including their failed variants) normalize to
  `rescue` so the policy and UI treat them as one rescue problem.
- **Mark fixed / Mark broken / Rescue enter / Rescue exit** stay available as
  advanced primitives, but Swallow gates them on live state before calling the
  provider and returns a Swallow-authored reason on refusal, never only the raw
  provider message.
- **Rescue** is a diagnostic environment, not a recovery path. It is allowed from
  `deployed`, `broken`, and `failed`; exiting restores the previous provider
  state and does not by itself reach `ready`.

The allowed-source matrix and the Recover primitive plan live in one domain
policy (`provisioning/domain/recovery_policy.go`), reused by durable Release,
the Recover Operation, and the single-action operator-state use cases so the
three never drift.

## Alternatives considered

- **Keep passthrough, only widen the dashboard gating.** Rejected: it leaves the
  contradictory backend preconditions (`deployed`-only Release, MAAS-only Mark
  fixed) in place, so the UI still lies about what will succeed and the provider
  message remains the real contract.
- **Make Release the only recovery verb.** Rejected: a `broken` Machine is best
  returned with `mark_fixed` (no disk erase, no re-commission), and a `rescue`
  Machine must exit rescue first; collapsing everything into Release would wipe or
  churn Machines unnecessarily. Recover encapsulates the state-specific sequence.
- **Auto-recover failed nodes after a failed deployment.** Rejected for this
  round: automatic destructive recovery contradicts ADR 016/007 (operator-triggered
  recovery). Recover and the relaxed Release remain operator-initiated.

## Consequences

- Operators get two clear "make it usable again" verbs (Recover, Release) whose
  preconditions match reality, plus advanced primitives that refuse with a
  Swallow reason instead of a provider error.
- The recovery matrix is centralized, so the durable Release, the Recover
  Operation, the operator-state actions, the dashboard availability gating, and
  the platform failure playbook all reference one source of truth.
- A platform deploy that leaves a node `failed` now has a documented path: Recover
  or Release the node to `ready`, then retry the Task or rerun the Workflow.
- New provider adapters must map their own states onto the normalized set the
  policy branches on; they do not re-implement the policy.

## Current status

Implemented: domain policy, relaxed durable Release, Recover Operation and its
provider Step, operator-state gating, dashboard intents and gating, and the
contract/glossary/platform-deployment documentation.

Refined by [decision 036](036-provisioning-lifecycle-integrity.md): the
allowed-source set adds `allocated` (reserved but not deployed), so Recover and
Release converge it to `ready` instead of dead-ending; the recovery matrix and
dashboard gate are otherwise unchanged.
