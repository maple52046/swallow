# 046. OS deployment progress and provider-stage stall

- Status: Accepted
- Date: 2026-10-03

## Context

On 2026-10-02 `lab-compute-3` stayed `deploying` for 1h46m. MAAS kept the machine powered on and
`Deploying` while curtin, inside the ephemeral installer, retried package downloads through the
MAAS rack proxy: the lab's egress had lost its path to Canonical's CDN. The newest MAAS event
stayed *Configuring OS* for 1h28m until curtin gave up and MAAS marked the node failed.

Swallow saw only `Deploying` + power on. Its existing guards — a ten-minute power-off check and a
two-hour observation timeout — could not tell a slow installation from one that would never
finish, and operators had no way to see which installation stage a deployment was in.

## Decision

While a provider reports a machine as deploying, the `provision-os` Task reads the provider's
machine event stream on every observation. The newest event since the current attempt started is
projected onto the Server deployment axis as a non-terminal `stage` and `statusReason` (code stays
empty), so the Server shows the stage it is in. If no newer event arrives for 25 minutes, the Task
becomes `requires_attention` with `deployment_provider_stage_stall`, naming the stuck stage.
Progress is measured from provider event timestamps, so a worker restart or activity retry
continues the same measurement. The provider deployment is not aborted. A provider without an
event stream, or a failed read, never produces a stall.

## Alternatives considered

- **Rely on the two-hour observation timeout, or lower it.** Rejected: a global timeout cannot tell
  a slow mirror from a dead one and still burns most of the window; provider events show progress.
- **Abort the provider deployment on stall.** Rejected: the stall may be environmental and fixable
  in place; aborting destroys the evidence an operator needs. The Task asks for attention instead.
- **A package-origin preflight before every deploy, with a per-OS-Image check URL.** Implemented on
  2026-10-03 and removed the same day by user decision. Swallow probed a check URL (MAAS archive,
  OS-family default, or an operator value on the OS Image) through the MAAS proxy and refused
  deploys that would stall. In use it failed its purpose: an operator value pointing at a reachable
  mirror made the probe pass while the installation still fetched from MAAS's unreachable archive,
  the check URL changed nothing about where packages come from, refusals had no in-product fix, and
  every custom image was blocked until a URL was set. A preflight is only worth having if swallow
  can determine each OS image's real repository configuration and probe exactly that; it cannot
  today, so the preflight and the check URL were removed.

## Consequences

- A stalled installer surfaces after 25 minutes with its stuck stage instead of after two hours.
- The deploying stage is visible on the Server while it runs.
- One provider event read per observation tick while deploying.
- Unreachable package sources are still discovered only during installation. Configuring the
  sources installations actually use (provider package repositories, or cloud-init for the
  installed OS) is separate, later work.

## Current status

Implemented: `api-server` step-executor progress projection and stage stall; `dashboard` Server
deploying stage line.
