# 061. Release and Recover supersede the Workflows holding their Servers on the backend

- Status: Accepted
- Date: 2026-10-19

## Context

A Release or Recover is refused while an unfinished Workflow holds any of its Servers. The
dashboard offered a way through: the Release dialog's "Cancel running operations before
releasing" and the failed Server's "Cancel blocking Operations and Recover" canceled that work from
the browser, then polled every second for up to 30 seconds until the Servers were free, and only
then submitted. While it waited every control was disabled and the modal covered the page, which
operators read as a frozen dashboard. [Decision 060](060-cancel-and-rerun-converge.md) made the
wait longer: canceling an OS deployment now returns the interrupted Server to Ready before the
Workflow ends, which can take minutes, so the dialog would time out.

## Decision

- **The backend supersedes the blocking work.** `POST /provisioning/release-operations` and
  `POST /provisioning/recover-operations` accept `cancelActiveWorkflows`. After every other
  validation passes, Swallow requests cancellation of each unfinished Workflow holding a target and
  creates the Operation at once, ignoring those Workflows in its busy check. The Operation acquires
  the same resource leases, so it waits in `waiting_dependency` and runs only after they end. A
  cancel refused for a Workflow that is still running fails the request as a conflict naming it.
- **A Release observes before acting on every attempt.** A first `release-os` attempt that finds
  the Server already Ready (for example returned by a canceled deployment) or Releasing follows
  that instead of sending another Release. When static IP cleanup was requested but this Step sent
  no Release, it fails with `release_cleanup_not_applied` rather than claiming a cleanup that never
  ran.
- **The dashboard only sends the flag.** Both dialogs close within the request; the toast says the
  work starts once the canceled Workflows end.
- **Platform uninstall supersedes too** (amended 2026-10-27). A Platform whose deployment failed and
  waits for attention (`requires_attention`) shows `deploy_failed` and offers Uninstall, yet that
  deployment still holds its Servers, so the uninstall was refused as busy and the operator had to
  find and cancel the Workflow first. `POST /platforms/{id}/uninstall` now accepts
  `cancelActiveWorkflows` with the same semantics, and with it a Platform still `deploying` may be
  uninstalled. The single and bulk uninstall dialogs list the Workflows holding the targets and
  require the operator to confirm before sending the flag; the CLI's `platforms uninstall`,
  `provisioning release`, and `provisioning recover` take `--cancel-active-workflows`. Without the
  flag the conflict names the Workflow holding the Server.

## Alternatives considered

- **Close the dialog after sending the cancels and ask the operator to release again later.**
  Rejected by the user: it moves the waiting to the operator and loses the intent if they forget.
- **Keep polling in the browser with a longer timeout and an escape button.** Rejected: the intent
  lives in a browser tab, so navigating away or closing it abandons the Release, and the wait is
  unbounded once a canceled deployment returns its Server.
- **For uninstall, cancel from the dashboard and wait a bounded time before submitting** (backend
  unchanged). Rejected by the user on 2026-10-27: a dialog that waits leaves the operator unsure
  what is happening; the cancel belongs inside the uninstall, as it does for Release.

## Consequences

- Release and Recover behind running work are durable intents that start by themselves.
- A superseding Operation can wait as long as the canceled Workflow's own cancel path takes,
  including returning an interrupted deployment (up to 30 minutes).
- The cancel-then-act sequence lives in one backend place for every client, including the CLI.

## Current status

Implemented in `api-server`, `dashboard`, and `cli`.
