# 057. Boot ISOs boot UEFI through their own GRUB into MAAS's UEFI path, and can be rebuilt in place

- Status: Accepted
- Date: 2026-10-13

## Context

Two UEFI lab Servers (`lab-afde-mi308-1`, `lab-afde-mi308-2`, libvirt virtual machines with OVMF
whose Boot Media is a Boot ISO from [ADR 049](049-boot-iso-builder.md)) failed every disk
deployment of `ubuntu/noble` in curtin's bootloader step: `Failed to find primary ESP mounted at
/boot/efi`.

MAAS had given them a BIOS disk layout — a 1 MiB `bios_grub` partition and an ext4 root, no EFI
system partition — while the deployment ran in UEFI mode. MAAS 3.7 decides the layout from the
boot method it last recorded for the Machine (`bios_boot_method`), and the rack reports the method
of the path it served. The Boot ISO ran iPXE on both firmwares and chained `ipxe.cfg`, so MAAS
recorded `ipxe`; `get_bios_boot_method()` maps any value it does not know to `pxe`, and
`create_basic_layout()` adds the 512 MiB fat32 `/boot/efi` partition only for `uefi`, which the rack
reports when GRUB requests `grub/grub.cfg-<mac>`. Every UEFI Server booted through a Boot ISO was
therefore commissioned with a layout its deployment cannot use. The ISO itself already booted on
both firmwares; only the hand-off to MAAS was wrong.

The first fix kept iPXE on UEFI and chained the rack's `grubx64.efi`. On `lab-afde-mi308-1` GRUB
started and stopped at its prompt: `prefix`, `root`, `cmdpath`, and `net_default_server` were
empty and it had no address, so a GRUB that iPXE chains gets none of iPXE's network state. GRUB's
own `net_bootp` then took the site DHCP lease, which proved the rest of the path once GRUB is told
where the rack is.

Boot ISOs could not be changed after their build, so fixing the template alone would not reach a
Server already using one.

## Decision

- **The firmware chooses the boot loader.** BIOS keeps iPXE with the script ADR 049 verified
  (DHCP, `next-server` to the rack, chain `ipxe.cfg`). UEFI boots a GRUB built for the ISO with
  `grub-mkstandalone`, its configuration embedded and rendered by swallow from a fixed template:
  `net_bootp` (retried until a card has a lease), the first card with a lease, then the rack's
  `grub/grub.cfg-<mac>` over HTTP. MAAS records `uefi`, identifies the Machine by that MAC (or
  enlists an unknown one), and the next commissioning gives it an EFI system partition.
  `net_default_interface` is set to `<card>_dhcp`, the name GRUB's own variables use, so
  `net_default_mac` resolves for MAAS's `BOOTIF`; it and `net_default_server` are exported into
  MAAS's configuration. If MAAS does not boot the machine, GRUB exits to the firmware.
- **Deployed Machines boot through the same path.** With the Boot ISO first in the boot order,
  MAAS answers a deployed Machine with its local-boot configuration, which searches every disk's
  EFI system partition for the installed loader and chainloads it. GRUB autoloads filesystem
  modules but never partition maps, so the configuration loads `part_gpt` and `part_msdos` up
  front; without them no partition is visible and the search fails.
- **Packaging stays prebuilt.** `genfsimg` still builds the ISO; it receives `ipxe.lkrn` for BIOS
  and the per-ISO GRUB as the EFI binary. The api-server image carries `grub-common` and
  `grub-efi-amd64-bin`; no compiler runs at build time.
- **Boot ISOs are rebuilt in place.** `POST /provisioning/boot-isos/{id}/rebuild` renders the
  current templates for the ISO's stored rack address and atomically replaces the file under the
  same id, so its URL — and every Boot Media setting and accepted Workflow that names it — stays
  valid. A failed rebuild keeps the previous file. The record gains `grubConfig`, `uefiChainUrl`
  (reported only when the ISO has a GRUB; otherwise the Dashboard flags it), and `rebuiltAt` /
  `rebuiltBy`.
- **A Machine commissioned through the old path needs a new inspection.** MAAS applies its default
  layout when commissioning ends; after a rebuild, a UEFI Server must be inspected again before its
  next deployment.

## Alternatives considered

- **Keep iPXE on UEFI and chain MAAS's `grubx64.efi`:** implemented and rejected after the lab test
  above — GRUB receives no network state from iPXE and stops at its prompt. Chaining `bootx64.efi`
  (shim) has the same problem plus shim's own lookup of GRUB.
- **Keep MAAS's iPXE path and make the script also request `grub/grub.cfg-<mac>`, so the rack
  records `uefi` as a side effect:** rejected by the user. It relies on undocumented rack behaviour,
  and the Machine would still not boot the path MAAS believes it booted.
- **Add the EFI system partition to the MAAS layout before deployment:** rejected by the user as a
  repair of the symptom; every new UEFI Server would need it, and MAAS would keep believing the
  Machine boots BIOS.
- **Switch the lab VMs to legacy BIOS firmware:** rejected as a fix; physical UEFI Servers booting
  a Boot ISO would keep failing.
- **Build a new Boot ISO and switch every Server's Boot Media to it:** works without new API, but
  leaves the old ISO wrong for anyone who keeps it and needs one Boot Media change per Server.

## Consequences

- The Boot ISO contract (`boot-isos.md`) gains the rebuild route and additive fields; the Boot ISOs
  page gains a Rebuild action, a "UEFI needs rebuild" flag, and shows both boot scripts.
- Existing installations should rebuild their Boot ISOs and re-inspect UEFI Servers that use them;
  BIOS Servers are unaffected.
- The UEFI path no longer depends on the site DHCP pointing anywhere: GRUB takes any lease and
  reaches the rack by the address in its configuration. The rack must serve HTTP on its boot port
  to the leased network.
- `redfish` Boot Media streams the ISO URL, so a rebuild reaches it at the next boot; `libvirt`
  Boot Media keeps a copy as a hypervisor volume, which the next `ensure-boot-media` replaces
  because its SHA-256 no longer matches.
- Secure Boot must stay off: neither iPXE nor swallow's GRUB is signed.

## Amendment (2026-10-19): Swallow detects a layout that needs a new inspection

The new inspection the Decision requires stayed a manual step, and `lab-afde-mi308-3` was missed:
it had been commissioned once, on 2026-10-09, through the old path. Its MAAS layout held only an
ext4 root numbered as partition 2 (partition 1 had been the `bios_grub` partition), with no EFI
system partition, while MAAS now booted it UEFI. Every disk deployment failed in curtin's
partitioning step (`Cannot find previous partition on disk /dev/nvme0n1`), and each explicit retry
([ADR 059](059-retry-recovers-failed-deployment.md)) released the Machine and redeployed onto the
same layout. Its earlier success had been a RAM deployment, which never partitions the disk. The
Step error said only that the deployment failed.

- **Detection is provider-owned and proof-based.** A provisioner adapter flags a Machine whose
  layout cannot take a disk deployment until it is inspected again (`InspectionRequired` with a
  Swallow-authored reason). MAAS flags a Machine that boots UEFI (`bios_boot_method=uefi`) while its
  boot disk has partitions and no EFI system partition (FAT mounted at `/boot/efi`) exists; a
  Machine without a layout, or with data missing, is never flagged. The flag is projected onto the
  Server as `provisioning.inspectionRequired` / `inspectionRequiredReason`.
- **A disk deployment is refused before it starts.** The deployment preflight shared by acceptance,
  platform validation, and Step execution reads the flag live with the lock and refuses a disk
  deployment with the target issue `inspection_required`; a RAM deployment is allowed. An explicit
  retry checks it before Recover and ends the Step with a non-retryable `inspection_required`, so
  the Operation frees the Server for Inspect instead of wiping it only to be refused.
- **A failed deployment names its cause.** `deployment_failed` carries the provisioner's error
  description and the most specific line of this attempt's installation log (optional
  `InstallationFailureReader` capability; MAAS reads the newest failed installation result that
  started after the Step attempt, downloads only its `install.log`, bounds the line to 300
  characters, removes URL credentials, and drops any line that mentions a password, token, or
  secret).
- **Rejected: adding the EFI system partition to the MAAS layout before deployment** stays rejected
  for the reason above. **Rejected: only warning** — every disk deployment of such a Server fails
  after minutes of work, so accepting it gains nothing.

## Current status

Implemented in `api-server` and `dashboard`; verified on `lab-afde-mi308-1` and
`lab-afde-mi308-2`: after a rebuild and a new inspection MAAS recorded `uefi` and commissioned a
512 MiB fat32 `/boot/efi`, and a disk deployment of `ubuntu/noble` rebooted through the Boot ISO
into MAAS's local-boot configuration, which chainloaded the installed bootloader. The CLI has no
rebuild command yet. The 2026-10-19 amendment is implemented in `api-server` and `dashboard`; the
MAAS detection was checked against the real layouts of `lab-afde-mi308-3` (flagged) and
`lab-afde-mi308-1` (not flagged), and the installation-log line against its failed installation.
