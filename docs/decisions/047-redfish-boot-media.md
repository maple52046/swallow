# 047. Redfish Boot Media: a swallow-served iPXE ISO mounted through the BMC

- Status: Accepted
- Date: 2026-10-03
- 現行規格：[server-enrollment-and-boot-media.md](../development/server-enrollment-and-boot-media.md) §4 Boot Media

## Context

Some Servers sit on a physical network whose DHCP is provided by the site, not by the
provisioner, so MAAS cannot run DHCP there. The working manual recipe was to mount an iPXE ISO
through the BMC's web UI and make that virtual media the first boot device; the ISO takes a lease
from the site's DHCP and chains to the MAAS rack (`http://<rack>:5248/ipxe.cfg`). Doing that by
hand at every enrollment and every OS deployment does not scale.

MAAS cannot do it for us. It knows only the power driver (`power_type`) and the BMC account it
enlisted; commissioning reports DMI hardware, not Redfish virtual media or boot override, and
MAAS has no "mount this ISO" operation. On `tainan-ci` MAAS drives power over **IPMI** while the
same BMC and account offer **Redfish**, so the power driver is no gate either.

Probing `tainan-ci` (AMI MegaRAC BMC firmware 13.06.10, AMI Aptio BIOS, Redfish 1.15.1) on
2026-10-03 established what a real BMC requires:

- Two ComputerSystems (the host and a GPU baseboard without `Boot`); the baseboard answers HTTP
  500 while the host is off — exactly when a released Server's deployment starts.
- Remote media is off until an OEM action enables it. `InsertMedia` rejects `https://` sources
  and credentials for HTTP, never connects with a non-80 port or an image at the server root, and
  needs a Range-capable server: the BMC streams the ISO at every boot. The BMC network had no
  egress, so a public ISO URL failed too.
- The Redfish `Cd` target hangs the host (the virtual CD lives in the BIOS's "UEFI USB Device"
  group) and a `Continuous` `Cd` override reorders the BIOS fixed boot order; `UefiTarget`
  refuses `Continuous`, `BootNext` works once, and a Redfish `BootOrder` change is re-sorted by
  the BIOS at the next POST.
- MAAS's IPMI driver writes a one-time "force PXE" flag before every power-on, overriding any
  one-time Redfish setting; when that PXE attempt fails the BIOS falls back to its group order.
- A BMC restart unmounts the ISO. The InsertMedia Task completes before the virtual CD is ready:
  a power-on 18 or 67 seconds after a mount left the CD detached, three minutes held through a
  Redfish power-on but once still not through MAAS's IPMI power-on, and a device left detached
  refuses a new mount of the same image until ejected. A fresh mount was also dropped by a Redfish
  restart and by the host booting its disk.
- After a boot without the CD the BIOS deletes the virtual CD's UEFI boot option, and the BMC's
  `/Bios` view can disagree with what the BIOS boots.

## Decision

- **swallow serves the iPXE ISO and fixes its URL.** The API process serves the ISO
  unauthenticated and Range-capable at a URL derived from an installation-fixed base URL (HTTP on
  port 80 for broad BMC compatibility); operators never type an ISO URL per Server.
- **Per Server, swallow owns only intent and history.** The Boot Media setting and the BMC's
  Redfish capability snapshot (with `probedAt`) are swallow-owned fields on the Server, untouched
  by reconcile. BMC credentials stay provisioner-owned: read for each call, never stored or
  logged. The BMC's live state is read, never assumed.
- **Enrollment-time detection** probes the BMC address of new or stale Servers periodically,
  whatever the provisioner's power driver is.
- **Enable is a preflight**: probe, mount, direct the boot, read both back, and save `enabled`
  only when that worked on that Server, because support differs by model and firmware version.
- **Boot direction follows what the firmware supports**: an AMI Aptio fixed boot order with the
  USB group first (persistent), else the virtual CD first in `BootOrder` plus `BootNext`, else the
  DMTF `Cd` override; a one-time `BootNext` is added whenever possible.
- **Every OS deployment re-ensures** with an internal `ensure-boot-media` Task before
  `provision-os`, re-applying unconditionally with the ISO frozen in the Task; the provisioner's
  own power-on starts the host.
- **A fresh mount settles before anything powers the host on**, and is mounted once more if the
  BMC dropped it.
- **A deployment boot that missed the ISO is recovered once**: the `provision-os` observer remounts
  a dropped ISO early in the boot and, if the host never network-booted, re-applies Boot Media and
  restarts the host through Redfish, not through the provisioner.

Revision history: 2026-10-05 replaced the single installation-supplied ISO with Boot ISOs built
per provisioner ([ADR 049](049-boot-iso-builder.md)); 2026-10-06 added the same ensure before
hardware inspection ([ADR 053](053-server-enrollment-and-automatic-inspection.md)); 2026-10-09
moved BMC access onto the provisioner's Power Configuration and added a `libvirt` method
([ADR 054](054-provisioner-power-configuration.md), [ADR 055](055-libvirt-virtual-machine-enrollment.md))
(original text in git history).

## Alternatives considered

- **Mount the operator-given public HTTPS URL:** rejected — the reference BMC rejects HTTPS and
  could not reach the Internet; the ISO must be streamed from a host the BMC network reaches.
- **Per-Server ISO URL typed by the operator:** rejected by the user once swallow serves the ISO.
- **Preflight only, no Workflow step:** rejected — a BMC restart unmounted the ISO, the BIOS
  re-sorted the boot order, and MAAS overrides one-time settings at power-on.
- **Workflow step only, no preflight:** rejected — an operator must learn that a Server's
  hardware or firmware cannot do it before relying on it.
- **Gate on `power_type == redfish`:** rejected — the reference BMC is driven over IPMI yet
  supports Redfish.
- **Store BMC credentials in swallow:** rejected — the provisioner owns them (decision 001).
- **Use the `Cd` override everywhere:** rejected — it hangs AMI Aptio hosts and destroys the
  BIOS group order.
- **Only lengthen the settle before the provisioner's power-on:** rejected — three minutes still
  lost the mount once at MAAS's IPMI power-on, and no wait is proven sufficient.
- **Power-cycle through the provisioner after a remount:** rejected — the provisioner's IPMI
  power-on is the event that detaches a fresh mount; a Redfish reset does not rewrite the IPMI
  boot flags.
- **Build a per-Site ISO now:** deferred — the ISO embeds the provisioner's rack address; how it
  is produced and distributed for production is a follow-up (taken up by ADR 049).

## Consequences

- A Boot Media Server's deployment may be restarted once by swallow, outside the provisioner,
  when its boot missed the ISO; such a deployment takes ten to fifteen minutes longer, and the
  boot check needs the provisioner's event stream.
- On the reference Server the fixed-boot-order write did not persist, so its deployments boot the
  ISO only through the boot check. Whether preferring BootOrder + BootNext on Aptio avoids that is
  an open follow-up.
- The MAAS integration account must be an administrator to read power parameters.
- Installations must publish the ISO route on port 80 of an address the BMC network reaches.
- Vendor behaviour is encoded in one adapter; a new BMC family may need a new strategy there,
  found by its preflight failing with the BMC's own message.
- With the AMI Aptio strategy a Server keeps booting the ISO first while it is mounted, which is
  the intended permanent behaviour; MAAS answers "boot local" for a deployed machine.
- Only inspections and OS deployments re-ensure; other provisioner power-ons do not.
