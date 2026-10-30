# 049. Boot ISOs built by swallow per provisioner

- Status: Accepted
- Date: 2026-10-05
- 現行規格：[server-enrollment-and-boot-media.md](../development/server-enrollment-and-boot-media.md) §5 Boot ISO（另見 §4 Boot Media）

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

- **swallow builds Boot ISOs.** An operator chooses a provisioner Integration and gives its MAAS
  rack address; swallow renders the boot script from its fixed template — no free-form script —
  builds the ISO synchronously, stores it under the installation's Boot Media directory, and
  serves it unauthenticated with Range at a URL derived from the Boot ISO's id.
- **No compiler at runtime.** The api-server image carries iPXE compiled once from a pinned tag
  with no embedded script, plus iPXE's `genfsimg`; each ISO is packaged with `genfsimg -s`.
- **Boot Media selects a Boot ISO** of the Server's own provisioner Integration. Enabling or
  switching runs the decision 047 preflight with it, and deployments freeze that ISO's URL. A Boot
  ISO cannot be deleted while an enabled Boot Media setting uses it.
- **The installation ISO is retired**, with its configuration and fixed route. A setting enabled
  before this decision must choose a Boot ISO before its next deployment.

Revision history: 2026-10-13 changed the UEFI boot path from iPXE (`ipxe.efi` chaining `ipxe.cfg`)
to a GRUB built into each ISO, and added rebuild in place ([ADR 057](057-uefi-boot-iso-hand-off.md))
(original text in git history).

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

- The api-server image grows by the iPXE binaries and the packaging tools; the Boot Media
  directory must be writable by the API process.
- An upgraded installation's enabled Servers must choose a Boot ISO before their next deployment.
- iPXE is unsigned: a Server booting a Boot ISO must have Secure Boot off.
- Changing the iPXE version means rebuilding Boot ISOs to pick it up; each records the version it
  was built with.
