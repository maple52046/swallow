# Boot ISOs

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Build, rebuild, list, and delete Boot ISOs: swallow-built iPXE boot ISOs, one provisioner
Integration each, that take an address from the site's own DHCP and chain to that provisioner's
MAAS rack ([decision 049](../../../../../docs/decisions/049-boot-iso-builder.md),
[decision 057](../../../../../docs/decisions/057-uefi-boot-iso-hand-off.md)). A Server's Boot
Media mounts one of them through its BMC or its hypervisor
([server-detail-actions.md](server-detail-actions.md#boot-media)).

## Related Glossary Terms

- Boot ISO
- Boot Media
- OS Provisioning Provider
- Integration
- Site

## Endpoints

```text
GET    /api/v1/provisioning/boot-isos
POST   /api/v1/provisioning/boot-isos
GET    /api/v1/provisioning/boot-isos/{id}
POST   /api/v1/provisioning/boot-isos/{id}/rebuild
DELETE /api/v1/provisioning/boot-isos/{id}
GET    /boot-media/ipxe/{id}/swallow-ipxe.iso
```

## Authentication and Authorization

The `/api/v1` routes require an admin bearer token; shared envelopes, codes, and timestamps
follow [conventions.md](conventions.md). The ISO file route is unauthenticated, because a BMC
mounts the URL with no way to send a token; it is described in
[server-detail-actions.md](server-detail-actions.md#the-iso). An ISO holds no secret.

## The Boot ISO

```json
{
  "id": "6b3f0c1e-4f7a-4f53-9d2a-2a7f1d0c9e11",
  "name": "tainan-rack",
  "siteId": "site-tainan",
  "integrationId": "maas-tainan",
  "rackAddress": "10.170.168.20",
  "chainUrl": "http://10.170.168.20:5248/ipxe.cfg",
  "uefiChainUrl": "http://10.170.168.20:5248/grub/grub.cfg-<mac>",
  "script": "#!ipxe\n\nset maas_rack 10.170.168.20\n…",
  "grubConfig": "set timeout=0\ninsmod efinet\n…",
  "ipxeVersion": "v2.0.0",
  "sizeBytes": 2402304,
  "sha256": "9f2c…",
  "url": "http://10.170.168.20/boot-media/ipxe/6b3f0c1e-4f7a-4f53-9d2a-2a7f1d0c9e11/swallow-ipxe.iso",
  "inUseBy": 1,
  "createdAt": "2026-10-05T01:20:00Z",
  "createdBy": "admin",
  "rebuiltAt": "2026-10-13T01:40:00Z",
  "rebuiltBy": "admin"
}
```

| Field | Meaning |
| --- | --- |
| `id` | Opaque Boot ISO id. |
| `name` | Operator name, unique per Integration (case-insensitive), 1–63 characters. |
| `siteId`, `integrationId` | The provisioner Integration the ISO chains to, and its Site. Only a Server of this Integration may use it. |
| `rackAddress` | The MAAS rack address as given: an IPv4 address or hostname, optionally with `:port`. |
| `chainUrl` | Where iPXE chains after DHCP on BIOS firmware: `http://<rack host>:<port or 5248>/ipxe.cfg`. |
| `uefiChainUrl` | What the ISO's GRUB loads after DHCP on UEFI firmware: `http://<rack host>:<port or 5248>/grub/grub.cfg-<mac>`, `<mac>` being the interface that took the lease. `""` for an ISO without a GRUB (built before decision 057, which sends UEFI firmware through iPXE too); rebuild it. |
| `script` | The iPXE script BIOS firmware runs, rendered from swallow's fixed template (`autoexec.ipxe`). Display only. |
| `grubConfig` | The GRUB configuration UEFI firmware runs, rendered from swallow's fixed template and embedded in the ISO's GRUB. `""` for an ISO that predates it. Display only. |
| `ipxeVersion` | The iPXE release the ISO was built with. |
| `sizeBytes`, `sha256` | The built file. |
| `url` | Where BMCs mount the ISO (and where an operator can download it). Empty when the installation has no Boot Media base URL. |
| `inUseBy` | How many Servers have Boot Media enabled with this ISO. |
| `createdAt`, `createdBy` | When and by whom it was first built. |
| `rebuiltAt`, `rebuiltBy` | When and by whom it was last rebuilt; omitted when it never was. |

Both paths serve networks whose DHCP is not the provisioner's, and the firmware chooses between
them. On BIOS (ISOLINUX) the ISO boots iPXE: take a DHCP lease, set `next-server` to the rack,
chain `ipxe.cfg`, and retry DHCP after a failure (Ctrl-B opens the iPXE shell). On UEFI (x86_64)
it boots its own GRUB: take a DHCP lease (retried every ten seconds), then load the rack's
per-MAC configuration over HTTP — MAAS's own UEFI path, so MAAS records a UEFI boot and gives the
Machine the EFI system partition its deployment needs; if MAAS does not boot it, GRUB returns to
the firmware. Neither iPXE nor swallow's GRUB is signed: a Server booting the ISO must have Secure
Boot off.

## List

`GET /api/v1/provisioning/boot-isos` — optional query `siteId` and `integrationId` narrow the
list. Returns `200 OK`:

```json
{
  "builder": { "available": true },
  "items": [ { "…": "Boot ISO" } ]
}
```

`builder.available` is `false` with a `reason` when this installation cannot build Boot ISOs:
no Boot Media base URL (`api.bootMedia.baseURL`), the Boot Media directory is not writable, or
the iPXE assets, GRUB, or packaging tools are missing from the image. Items are ordered by name.

## Build

`POST /api/v1/provisioning/boot-isos`

```json
{ "name": "tainan-rack", "integrationId": "maas-tainan", "rackAddress": "10.170.168.20" }
```

Builds the ISO synchronously (seconds) and returns `201 Created` with the Boot ISO. The rack
address must be an IPv4 address or a hostname (letters, digits, `-`, `.`), optionally followed
by `:port` (1–65535); swallow neither resolves nor contacts it. A client may pre-fill it with
the host of the Integration's endpoint, which is the rack when MAAS runs region and rack on one
host.

## Rebuild

`POST /api/v1/provisioning/boot-isos/{id}/rebuild` takes no body. It renders both boot paths again
from the current templates and the ISO's own `rackAddress`, builds the file synchronously, and
replaces it in place. `id` and `url` do not change, so every Server whose Boot Media uses the ISO
boots the new script from its next boot, including deployments and inspections already accepted.
Returns `200 OK` with the Boot ISO. A rebuild that fails leaves the previous file served and the
record unchanged. Rebuilds run one at a time per installation.

## Get and delete

`GET /api/v1/provisioning/boot-isos/{id}` returns `200 OK` with the Boot ISO.

`DELETE /api/v1/provisioning/boot-isos/{id}` removes the record and its file, `204 No Content`.
It is refused with `409` while any Server has Boot Media enabled with it; disable Boot Media on
those Servers or switch them to another Boot ISO first.

## Errors

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_error` | Missing or invalid `name`, `integrationId`, or `rackAddress`; the Integration is not a provisioner. |
| 404 | `not_found` | The Boot ISO, or the `integrationId`, does not exist. |
| 409 | `conflict` | The name is taken for that Integration; the builder is unavailable (the message says why); the Boot ISO is in use (delete). |
| 500 | `internal_error` | Building failed; the message carries the packaging tool's summary. A build stores nothing; a rebuild keeps the previous file and record. |

## Compatibility Notes

Added 2026-10-05. It replaces the installation-supplied ISO (`api.bootMedia.isoPath`) and its
fixed route `/boot-media/ipxe/swallow-ipxe.iso`, which are removed. 2026-10-13
([decision 057](../../../../../docs/decisions/057-uefi-boot-iso-hand-off.md)): additive
`uefiChainUrl`, `grubConfig`, `rebuiltAt`, `rebuiltBy`, and the rebuild route; ISOs built earlier
keep working on BIOS and need a rebuild to boot UEFI firmware through MAAS's UEFI path.
