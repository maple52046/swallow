# 054. Provisioner Power Configuration, power adapters, and a diagnosable enrollment wait

- Status: Accepted
- Date: 2026-10-09
- 現行規格：[server-enrollment-and-boot-media.md](../development/server-enrollment-and-boot-media.md) §3 Power Configuration 與 power adapters（另見 §2.2）

## Context

Three libvirt virtual machines on `tainan-ci` network-booted through the iPXE Boot ISO and MAAS
enlisted them as New. Their `inspect-hardware` Workflows
([ADR 053](053-server-enrollment-and-automatic-inspection.md)) then sat in
`wait-enrollment-settled` until the twenty-minute timeout. MAAS reported `power_type = ""` and
`power_state = unknown`: no power driver was configured, so the power-off that ends enlistment
could never be read. Their enlistment script set had finished (`Passed`) with
`30-maas-01-bmc-config` skipped, so nothing would ever set a driver either.

Three gaps met there:

- swallow read power facts but could not write them. A remote libvirt hypervisor reached over
  `qemu+ssh` has to be configured per Machine, and VMs on the development host hid the gap
  because they were created through a MAAS VM host, which configures power itself.
- The code equated "no BMC" with "no power": the BMC reader rejected VM-host members and empty
  drivers, the detail view labelled any driver's address a BMC (a `virsh` URI included), and the
  enrollment timeout told operators to check BMC settings.
- The enrollment wait could not tell "enrollment is still running" from "the power-off can never
  be observed", and any retry skipped the wait altogether, so retrying after fixing power could
  commission a Machine whose enrollment had not ended.

## Decision

- **Power Configuration is provisioner-owned and written through.** swallow reads a Server's power
  driver and parameters live and writes them through to the provisioner from the API, the CLI, and
  the Dashboard, and never stores them. The password is write-only. A VM-host member's power
  belongs to its VM host and is read-only.
- **Power adapters are the code model.** Each driver family is one adapter — `bmc` (`ipmi`,
  `redfish`) and `virsh` — that validates and normalizes a configuration. Functions only some
  families have are optional adapter extensions; the BMC adapter exposes the out-of-band access
  Redfish Boot Media runs against, and callers ask the adapter for it instead of branching on the
  driver or on VM-host membership. A driver no family knows is reported verbatim and cannot be
  written.
- **Redfish is a BMC extension, not a power type**; Redfish Boot Media stays a probe of the BMC
  whatever its driver ([ADR 047](047-redfish-boot-media.md)).
- **Control is classified per driver**: `none`, `manual`, or `automatic`.
- **The enrollment wait diagnoses power.** The provider reports settled, enrolling, or power
  uncontrollable; MAAS reports the last only once its enlistment script set — where it sets a
  physical Machine's BMC driver — has finished without an automatic driver or a recorded
  power-off. That stops the Task for a Power Configuration instead of timing out.
- **A retry waits again.** Only an operator-requested inspection skips the wait, and it still stops
  when the Machine has no driver at all, because the provisioner could not power it on.
- **Virsh addresses are clean `qemu+ssh` URIs** that the provisioner connects with; its SSH access
  to the hypervisor is an installation prerequisite, not part of the Power Configuration.

Revision history: 2026-10-09 (same day) let virtual-machine enrollment place the provisioner's
installation-recorded public key on a swallow Hypervisor, and let a `virsh` Server whose address
names a Hypervisor use `libvirt` Boot Media ([ADR 055](055-libvirt-virtual-machine-enrollment.md))
(original text in git history).

## Alternatives considered

- **Make Redfish a third top-level power type:** rejected. ADR 047 rejected gating Boot Media on
  `power_type == redfish` because the reference BMC is driven over IPMI and still offers Redfish.
- **Skip the enrollment wait for virtual machines:** rejected. The commission would still find no
  power driver, and a VM still enlisting would race the commission like a physical server.
- **Shorten the timeout:** rejected. It only moves the same undiagnosed attention earlier and
  would cut short slow physical enlistments.
- **Declare power missing after a short fixed window:** rejected. A physical server gets its IPMI
  driver late in enlistment; the enlistment script set's state says when that can no longer happen.
- **Drive libvirt over SSH from swallow:** rejected. It duplicates the provisioner's power driver
  and would put hypervisor SSH keys into swallow.
- **Store power credentials in swallow:** rejected by [ADR 001](001-system-ownership-boundaries.md);
  the provisioner owns them.
- **Keep treating every retry as an override of the wait:** rejected. Retrying after a fix is the
  normal recovery, and it must not commission an enlisting Machine.

## Consequences

- New Power Configuration routes, provider capability, and CLI commands; only the `bmc` family is
  presented as a BMC.
- New attention code for a missing Power Configuration; ADR 053's "any retried run succeeds at
  once" is replaced by "a retried run waits again".
- A MAAS integration account must be an administrator to read and write power parameters.
- Lab VMs from any number of hypervisors can be enrolled and inspected once MAAS can reach each
  hypervisor over SSH.
