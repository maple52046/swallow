# 047. Boot Media: swallow builds Boot ISOs per provisioner and boots Servers from them

- Status: Accepted
- Date: 2026-10-03
- 現行規格：[server-enrollment-and-boot-media.md](../development/server-enrollment-and-boot-media.md) §4 Boot Media、§5 Boot ISO、§6 需要重新檢視的儲存配置

## Context

Some Servers sit on a network whose DHCP the site provides, so the provisioner (MAAS) cannot
network-boot them. The manual recipe was to mount an iPXE ISO through the BMC and boot it first; the
ISO takes the site's DHCP lease and chains to the MAAS rack. That does not scale by hand, and MAAS
cannot do it: it knows the power driver and the BMC account, not Redfish virtual media.

The reference BMC (`tainan-ci`: AMI MegaRAC, AMI Aptio BIOS, Redfish 1.15.1) accepts the ISO only over
plain HTTP on port 80 from a Range-capable server it can reach; its Redfish `Cd` target hangs the
host; MAAS's IPMI driver forces PXE before every power-on; and a BMC restart or a provisioner power-on
can drop a fresh mount. The ISO embeds the rack address it chains to, so one installation ISO cannot
serve a second rack. On UEFI, MAAS chooses a Machine's disk layout from the boot method it last
recorded; an ISO that chains MAAS's iPXE path is recorded as `pxe`, so UEFI Servers got a BIOS layout
and every disk deployment failed in curtin.

## Decision

### Boot Media on a Server

- **swallow serves the ISO and derives its URL** from an installation-fixed base URL (HTTP, port 80),
  unauthenticated and Range-capable. Users never type an ISO URL.
- **Per Server, swallow owns only intent and history**: the Boot Media setting and the capability
  probe results with `probedAt`, which reconcile never overwrites. Live BMC or Hypervisor state is
  read, never assumed. BMC credentials come from the provisioner's Power Configuration for each call
  and are never stored ([decision 054](054-provisioner-power-configuration.md) (provisioner Power
  Configuration)).
- **The method follows the Power Configuration**: `redfish` for a Server with a BMC, `libvirt` for a
  `virsh` Server on a swallow Hypervisor ([decision 055](055-libvirt-virtual-machine-enrollment.md)
  (libvirt virtual machine enrollment)); both share the setting, Boot ISO, preflight, and ensure Task.
- **Detection ignores the power driver**: new and stale Servers' BMCs are probed for Redfish
  periodically, because a BMC driven over IPMI may still offer it.
- **Enable is a preflight**: probe, mount, direct the boot, read both back, and save the setting only
  when that worked on that Server, because support differs by model and firmware version.
- **Vendor behavior lives in BMC Profiles; an unrecognized BMC uses the DMTF default profile.** The
  Redfish adapter recognizes each BMC's family from what it reports about itself; the common code uses
  only DMTF behavior, and each family's workarounds and boot-direction strategies live in its own
  profile, so a fix for one family cannot change another's
  ([bmc-profiles.md](../development/bmc-profiles.md)). Boot direction tries the profile's strategies
  in order: for AMI an Aptio fixed boot order with the USB group first (persistent), then the virtual
  CD first in `BootOrder` with a one-time `BootNext`, then the DMTF `Cd` override. A BMC that refuses
  a request (HTTP 403) is unsupported, not unreachable. The probe reports the profile for display.
- **Every hardware inspection and OS deployment re-ensures** with an `internal` `ensure-boot-media`
  Task that re-applies mount and boot direction unconditionally; an OS deployment freezes its Boot ISO.
  The provisioner's own power-on starts the host, after a fresh mount has settled.
- **A deployment boot that missed the ISO is recovered once** (`redfish` only): a dropped ISO is
  remounted, and a host that never network-booted gets Boot Media re-applied and is restarted through
  Redfish, not through the provisioner.

### Boot ISOs

- **swallow builds Boot ISOs per provisioner Integration** from fixed templates filled with the rack
  address the user gives; there is no free-form script. A build is synchronous and stored in the
  installation's Boot Media directory. The installation-supplied single ISO is retired.
- **Boot Media selects a Boot ISO of the Server's own provisioner Integration**; one in use by an
  enabled setting cannot be deleted.
- **No compiler at runtime**: the api-server image carries iPXE built once from a pinned tag without
  an embedded script, and packages each ISO with iPXE's `genfsimg`.
- **The firmware chooses the boot loader.** BIOS runs iPXE, which chains the rack's `ipxe.cfg`. UEFI
  runs a GRUB built for the ISO from a swallow template, which takes the lease itself and loads the
  rack's `grub/grub.cfg-<mac>`, then, as MAAS's own GRUB pre-loader does, its default configuration,
  which enlists a machine MAAS does not know: MAAS's own UEFI path, so MAAS records `uefi` and
  commissioning gives an EFI system partition. Deployed Machines boot their installed loader through
  the same path.
