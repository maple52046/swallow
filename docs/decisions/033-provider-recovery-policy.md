# 033. swallow-owned provider recovery policy, including the retry of a failed OS deployment

- Status: Accepted
- Date: 2026-09-20
- 現行規格：[os-provisioning.md](../development/os-provisioning.md) §11 Provider Recovery（§11.3 重試失敗的部署）

## Context

A Server whose provisioner reports `failed` (for example MAAS `FAILED_DEPLOYMENT` or
`FAILED_TESTING`), `broken`, `rescue`, or `allocated` needs a way back to `ready`. While swallow
only forwarded the user's intent to the provisioner, every command dead-ended on the provider's own
rejection. Mark fixed was refused because MAAS accepts it only for a genuinely Broken node, while
the dashboard showed `failed` and `broken` as one "Failed" badge. Release was refused by a
`deployed`-only precondition swallow had hardcoded, although MAAS releases a failed deployment.
Rescue was used as a recovery button, but exiting rescue returns the Machine to its previous
state. The provider's rejection text had become the contract.

A failed OS deployment inside a Workflow had a second dead end. The documented way out, Recover the
node and then retry the `provision-os` Task, could not run: the parked deployment still owned the
Server, so Recover was refused as busy, and each retry was refused by swallow's own preflight
(which accepts only `ready` targets) and reported as retryable, so the Task parked again.

## Decision

### One policy decides recovery

swallow owns a single provider recovery policy. It decides which recovery intent is allowed from
each normalized `provisioning.state` and which provider primitives realize it; the provisioner
executes the chosen primitive and is no longer the first gate a user meets. The normalized states
are swallow's ([decision 048](048-os-provisioning-generic-states.md) (swallow-defined OS
provisioning states)), so a provider adapter maps its states onto them and never re-implements the
policy.

- **Release** is allowed from `deployed`, `allocated`, `failed`, `broken`, and `rescue`, and
  converges the Machine to `ready`. `allocated` (reserved but not deployed, for example after a
  failed or canceled deployment) was added by
  [decision 036](036-provisioning-lifecycle-integrity.md) (provisioning lifecycle integrity).
- **Recover** ("Return to Ready") is a durable Workflow from the same states; a `ready` Server
  succeeds at once. It chooses the primitive from the state it observes and decides again after
  each step: `broken` → Mark fixed; `rescue` → Exit rescue; `failed`, `deployed`, or `allocated` →
  Release without erasing disks; `releasing` → follow that Release. A rescue that will not exit, or
  a Release that cannot complete, escalates to Mark broken and then Mark fixed, which returns the
  Machine without a disk erase. Every primitive is bounded, and Recover succeeds only when it
  observes `ready`.
- **Mark fixed, Mark broken, Rescue enter, and Exit rescue** stay as advanced primitives, gated on
  live state; a refusal carries a swallow-authored reason, never only the provider's message.
- **Rescue is a diagnostic environment, not a recovery path.** It is allowed from `deployed`,
  `broken`, and `failed`, and exiting it restores the previous provider state. The provider's
  rescue-transition states, including their failed variants, normalize to `rescue`.
- The allowed-source matrix and the Recover plan live in one domain policy, used by Release and
  Recover acceptance, Recover's execution, the single-action state changes, the deployment retry
  below, and the dashboard's action gating.
- Other gates stay outside the policy: a Server a Platform claims is never released directly, and
  is refused Recover only while `deployed`
  ([decision 067](067-platform-claim-on-server.md) (Platform Claim on Server)).

### A retry of a failed OS deployment recovers the Server first

- An explicit retry of a failed `provision-os` (Task attempt greater than one) is the user's request
  to redeploy that Server. When the provider reports `failed`, `allocated`, `broken`, or `rescue`,
  the retry runs the same Recover plan inside the deploying Workflow, under the resources it
  already owns, without unbinding static IPs so the frozen network intent still applies, and then
  resubmits the frozen deployment.
- A Server already deployed with the frozen image skips Recover: it succeeds once SSH is reachable,
  and is released and redeployed only when the provider gave it no address. One deployed with a
  different image is never replaced; the retry ends the Task with `deployment_image_mismatch`.
- A first attempt never recovers, and neither does an infrastructure retry or a resumed execution
  at the same attempt: they report the provider's deployment failure, or keep observing an
  Allocated Machine.
- A deployment that swallow's own preflight refuses before any provider write fails as the
  non-retryable `deployment_preflight_rejected`, so the Workflow ends and frees its Servers instead
  of parking on a retry that repeats the refusal.

### Recovery runs only on a user action

Recovery never runs by itself after a general deployment failure. It runs on Recover, Release, an
explicit deployment retry, or a cancel that returns the deployment it interrupted
([decision 061](061-recovery-supersedes-blocking-work.md) (recovering and stopping a Workflow)).
Only an image verification returns its borrowed Server automatically, because the borrow promised
to give it back (decision 036).

## Alternatives considered

- **Keep the passthrough and only widen the dashboard gating.** Rejected: the contradictory backend
  preconditions (the `deployed`-only Release, the MAAS-only Mark fixed) would remain, the UI would
  still promise actions that fail, and the provider's message would stay the real contract.
- **Make Release the only recovery verb.** Rejected: a `broken` Machine is best returned with Mark
  fixed (no disk erase, no recommissioning), and a `rescue` Machine must exit rescue first; Release
  for everything would wipe or churn Machines needlessly.
- **Recover automatically after a failed deployment.** Rejected: a failed Machine is evidence a user
  may want to inspect before anything wipes it, and automatic destructive recovery contradicts
  user-triggered recovery ([decision 007](007-cluster-deployment-ownership.md) (cluster deployment
  ownership) rejected automatic retry of failed deployments;
  [decision 072](072-store-driven-workflow-execution.md) (store-driven Workflow execution) lets
  only a user's retry recover destructively).
- **Allow Recover while the failed deployment is parked.** Rejected: the parked Workflow keeps
  owning the Server, so a second Workflow would wait on it indefinitely, and two Workflows would
  claim one Server.
- **Make a provider deployment failure non-retryable.** Rejected: the Workflow would end, and the
  user would have to Recover the Server and then repair or rerun the deployment, the same recovery
  with more steps and more ways to get it wrong.

## Consequences

- Users get two "make it usable again" verbs, Recover and Release, whose preconditions match what
  the provider allows, plus advanced primitives that refuse with a swallow reason instead of a
  provider error.
- The recovery matrix is one source of truth for the backend, the dashboard's action gating, and
  the platform failure guidance; a new provider adapter only maps its states.
- Retry on a failed OS deployment does what it says, on single-Server and platform deployments
  alike, and the other Servers of a platform deployment keep their progress.
- Recover and the deployment retry share one recovery implementation, including its escalation.

## Revision history

- 2026-09-20: decision 033 made provider recovery a swallow-owned policy that chooses the primitive for each normalized provisioning state, added the durable Recover, and relaxed Release beyond `deployed`.
- 2026-10-19: decision 033 made an explicit retry of a failed OS deployment run the Recover plan before redeploying, and made a deployment refused by swallow's own preflight fail as non-retryable.
- 2026-11-02: merged decision 033 into this ADR; the body states the current decision.
