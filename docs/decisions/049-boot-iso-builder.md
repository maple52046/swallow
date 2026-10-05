# 049. Boot ISOs built by swallow per provisioner

- Status: Accepted
- Date: 2026-10-05

## Context

Decision 047 made swallow serve one iPXE ISO that the installation supplies
(`SWALLOW_API_BOOT_MEDIA_ISO_PATH`), and mount it through each enabled Server's BMC. The ISO
embeds the MAAS rack address it chains to, so it was built by hand outside swallow, and a
second Site or rack needed a second hand-built file the model could not hold. Decision 047
deferred per-Site ISOs for that reason.

The recipe is known and verified (MAAS knowledge base, iPXE 2.0.0, MAAS 3.7, OVMF): an iPXE
script that takes a DHCP lease from the site network, sets `next-server` to the rack, and chains
`http://<rack>:5248/ipxe.cfg`, packaged as a BIOS and UEFI ISO with iPXE's `genfsimg`.

## Decision

- **swallow builds Boot ISOs.** On the Provisioning page an operator chooses a provisioner
  Integration and gives its MAAS rack address (pre-filled from the Integration endpoint's host);
  swallow renders the script from its fixed template — no free-form script — and builds the ISO
  synchronously. The ISO is stored under the installation's Boot Media directory and served,
  unauthenticated and with Range, at `<baseURL>/boot-media/ipxe/<isoId>/swallow-ipxe.iso`.
- **No compiler at runtime.** The api-server image carries iPXE `ipxe.lkrn` and `ipxe.efi`
  compiled once from a pinned tag with no embedded script, plus iPXE's `genfsimg`. Each ISO is
  `genfsimg -s autoexec.ipxe`: the script goes on the ISO's ESP root, which iPXE's EFI build runs
  as its autoexec script, and to the BIOS kernel as its initrd script.
- **Boot Media selects a Boot ISO.** A Server's Boot Media setting names one Boot ISO of the
  Server's own provisioner Integration; enabling or switching ISOs runs the 047 preflight with
  it, and deployments freeze that ISO's URL in their `ensure-boot-media` and `provision-os`
  Tasks. A Boot ISO cannot be deleted while an enabled Boot Media setting uses it.
- **The installation ISO is retired.** `bootMedia.isoPath` and the fixed
  `/boot-media/ipxe/swallow-ipxe.iso` route are removed. A setting enabled before this decision
  has no Boot ISO; the Server page asks for one and its ensure Task fails
  `boot_media_not_configured` until it is chosen.

## Alternatives considered

- **Compile iPXE for every ISO with the script embedded (`EMBED=`):** rejected — it needs a C
  toolchain in the runtime image and takes minutes per build, for the same result `genfsimg -s`
  gives with prebuilt binaries in seconds.
- **A free-form script editor:** rejected by the user for now; the verified template covers the
  external-DHCP case, and a typo in a free-form script only shows up as a host that never boots.
- **Keep the installation ISO as a default beside built ones:** rejected by the user; two sources
  of the same thing would keep the hand-built path alive.
- **Build ISOs in a durable Workflow:** not needed — a build takes seconds and touches no
  external system.

## Consequences

- New contract `boot-isos.md`; Boot Media in `server-detail-actions.md` gains `isoId` and an
  `image` naming the chosen Boot ISO; `provisioning.md` freezes the Server's own ISO URL.
- The api-server image grows by the iPXE binaries and mtools, xorriso, and syslinux files; the
  Boot Media directory must be writable by the API process.
- An upgraded installation's enabled Servers must choose a Boot ISO before their next deployment.
- iPXE is unsigned: a Server booting a Boot ISO must have Secure Boot off.
- Changing the iPXE version means rebuilding Boot ISOs to pick it up; each records the version it
  was built with.

## Current status

Implemented.
