# 047. Redfish Boot Media: a swallow-served iPXE ISO mounted through the BMC

- Status: Accepted
- Date: 2026-10-03

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
2026-10-03 established what a real BMC requires (details in the plan manuscript):

- Two ComputerSystems: the host and the GPU baseboard (`UBB`, no `Boot`). The host must be
  chosen by hardware UUID or by "the only System with `Boot`".
- Remote media is off until the OEM action `AMIVirtualMedia.EnableRMedia` runs.
- `InsertMedia` rejects `https://` sources outright, rejects `UserName`/`Password` for HTTP,
  never connects when the URL has a non-80 port or the image sits at the server root, and needs
  a Range-capable server: the BMC streams the ISO on demand at every boot (`httpfs2`). The public
  ISO URL failed over HTTP too (no egress from the BMC network).
- Boot direction: the Redfish class target `Cd` hangs the host — the virtual CD is a USB CD-ROM
  in the BIOS's "UEFI USB Device" group, and AMI implements a `Continuous` `Cd` override by moving
  the empty "UEFI CD/DVD" group to the front of the BIOS fixed boot order, which undid the
  operator's earlier "virtual media first" setup. `UefiTarget` refuses `Continuous`; `BootNext`
  (one-shot) works; a Redfish `BootOrder` change is re-sorted by the BIOS at the next POST.
- MAAS's IPMI driver writes a one-time "force PXE" boot flag before **every** power-on, which
  overrides any one-time Redfish setting. When that PXE attempt finds no PXE server the BIOS falls
  back to its group order.
- A BMC restart unmounts the ISO. The BMC answered HTTP 503 for about twenty minutes after one
  host POST.
- The InsertMedia Task completes before the virtual CD is ready for the host: a power-on 18 or
  67 seconds after an eject-and-mount left the CD detached (`Inserted: false`, image still
  named) and the host booted its disk. A Redfish power-on three minutes after the mount booted
  the ISO, but MAAS's IPMI power-on three minutes after a fresh mount still detached it once; a
  mount that had been in place across earlier boots survived every MAAS power-on. A device left
  in that state refuses a new mount of the same image until it is ejected.
- While the host is powered off — exactly when a released Server's deployment starts — the GPU
  baseboard System answers HTTP 500.
- After a boot without the CD attached, the BIOS deletes the virtual CD's UEFI boot option, so
  strategies that name that option are not always available; the fixed boot order (USB group
  first) still directs the boot with the Redfish override `Disabled`.
- The BMC's `/Bios` view can disagree with the BIOS: after two writes to the pending settings
  object before one POST, `/Bios` reported the USB group first while the BIOS booted the disk on
  two power-ons and, at its next POST, reported "UEFI Hard Disk" first. A POST that applies a
  pending BIOS change also consumed the one-time `BootNext`. A one-time boot to the virtual CD
  followed by a Redfish restart booted the ISO.
- A fresh mount was also detached by a Redfish `ForceRestart` four minutes after it, and by the
  host's own boot of its disk. Mounting during the POST that followed a Redfish restart booted
  the ISO; mounting two minutes after MAAS's power-on did not (the BIOS had already enumerated
  USB, and deleted the virtual CD's boot option).

## Decision

**swallow serves the iPXE ISO and fixes its URL.** The API process serves one
installation-supplied ISO file, unauthenticated and Range-capable, at
`<api.bootMedia.baseURL>/boot-media/ipxe/swallow-ipxe.iso`. The installation fixes the base URL
(HTTP on port 80 for broad BMC compatibility); operators never type an ISO URL per Server.

**Per Server, swallow owns only intent and history.** The Server's Boot Media setting (`enabled`,
last apply, last error) and its Redfish capability snapshot (with `probedAt`) are swallow-owned
fields on the Server, untouched by reconcile. BMC credentials stay provisioner-owned: they are
read from MAAS `power_parameters` for each call and never stored or logged. The BMC's live state
is read, never assumed.

**Enrollment-time detection.** The API process probes, every ten minutes, each present Server
whose Redfish capability is missing (a newly enrolled Server) or older than a day, against the
BMC address whatever the power driver is.

