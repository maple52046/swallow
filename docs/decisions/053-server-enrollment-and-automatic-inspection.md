# 053. Server enrollment guidance and automatic hardware inspection

- Status: Accepted
- Date: 2026-10-06
- 現行規格：[server-enrollment-and-boot-media.md](../development/server-enrollment-and-boot-media.md) §1 Server Enrollment、§2 Hardware inspection

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

- **Server Enrollment has guided paths**, offered from **Add servers** on the Server list:
  network boot (power cycle; on an externally served network, PXE through the iPXE Boot ISO) and
  existing OS (no reboot). Enrollment only puts a Machine into the provisioner's inventory; the
  Server still comes from reconciliation.
- **Hardware inspection is a durable Workflow**, `inspect-hardware`, one Server each, one Job of
  three ensure Tasks: wait until the provider's own enrollment has finished, apply the Server's
  Boot Media as read when the Task runs, then issue a bounded number of inspections, aborting one
  that makes no progress.
- **swallow does not mark the Server failed.** The attention lives on the Workflow, a Machine that
  never booted returns to New, and a retry re-runs the whole Job so Boot Media enabled in the
  meantime is applied before the next commission.
- **Inspection starts automatically** for fresh enrollments only — a `new`, unlocked Server first
  observed recently, on an enabled Integration that has not turned it off, that never had an
  inspection Workflow. The reconciler stays a read-only projection; a separate sweep starts it.
- **Manual Inspect uses the same Workflow**, without the enrollment wait, or retries the one
  waiting for attention.
- **Existing-OS enrollment is one command on the host**: it downloads an enrollment script and the
  swallow CLI from the installation, and the CLI registers the host with the provisioner without a
  reboot. The Server is `deployed` and is never inspected automatically. An admin-only endpoint
  hands out the provisioner endpoint and its stored API credential for the command.

Revision history: 2026-10-09 made the enrollment wait stop for a missing Power Configuration and
made a retried run wait again instead of succeeding at once
([ADR 054](054-provisioner-power-configuration.md)), and added a third, virtual-machine path
([ADR 055](055-libvirt-virtual-machine-enrollment.md)) (original text in git history).

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

- New published Workflow and Task vocabulary; Inspect answers with the Workflow it started or
  resumed and refuses in-progress and deployed states.
- New optional provider capabilities for enrollment settling and existing-host enrollment (MAAS
  only), and an Integration setting that turns automatic inspection off.
- Two new unauthenticated downloads; the installation must serve its CLI to the API process.
- The generated existing-OS command carries the provisioner's API key; a host that runs it
  could use that key. Rotate it when a host is not trusted.
- The Server Default User is still not captured during enrollment (ADR 045 future direction).
