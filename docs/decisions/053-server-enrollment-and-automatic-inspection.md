# 053. Server enrollment guidance and automatic hardware inspection

- Status: Accepted
- Date: 2026-10-06

## Context

A Server comes into being only when the reconciler sees a Machine in a provisioner's inventory
([ADR 002](002-server-identity.md)). After installation nothing told an operator how to get a
Machine there: the empty Server list only linked to the provisioner Integration. With MAAS,
getting a machine to a deployable state took two steps outside swallow: network-boot it so MAAS
enlists it as New, then press Commission in the MAAS UI. A host that already runs an OS could
only be added by running MAAS's own `maas-run-scripts` with a MAAS API key, which ADR 045
recorded as missing ("swallow has no enrollment step for existing hosts").

Two facts shape automation of the second step. MAAS creates the Machine while the enlistment
environment is still running and powers the machine off when enlistment ends, so a commission
issued as soon as swallow sees New races the enlistment boot. And a commission that powers the
machine on cannot reach MAAS when the Server's network is not served by MAAS DHCP; it then sits
in Commissioning until a timeout, while the operator's fix is Boot Media
([ADR 047](047-redfish-boot-media.md), [ADR 049](049-boot-iso-builder.md)).

## Decision

- **Server Enrollment has two guided paths**, offered from **Add servers** on the Server list:
  network boot (power cycle; Boot Media first on an externally served network), and existing
  OS (no reboot). Enrollment only puts a Machine into the provisioner's inventory; the Server
  still comes from reconciliation.
- **Hardware inspection is a durable Workflow**, kind `inspect-hardware`, one Server each, one
  Job `ensure-inspected` of three ensure Tasks:
  1. `wait-enrollment-settled` (provisioner Runner) waits for the provider's enrollment to
     finish through an optional provider capability. MAAS reports it settled once the Machine
     has left New, or is New and powered off. A provisioner without the capability skips it.
     Twenty minutes without a settled reading is `requires_attention`; nothing is commissioned.
  2. `ensure-boot-media` (internal Runner) applies the Server's Boot Media, read when the Task
     runs, and does nothing when Boot Media is disabled.
  3. `inspect` (provisioner Runner) issues Inspect (MAAS commission) at most three times. An
     attempt that records no provider progress for 15 minutes is aborted, which returns a
     never-inspected MAAS Machine to New. After the last attempt the Task stops in
     `requires_attention` and names Boot Media as the fix for an unreachable network.
- **swallow does not mark the Server failed.** The attention lives on the Workflow, and a
  Machine that never booted returns to New. Retrying the Task re-runs the whole Job, so Boot
  Media enabled in the meantime is applied before the next commission.
- **Inspection starts automatically** for a present, unlocked Server in `new`, first observed
  within the last 24 hours, whose provisioner Integration is enabled and has not set
  `settings.autoInspect` to `false`, and that never had an `inspect-hardware` Workflow. The
  reconciler stays a read-only projection; a separate sweep starts the Workflow.
- **Manual Inspect uses the same Workflow.** `POST /servers/{id}/inspect` starts it without the
  enrollment wait, or retries the one waiting for attention.
- **Existing-OS enrollment is one command on the host.** It downloads an enrollment script from
  the installation (`/downloads/swallow-enroll.sh`, no credential needed), which downloads the
  swallow CLI from the installation (`/downloads/swallow`) and runs
  `swallow servers enroll --provisioner=maas`. That wraps `maas-run-scripts register-machine`,
  which creates the Machine as Deployed, and `report-results`, which records its hardware
  without a reboot. The Server is projected as `deployed` and keeps its OS; automatic inspection
  never starts for it. An admin-only endpoint hands out the provisioner endpoint and its stored
  API credential for the generated command. The host needs nothing from swallow beforehand.
- **External DHCP is still PXE**, through the iPXE Boot ISO. For the enlisting boot the guide
  shows the Boot ISO URL to mount from the BMC console or with Redfish commands, because swallow
  does not know the BMC of a machine the provisioner has not enlisted yet.

## Alternatives considered

- **Commission from the reconciler when it creates a New Server:** rejected. It writes from a
  read path ([ADR 001](001-system-ownership-boundaries.md)), races the enlistment boot, and has
  no durable retry or attention state.
- **Mark the Server failed after the retries and require a reset to New:** rejected. Recover
  returns a Server to Ready, not New, and MAAS has no primitive that returns a Failed
  commissioning to New; the operator's next step (Boot Media, then Inspect) needs no reset.
- **Rely on MAAS's own commissioning timeout:** rejected. It leaves the Machine Failed
  commissioning after half an hour and gives swallow no point to explain Boot Media.
- **Inspect every New Server on upgrade:** rejected. It would power on Machines operators left
  New on purpose; the 24-hour window limits automation to fresh enrollments.
- **Register existing hosts from api-server:** rejected. Hardware facts must be read on the
  host, so a program has to run there either way.
- **Keep the provisioner credential out of the generated command:** not possible with MAAS;
  `register-machine` needs a MAAS API key. The endpoint is admin-only and never cached.
- **Assume the swallow CLI is installed on the host, or download it from the internet:**
  rejected. Hosts rarely have it and often have no internet access; the installation already
  ships the CLI, so it serves it.
- **Implement MAAS registration again in the shell script:** rejected. The script only fetches
  the CLI, so the provider logic lives once, in `swallow servers enroll`.

## Consequences

- New Workflow kind `inspect-hardware`, Task kinds `wait-enrollment-settled` and `inspect`, and
  a live-resolving form of `ensure-boot-media`. `POST /servers/{id}/inspect` now answers with
  the Workflow it started or resumed and refuses in-progress and deployed states.
- New provider capabilities: enrollment settling and existing-host enrollment (MAAS only).
- New Integration setting `autoInspect`; unset means on.
- New unauthenticated downloads `/downloads/swallow-enroll.sh` and `/downloads/swallow`, and
  config `api.cliBinary`; the production Compose mounts the bundle's `bin/` into the API.
- The generated existing-OS command carries the provisioner's API key; a host that runs it
  could use that key. Rotate it when a host is not trusted.
- The Server Default User is still not captured during enrollment (ADR 045 future direction).

## Current status

Implemented.
