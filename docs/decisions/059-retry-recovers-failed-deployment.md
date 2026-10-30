# 059. Retrying a failed OS deployment recovers the Server before redeploying

- Status: Accepted
- Date: 2026-10-19
- 現行規格：[os-provisioning.md](../development/os-provisioning.md) §11 Provider Recovery（§11.3 重試失敗的部署）

## Context

On 2026-10-19 a platform deployment's `ensure-os` Job failed on `lab-afde-mi308-3`: MAAS reported
*Failed deployment*, so the Machine sat at `failed` and the `provision-os` Task ended `failed` with
a retryable `deployment_failed`. The operator retried the Task three times. Each retry was refused
in under a second by Swallow's deployment preflight, which accepts only `ready` targets, and the
refusal was reported as a retryable `provider_outcome_unknown`, so the Task parked in
`requires_attention` again every time. The Server never redeployed.

[Decision 033](033-provider-recovery-policy.md) and the platform-deployment guide prescribed the
way out: Recover or Release the node to `ready`, then retry the Task. That path did not work. The
parked platform deployment still holds the Server, so the Recover Operation is refused as busy, and
the Server page's only action canceled the whole platform deployment. A retry that cannot succeed
and a recovery that cannot start left no route out short of abandoning the deployment.

## Decision

An explicit retry of a failed `provision-os` (Task attempt greater than one) is the operator's
request to redeploy that Server. When the provider reports the Machine `failed`, `allocated`,
`broken`, or `rescue`, the retry first runs the same state-driven Recover the Recover Operation
runs, returning the Machine to `ready` without unbinding static IPs, and then replays the frozen
deployment intent. It runs inside the deploying Operation, under the Operation's resource leases.
A Machine already `deployed` with the requested image succeeds as before; one deployed with a
different image is never replaced by a retry and ends the Task instead.

Recovery stays operator-initiated, as decision 033 requires. A first attempt never recovers, and
neither does a Temporal activity retry after worker turnover: it reports the provider's deployment
failure. This extends the existing rule that an explicit retry of a deployment installed without a
provider address releases and redeploys it.

A deployment Swallow's own preflight refuses before any provider write is reported as a known,
non-retryable `deployment_preflight_rejected` failure, so the Operation ends and frees its Servers
instead of parking on a retry that repeats the refusal.

## Alternatives considered

- **Allow Recover while the deployment is parked.** Rejected: the parked Operation keeps its
  Servers' resource leases, so a second Operation would wait on them indefinitely, and two
  Operations would claim one Server.
- **Make a provider deployment failure non-retryable.** Rejected: the Operation would end, and the
  operator would have to Recover the Server and then repair or rerun the platform deployment. It
  is the same recovery with more steps and more ways to get it wrong.
- **Recover automatically after a failed deployment.** Rejected, as in decision 033: a failed
  Machine is evidence an operator may want to inspect before anything wipes it.

## Consequences

- Retry on a failed OS deployment does what it says, on single-Server and platform deployments
  alike, and the other Servers of a platform deployment keep their progress.
- The Recover Operation and the deployment retry share one recovery implementation, including its
  escalation when a disk-erasing Release cannot complete.
- A Server page with a failed Server held by a parked Operation links to that Workflow, so the
  operator can retry there rather than cancel the whole Operation.
- Each attempt's normalized error is kept on its timeline event, so a retry no longer erases why an
  earlier attempt failed.
