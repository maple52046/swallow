# 033. Swallow-owned provider recovery policy

- Status: Accepted (allowed sources refined by [ADR 036](036-provisioning-lifecycle-integrity.md); the retry path refined by [ADR 059](059-retry-recovers-failed-deployment.md))
- Date: 2026-09-20
- 現行規格：[os-provisioning.md](../development/os-provisioning.md) §11 Provider Recovery

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

- **Release** is allowed from `deployed`, `allocated`, `failed`, `broken`, and
  `rescue`, and converges the Machine to `ready`. This relaxes the former
  `deployed`-only rule to match what the provider already supports.
- **Recover** ("Return to Ready") is a Swallow-owned durable Operation that
  chooses the primitive by state: `broken` -> `mark_fixed`; `rescue` ->
  `exit_rescue_mode`, then Release if the Machine is still not `ready`; `failed`,
  `allocated`, or `deployed` -> Release. Its success condition is observing
  `ready`. A Machine that will not leave rescue is escalated to `mark_broken` and
  then `mark_fixed`, the MAAS-idiomatic way to unstick a rescue without a disk
  erase; the rescue-transition states (including their failed variants) normalize
  to `rescue` so the policy and UI treat them as one rescue problem.
- **Mark fixed / Mark broken / Rescue enter / Rescue exit** stay available as
  advanced primitives, but Swallow gates them on live state before calling the
  provider and returns a Swallow-authored reason on refusal, never only the raw
  provider message.
- **Rescue** is a diagnostic environment, not a recovery path. It is allowed from
  `deployed`, `broken`, and `failed`; exiting restores the previous provider
  state and does not by itself reach `ready`.
- The allowed-source matrix and the Recover primitive plan live in one domain
  policy, reused by durable Release, the Recover Operation, and the single-action
  operator-state use cases so they never drift.
- Recovery is operator-initiated.

Revision history: on 2026-09-21 [ADR 036](036-provisioning-lifecycle-integrity.md)
added `allocated` as a Release and Recover source. On 2026-10-19
[ADR 059](059-retry-recovers-failed-deployment.md) replaced the documented
"Recover the node, then retry the Task" path, which could not run while the
deploying Operation held the Server: an explicit retry of a failed `provision-os`
now runs the Recover plan itself before redeploying (original text in git
history).

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
  the platform failure guidance all reference one source of truth.
- New provider adapters must map their own states onto the normalized set the
  policy branches on; they do not re-implement the policy.
