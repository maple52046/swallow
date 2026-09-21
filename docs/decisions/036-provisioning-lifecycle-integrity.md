# 036. Provisioning lifecycle integrity: allocated recovery, verification borrow, honest projection

- Status: Accepted
- Date: 2026-09-21

## Context

Three lifecycle holes surfaced together in the lab and left real Servers
stranded, so they are recorded as one decision because they share a root cause:
Swallow treated a provider lifecycle state or a stale projection as more
authoritative than it is.

- **`allocated` was an un-actionable dead end.** MAAS parks a Machine in
  `ALLOCATED`/`RESERVED` (normalized to `allocated`) when a deployment was
  reserved but never completed — a failed or canceled deploy, or a verification
  borrow that ended before install. The provider allows Release from it, but the
  Swallow-owned recovery policy ([ADR 033](033-provider-recovery-policy.md))
  omitted it, so Recover and Release both refused and the operator had no verb to
  return the Machine to `ready`.
- **`verify-os-image` had no failure compensation.** The Workflow is a strict
  `provision-os` -> `record-image-verification` -> `release-os` dependency chain,
  and a failed proving deploy (notably an SSH-unreachable install, marked
  retryable) parked the Operation at `requires_attention`. Because that is not a
  terminal status, the auto-`release-os` never ran, the borrowed Server was left
  occupied (and busy-locked), and the OS Images list — which treats any active
  `verify-os-image` as "verifying" — spun forever. [ADR 035](035-os-image-verification-and-deploy-target.md)'s
  auto-release only covered the success path.
- **The list badge presented a stale deployment result as the truth.** A Server
  observed back at `allocated` still carried an old `deployment.succeeded`
  record, and the merged Deployment badge rendered it as green "Deployed" while
  the provider said otherwise, so an operator saw "Deployed" on a Machine that
  could not be Released. `clearStaleDeployment` only cleared a finished
  deployment record when the provider was back at `ready`, never at `allocated`.

## Decision

- **`allocated` is a Recover/Release source.** The recovery matrix, the dashboard
  action gate, and the Recover primitive plan all treat `allocated` like
  `failed`/`deployed` for the purpose of returning to `ready` (the primitive is
  Release). This refines ADR 033's allowed-source set; it does not change how any
  other state is handled.
- **Verification is a borrow-and-return contract, not a park-on-failure.** A
  `verify-os-image` Workflow always returns the borrowed Server toward `ready`
  whether the proving deploy succeeds, fails, or is canceled. Mechanically: a
  success-record step runs only on provision success, a mirror failure-record step
  runs only on provision failure, and the return-to-ready step runs after whichever
  record step ran (the other is skipped). Under a verification run the proving
  deploy's install/SSH failure is treated as a **non-retryable** proof failure so
  the Workflow reaches a terminal state and its return-to-ready runs, rather than
  parking at `requires_attention`; a genuinely transient provider-unavailable error
  (which never reached the Machine) stays retryable.
- **A failed verification is a durable, visible fact.** The failure-record step
  writes a swallow-owned per-Deploy-Target failure (mirroring the success
  attestation and mutually exclusive with it), projected as `failedDeployTargets`,
  so the OS Images list shows "proven not to deploy this way" distinctly from "never
  tried". Without it a failed verification reverted to the same "not verified"
  display as a never-run one, so an operator could not tell a run had happened. The
  gate still blocks a failed target exactly as it blocks an unverified one; this
  changes display, not deploy admission.
- **Projection stays honest.** The Deployment badge resolves a live provider
  lifecycle state (including `allocated`) before falling back to a stored
  `deployment.succeeded`, so a stale success can never mask the provider truth.
  Reconcile clears a finished (`succeeded`/`failed`/`canceled`) deployment record
  when the provider is back at `ready` **or** `allocated`. The OS Images list
  counts only genuinely in-flight Operations (pending/running/waiting) as
  "verifying"; `requires_attention`, `failed`, and `canceled` are not.
- **General (non-verification) deploy failures are unchanged.** They still leave
  the Machine for operator-initiated Recover/Release per ADR 033; only the
  verification *borrow* auto-returns, because the borrowed spare's contract is to
  be given back.

## Alternatives considered

- **Make every failed deploy auto-release.** Rejected: it contradicts ADR 033's
  operator-initiated recovery for production deploys, where the failed Machine is
  evidence the operator may want to inspect. The borrow contract is specific to
  verification, which chose a spare and promised to return it.
- **Keep parking verification on failure to "preserve the scene".** Rejected: the
  scene lives in the Operation (the provider's reason is recorded on the failed
  Step); holding a spare and a fake "verifying" badge hostage helps no one and
  blocks the Server behind a busy lock.
- **Collapse the three status axes so the badge cannot disagree with itself.**
  Rejected: the axes are owned by different systems ([server-status](../development/glossaries/terms/server-status.md));
  the fix is to read the provider axis first, not to erase it.
- **Add a bespoke `release-on-failure` step kind.** Rejected: `recover-server`
  already converges `allocated`/`failed`/`deployed`/`rescue`/`broken` and a
  ready no-op, so verification reuses it as the return-to-ready step behind a
  general "run this step even when its dependency was skipped" edge.

## Consequences

- Operators regain a verb for a reserved-but-stuck Machine, and a failed
  verification returns its borrowed Server to `ready` on its own while leaving the
  image unverified with the provider's reason on the Operation.
- The orchestration engine gains a small, general capability: a Task may declare
  that it still runs when a dependency was skipped (not only when it succeeded),
  used here so the return-to-ready step follows a skipped finalize. The default is
  unchanged, so every existing Workflow replays identically.
- The Deployment badge and reconcile no longer let a stale success outlive the
  provider truth, removing the "shows Deployed but cannot Release" contradiction.

## Current status

Implemented. The recovery matrix, dashboard gate, verification compensation and
non-retryable proof failure, honest badge/reconcile projection, and the
verifying-status filter are in place, and the lab Servers left stranded by the
original holes were converged back to `ready` through the swallow API.