- **Both boot loaders open with a boot mode menu**, in their own text UI: DHCP by default after ten
  seconds, or a static IPv4 address typed at the console (iPXE's form on BIOS, GRUB menus on UEFI).
  The static address only reaches the rack; it is never stored, and MAAS's ephemeral environment still
  takes its own DHCP lease.
- **Boot ISOs are rebuilt in place** from the current templates and the stored rack address, under the
  same id, so every setting and accepted Workflow naming it stays valid; a failed rebuild keeps the
  previous file.

### Disk deployments that need a new inspection

- The provider adapter flags, only on proof, a Machine whose layout cannot take a disk deployment
  until it is inspected again. The deployment preflight refuses a disk deployment of it (RAM is
  allowed), and an explicit retry stops before Recover
  ([decision 033](033-provider-recovery-policy.md) (provider recovery policy)). A failed deployment
  names the provisioner's cause and the relevant installation-log line.

## Alternatives considered

- **Mount a public HTTPS URL, or a per-Server URL the user types:** rejected; the reference BMC rejects
  HTTPS and cannot reach the Internet, and the user rejected typed URLs once swallow serves the ISO.
- **Preflight only, or a Workflow step only:** rejected; a BMC restart, a BIOS re-sort, or MAAS's
  power-on undoes a preflight, and a user must learn that hardware cannot do it before relying on it.
- **Gate on `power_type == redfish`:** rejected; the reference BMC is driven over IPMI yet offers
  Redfish. **Store BMC credentials in swallow:** rejected; the provisioner owns them (decision 054).
- **Use the `Cd` override everywhere:** rejected; it hangs AMI Aptio hosts and destroys the BIOS
  group order.
- **Only lengthen the settle, or power-cycle through the provisioner after a remount:** rejected; three
  minutes still lost the mount once, and the provisioner's IPMI power-on is what detaches it.
- **Compile iPXE per ISO with an embedded script:** rejected; a C toolchain in the runtime image and
  minutes per build, for what prebuilt binaries give in seconds.
- **A free-form script editor, or keeping the installation ISO beside built ones:** rejected by the
  user; a script typo only shows as a host that never boots, and two sources keep the hand-built path.
- **Keep iPXE on UEFI and chain MAAS's `grubx64.efi` (or shim):** implemented and rejected after a lab
  test; GRUB gets no network state from iPXE and stops at its prompt.
- **Have iPXE also request `grub/grub.cfg-<mac>` so the rack records `uefi`:** rejected by the user; it
  relies on undocumented rack behaviour and MAAS would believe a path the Machine did not boot.
- **Add the EFI system partition before deployment, or switch to BIOS firmware:** rejected as repairs
  of the symptom; every new UEFI Server would need them.
- **Build a new Boot ISO and switch every Server instead of rebuilding:** rejected; the old ISO stays
  wrong and every Server needs a Boot Media change.
- **Carry the static address into MAAS's ephemeral environment through `kernel_opts`:** rejected after
  research; MAAS appends those options after `---`, so curtin copies a static `ip=` into the deployed OS,
  which `ip=` wins depends on the initramfs, a typed value reaches them only through undocumented
  rendering of boot loader variables, and every unattended boot would still need someone at the console.
- **Boot a small Linux with an installer-style UI that kexecs MAAS's kernel:** rejected; swallow would
  interpret MAAS's boot loader configurations itself, could not chainload a deployed Machine's EFI
  loader, and would need a kernel and initramfs build.
- **Only warn about a layout that needs a new inspection:** rejected; every disk deployment of such a
  Server fails after minutes of work.

## Consequences

- A Boot Media deployment may be restarted once by swallow outside the provisioner, taking ten to
  fifteen minutes longer; the boot check needs the provisioner's event stream.
- The installation must publish `/boot-media/` on port 80 of an address the BMC network reaches, and
  the MAAS integration account must be an administrator to read power parameters.
- Vendor behaviour lives in the BMC adapter's profiles; a new BMC family may need a profile there,
  found by its preflight failing with the BMC's own message, and is handled by the default profile
  until then. With the Aptio strategy a Server boots the ISO first
  while it is mounted, by design. Other provisioner power-ons do not re-ensure.
- Secure Boot must be off: neither iPXE nor swallow's GRUB is signed. A new iPXE version, like a fixed
  template, reaches a Boot ISO only through a rebuild; UEFI Servers commissioned through the old path
  also need a new inspection.
- Every boot from a Boot ISO waits up to ten seconds at the boot mode menu. A network without any
  DHCP still cannot enlist, commission, or deploy through a Boot ISO; there a static address only lets
  a deployed Machine boot its disk, typed again at every boot.
- Boot Media is not yet a spec/status resource under
  [decision 069](069-swallow-owns-desired-state.md) (swallow owns desired state); it keeps this
  behavior until its migration is designed.

## Revision history

- 2026-10-03: decision 047 made swallow serve an iPXE ISO, mount it through each Server's BMC over Redfish after a per-Server preflight, and re-apply it before every OS deployment.
- 2026-10-05: decision 047 replaced the installation-supplied ISO with Boot ISOs that swallow builds per provisioner from fixed templates.
- 2026-10-13: decision 047 booted UEFI through the ISO's own GRUB into MAAS's UEFI path, rebuilt Boot ISOs in place, and (revised 2026-10-19) refused disk deployments of Servers whose layout needs a new inspection.
- 2026-11-02: merged decision 047 into this ADR; the body states the current decision.
- 2026-11-10: Boot ISOs open with a boot mode menu, DHCP by default or a static IPv4 address typed at the console that only reaches the rack, because MAAS's ephemeral environment cannot take a static address.
- 2026-11-10: the UEFI GRUB also loads MAAS's default configuration after the per-MAC one, because the rack answers an unknown MAC with 404 and a new UEFI machine (pvt250) never enlisted.
- 2026-11-10: vendor behavior moved into BMC Profiles (default, ami, supermicro), with a DMTF-only default for unrecognized BMCs and a refused request (HTTP 403) reported as unsupported, because Supermicro (pvt250) refuses the BIOS read the AMI strategy needs.
