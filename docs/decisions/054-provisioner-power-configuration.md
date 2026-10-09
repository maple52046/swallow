# 054. Provisioner Power Configuration, power adapters, and a diagnosable enrollment wait

- Status: Accepted
- Date: 2026-10-09

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

- **Power Configuration is provisioner-owned and written through.** A Server's Power
  Configuration is the power driver its provisioner uses and that driver's parameters. swallow
  reads it live and writes it through to the provisioner (MAAS `power_type` and
  `power_parameters`) from the API, the CLI, and the Dashboard, and never stores it. The password
  is write-only: omitted keeps it for an unchanged driver and clears it when the driver changes.
  A VM-host member's power belongs to its VM host and is read-only.
- **Power adapters are the code model.** Each driver family is one power adapter: `bmc` (drivers
  `ipmi` and `redfish`) and `virsh`. A registry resolves a driver to its adapter, which validates
  and normalizes a configuration. Functions only some families have are optional adapter
  extensions: the BMC adapter exposes the out-of-band access Redfish Boot Media runs against, and
  callers ask the adapter for it instead of branching on the driver or on VM-host membership. A
  driver the registry does not know is reported verbatim, has no extension, and cannot be
  written.
- **Redfish is a BMC extension, not a power type.** The provisioner's `redfish` driver is one way
  the BMC family switches power; Redfish Boot Media stays a probe of the BMC whatever its driver
  ([ADR 047](047-redfish-boot-media.md)).
- **Control is classified per driver.** `none` (no driver), `manual` (MAAS `manual`), or
  `automatic` (every other driver, including drivers swallow does not write).
- **The enrollment wait diagnoses power.** The provider reports an observation instead of a
  boolean: settled, enrolling, or power uncontrollable. MAAS reports power uncontrollable only
  once its enlistment script set has finished and the driver is not automatic and power is not
  recorded off, because MAAS sets a physical Machine's BMC driver from within that script set.
  Two such readings stop the Task in `requires_attention` with `power_configuration_required`.
- **A retry waits again.** Only a Workflow requested through `POST /servers/{id}/inspect` skips
  the wait; it still stops for `power_configuration_required` when the Machine has no driver at
  all, because the provisioner could not power it on. Automatic inspection keeps its one-shot,
  no-resume policy.
- **Virsh addresses are clean `qemu+ssh` URIs.** swallow accepts `qemu+ssh://[user@]host[:port]/system`
  without a password, query, or fragment, and a domain name or UUID as `powerId`. The provisioner
  connects with them; its SSH access to the hypervisor is an installation prerequisite, not
  something swallow manages.

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

- New routes `GET` and `PUT /servers/{id}/power-configuration`, provider capability
  `powerConfiguration`, and CLI `swallow servers power-configuration get|set`.
- The provisioner detail shows a `BMC` section only for the `bmc` family; other drivers get a
  password-free `Power` section.
- New attention code `power_configuration_required`; ADR 053's "any retried run succeeds at once"
  is replaced by "a retried run waits again".
- A MAAS integration account must be an administrator to read and write power parameters.
- Lab VMs from any number of hypervisors can be enrolled and inspected once MAAS can reach each
  hypervisor over SSH.

## Current status

Implemented.