**Enable is a preflight.** Enabling on the Server detail page probes, mounts, directs the boot,
reads both back, and saves `enabled` only when that worked on that Server — support differs by
hardware model and even by BMC firmware version.

**Boot direction, by what the firmware supports.** (1) AMI Aptio fixed boot order: put the
"UEFI USB Device" group first (persistent, `Continuous`); it survives MAAS's force-PXE because a
failed PXE falls back to it, and an empty USB group falls through to the disk. (2) A BIOS that
lists the virtual CD as a UEFI boot option: put it first in `BootOrder` and set `BootNext` to it
(`Once`). (3) Otherwise the DMTF `Cd` override, `Continuous` when allowed. A one-time `BootNext`
is added whenever possible.

**Every OS deployment re-ensures.** Deploy OS, OS image verification, and a Platform's
`ensure-os` Job add an internal `ensure-boot-media` Task before each `provision-os` of an enabled
Server; it re-applies unconditionally, with the ISO URL frozen in the Task. MAAS powers the
machine on as part of the deployment, so the Task does not reboot it.

**A fresh mount settles before anything powers the host on.** After mounting, the adapter waits
three minutes and checks the CD is still inserted, ejecting and mounting once more if the BMC
dropped it; a device still naming a dropped image is ejected before it is reused. A Server whose
ISO is still mounted pays no wait. Discovery skips
a System it cannot read as long as the host System can be identified.

**A deployment boot that missed the ISO is recovered once.** The `provision-os` Task of an
enabled Server carries the same frozen ISO URL, and its observer watches the boot, timed from its
first Deploying reading (the provisioner's power-on):

- *Media check, at two minutes:* if the BMC no longer holds the ISO, mount it at once — no
  settle, no restart — so a mount exists for the boot check to rely on.
- *Boot check, at ten minutes* (past the slowest POST observed, 8.7 minutes): if the
  provisioner's newest event is still the deployment start — the host never network-booted into
  it — re-apply Boot Media (mount with the settle if missing, boot order, one-time boot to the
  virtual CD) and restart the host; 45 seconds later, mount the ISO again if the restart dropped
  it. The stage-stall clock restarts with the host.

The restart goes through Redfish (`ForceRestart`, or `On` when off), not the provisioner, whose
IPMI power-on writes the force-PXE flag again. The provisioner keeps the machine in Deploying, so
the restarted host chains into the same deployment. Only the boot check restarts the host, once
per observation; a failed recovery is only logged, and a deployment that still never boots the
ISO is reported by the stage-stall detection (decision 046).

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
  is produced and distributed for production is a follow-up.

## Consequences

- New contract sections in `server-detail-actions.md` (Boot Media routes and the ISO route) and
  `provisioning.md` (the `ensure-boot-media` Step and the deployment boot recovery); glossary
  terms BMC and Boot Media.
- A Boot Media Server's deployment may be restarted once by swallow, outside the provisioner,
  when its boot missed the ISO; the restart is logged by the worker. A deployment that needed the
  boot check takes ten to fifteen minutes longer.
- The boot check needs the provisioner's event stream; with a provisioner that reports no events
  only the media check runs.
- On the reference Server the fixed-boot-order write did not persist: the BIOS reported "UEFI
  Hard Disk" first again after applying it, so the ensure Task writes it before every deployment
  and those deployments boot the ISO only through the boot check. Whether preferring BootOrder +
  BootNext on Aptio avoids that is an open follow-up.
- New optional provisioner capability `BMCConnection` (MAAS implements it). The MAAS integration
  account must be an administrator to read power parameters.
- The ISO route is the API's only unauthenticated file route; it serves one configured file.
  Installations must publish it on port 80 of an address the BMC network reaches (the
  production reverse proxy forwards `/boot-media/`).
- Vendor behaviour is encoded in one adapter (`internal/server/infra/redfish`); a new BMC family
  may need a new strategy there, found by its preflight failing with the BMC's own message.
- With the AMI Aptio strategy a Server keeps booting the ISO first while it is mounted, which is
  the intended permanent behaviour; MAAS answers "boot local" for a deployed machine.
- Other MAAS power-ons (commissioning, rescue) benefit from a persistent strategy but are not
  re-ensured; only OS deployments are.

## Current status

Implemented.
