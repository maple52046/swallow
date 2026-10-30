# 055. libvirt virtual machine enrollment and libvirt Boot Media

- Status: Accepted
- Date: 2026-10-09
- 現行規格：[server-enrollment-and-boot-media.md](../development/server-enrollment-and-boot-media.md) §1.3 libvirt virtual machine、§4.6 `libvirt` method

## Context

[ADR 054](054-provisioner-power-configuration.md) let swallow write a Server's Power Configuration
and stop the enrollment wait with `power_configuration_required` when the power-off cannot be
observed. Enrolling three lab VMs on `tainan-ci` still needed an operator to log in to the
hypervisor to start them, to set each VM's `virsh` driver in the window between enlistment and the
wait, and to mount the iPXE ISO on each domain's CD-ROM by hand. At thirty VMs that does not work,
and it breaks the rule that operators work through swallow alone. Configuring MAAS's own SSH access
to the hypervisor was the one step that had to happen on the MAAS host.

MAAS 3.7 facts that shape the decision: the admin machine create needs `architecture` and
`mac_addresses` and commissions unless `commission=false`; the `virsh` power driver starts and
stops a domain with `virsh start` / `virsh destroy` and never changes its boot order; only MAAS's own
VM-host discovery (`add-chassis`, pod probe) rewrites a domain to boot network then disk.

## Decision

- **Virtual machines enroll by name.** An operator names libvirt domains on a Hypervisor (a
  deployed swallow Server) and, for an externally served network, a Boot ISO. swallow reads each
  domain's MAC addresses and architecture, registers the Machine with the provisioner without
  commissioning it and with its `virsh` Power Configuration, and leaves it `new` and powered off.
  Automatic hardware inspection then takes it to `ready`; its commission is what starts the domain,
  through MAAS.
- **It is a durable Workflow** targeting the Hypervisor, with one independent ensure Task per
  domain, so many VMs run in parallel and one failure is retried alone.
- **swallow reaches the Hypervisor with the Deployment Key** (ADR 039/041), as the Hypervisor's
  effective Server Default User or an account the operator names. Being a Hypervisor is a role of
  a deployed Server, not a new record or credential. This refines ADR 054's rejection of driving
  libvirt from swallow: swallow still does not switch power — MAAS does — and holds no hypervisor
  secret of its own; it touches libvirt only for what MAAS cannot do, reading domains and attaching
  Boot Media (and, only when the operator explicitly asks, stopping a running domain before
  enrollment).
- **Boot Media has a method**, chosen from the Power Configuration: `redfish` for a Server with a
  BMC (ADR 047/049, unchanged) and `libvirt` for a `virsh` Server whose address names a Hypervisor,
  where the Boot ISO is uploaded to the Hypervisor's storage, put on the domain's CD-ROM, and the
  CD-ROM boots first in the domain's persistent definition. The setting, the Boot ISO, the
  preflight, and the ensure Task before inspection and deployment are shared.
- **The provisioner's SSH identity is prepared by the installation and placed by swallow.** The
  installation records the co-located MAAS rack's public key on the provisioner Integration, and
  virtual-machine enrollment authorizes it for the login account on the Hypervisor. After
  installation no step leaves swallow.
- **The Dashboard path is an experimental feature**; the API and the CLI are generally available,
  because experimental switches only ever change Dashboard presentation.

## Alternatives considered

- **MAAS `add-chassis chassis_type=virsh`:** rejected. It selects VMs by name prefix rather than
  exact names, rewrites each domain to boot network then disk (which defeats an iPXE ISO on an
  externally served network), and runs asynchronously on the rack, so swallow cannot report per VM.
- **Register the hypervisor as a MAAS VM host:** rejected; MAAS would own the VMs' lifecycle and
  boot order, and ADR 054 kept VM-host management out of swallow.
- **Have the operator type MAC addresses:** rejected by the requirement to name VMs.
- **swallow starts the domain itself:** rejected; it would bypass MAAS's power state and the
  commission that starts it anyway.
- **Attach the ISO as a QEMU HTTP CD-ROM:** rejected; it depends on QEMU's curl block driver and on
  the hypervisor reaching swallow's Boot Media URL. Uploading a volume over the SSH session needs
  neither.
- **Commission at registration:** rejected; the inspect-hardware Workflow owns commissioning, with
  its bounded attempts, attention, and Boot Media ensure.
- **A synchronous enrollment request:** rejected; thirty VMs would hold one request for minutes,
  with no per-VM attention or retry.
- **Store a hypervisor credential in swallow:** rejected; the Deployment Key already authenticates
  swallow to its Servers, and a Hypervisor is one.

## Consequences

- A Hypervisor's login account must be in its `libvirt` group and have the Deployment Key
  authorized. Boot Media may create and start the Hypervisor's default storage pool.
- Changing a VM's domain definition is a mutation of the Hypervisor, so a locked Hypervisor refuses
  libvirt Boot Media and enrollment.
- New published Workflow and Task vocabulary, a provider capability for machine registration, and
  an Integration setting for the provisioner's public key.
