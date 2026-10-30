# 057. Boot ISOs boot UEFI through their own GRUB into MAAS's UEFI path, and can be rebuilt in place

- Status: Accepted
- Date: 2026-10-13
- 現行規格：[server-enrollment-and-boot-media.md](../development/server-enrollment-and-boot-media.md) §5 Boot ISO、§6 需要重新檢視的儲存配置

## Context

Two UEFI lab Servers (`lab-afde-mi308-1`, `lab-afde-mi308-2`, libvirt virtual machines with OVMF
whose Boot Media is a Boot ISO from [ADR 049](049-boot-iso-builder.md)) failed every disk
deployment of `ubuntu/noble` in curtin's bootloader step: `Failed to find primary ESP mounted at
/boot/efi`.

MAAS had given them a BIOS disk layout — a 1 MiB `bios_grub` partition and an ext4 root, no EFI
system partition — while the deployment ran in UEFI mode. MAAS 3.7 decides the layout from the
boot method it last recorded for the Machine (`bios_boot_method`), and the rack reports the method
of the path it served. The Boot ISO ran iPXE on both firmwares and chained `ipxe.cfg`, so MAAS
recorded `ipxe`, which it maps to `pxe`; only `uefi` — reported when GRUB requests
`grub/grub.cfg-<mac>` — gets the 512 MiB fat32 `/boot/efi` partition. Every UEFI Server booted
through a Boot ISO was therefore commissioned with a layout its deployment cannot use. The ISO
itself already booted on both firmwares; only the hand-off to MAAS was wrong.

The first fix kept iPXE on UEFI and chained the rack's `grubx64.efi`. On `lab-afde-mi308-1` GRUB
started and stopped at its prompt: `prefix`, `root`, `cmdpath`, and `net_default_server` were
empty and it had no address, so a GRUB that iPXE chains gets none of iPXE's network state. GRUB's
own `net_bootp` then took the site DHCP lease, which proved the rest of the path once GRUB is told
where the rack is.

Boot ISOs could not be changed after their build, so fixing the template alone would not reach a
Server already using one.

On 2026-10-19 `lab-afde-mi308-3` showed that the required new inspection was easy to miss: it had
been commissioned once through the old path, every disk deployment failed in curtin's partitioning
step, each explicit retry ([ADR 059](059-retry-recovers-failed-deployment.md)) released and
redeployed onto the same layout, and the Step error said only that the deployment failed.

## Decision

- **The firmware chooses the boot loader.** BIOS keeps iPXE with the script ADR 049 verified.
  UEFI boots a GRUB built for the ISO with `grub-mkstandalone`, its configuration rendered by
  swallow from a fixed template: take the site DHCP lease itself, then load the rack's
  `grub/grub.cfg-<mac>` over HTTP — MAAS's own UEFI path, so MAAS records `uefi` and the next
  commissioning gives the Machine an EFI system partition.
- **Deployed Machines boot through the same path**: MAAS's local-boot configuration, reached through
  the same GRUB, chainloads the installed loader.
- **Packaging stays prebuilt**: `genfsimg` still builds the ISO; no compiler runs at build time.
- **Boot ISOs are rebuilt in place**: the current templates are rendered for the ISO's stored rack
  address and replace its file under the same id, so its URL and everything that names it stay
  valid; a failed rebuild keeps the previous file.
- **A Machine commissioned through the old path needs a new inspection**, and swallow enforces it:
  the provider adapter flags, only on proof, a Machine whose layout cannot take a disk deployment
  until it is inspected again; the deployment preflight refuses a disk deployment of such a Server
  (a RAM deployment is allowed), and an explicit retry stops before Recover. A failed deployment
  names the provisioner's cause and the relevant installation-log line.

Revision history: 2026-10-19 changed the new inspection from a manual step to provider-detected
and enforced at deployment preflight, and added the failure cause to `deployment_failed`
(original text in git history).

## Alternatives considered

- **Keep iPXE on UEFI and chain MAAS's `grubx64.efi`:** implemented and rejected after the lab test
  above — GRUB receives no network state from iPXE and stops at its prompt. Chaining `bootx64.efi`
  (shim) has the same problem plus shim's own lookup of GRUB.
- **Keep MAAS's iPXE path and make the script also request `grub/grub.cfg-<mac>`, so the rack
  records `uefi` as a side effect:** rejected by the user. It relies on undocumented rack behaviour,
  and the Machine would still not boot the path MAAS believes it booted.
- **Add the EFI system partition to the MAAS layout before deployment:** rejected by the user as a
  repair of the symptom; every new UEFI Server would need it, and MAAS would keep believing the
  Machine boots BIOS. The 2026-10-19 revision kept it rejected for the same reason.
- **Switch the lab VMs to legacy BIOS firmware:** rejected as a fix; physical UEFI Servers booting
  a Boot ISO would keep failing.
- **Build a new Boot ISO and switch every Server's Boot Media to it:** works without new API, but
  leaves the old ISO wrong for anyone who keeps it and needs one Boot Media change per Server.
- **Only warn about a layout that needs a new inspection (2026-10-19):** rejected — every disk
  deployment of such a Server fails after minutes of work, so accepting it gains nothing.

## Consequences

- Existing installations should rebuild their Boot ISOs and re-inspect UEFI Servers that use them;
  BIOS Servers are unaffected.
- The UEFI path no longer depends on the site DHCP pointing anywhere: GRUB takes any lease and
  reaches the rack by the address in its configuration. The rack must serve HTTP on its boot port
  to the leased network.
- `redfish` Boot Media reaches a rebuilt ISO at the next boot; `libvirt` Boot Media keeps a copy on
  the hypervisor, which the next ensure replaces.
- Secure Boot must stay off: neither iPXE nor swallow's GRUB is signed.
