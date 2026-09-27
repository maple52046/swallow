# Servers and infrastructure

[繁體中文](../../zh-TW/guides/servers-and-infrastructure.md) · [Documentation home](../README.md)

Servers are reconciled projections of provisioner machines. Operators inspect,
organize, protect, and act on them; they do not create Server records.

## Inventory and details

The Servers list supports Site scope, pagination, search, status and capability
filters, saved views, live row updates, and bulk selection. A Server detail
workspace separates summary, activity, monitoring, network, storage, and PCI
observations.

Always use the opaque Server ID in links and automation. Duplicate hostnames and
addresses are valid across Sites.

## Provider and observation failures

A provider error does not erase the last projection. The UI surfaces provider
failure and observation age separately from health. Refresh when a single
Server needs an immediate provider-backed update; reconcile remains the fleet
mechanism.

## Tags

Tags can be edited for one Server or as a tri-state bulk change. When MAAS
supports tags, MAAS is authoritative and reconciliation mirrors the result.
Otherwise swallow stores a same-shaped fallback. Consumers see one effective
tag set.

## Zones and Pools

Zones and Pools are swallow-owned grouping resources managed under
**Infrastructure**. Assign a Server through its placement action. A capable
provisioner realizes the grouping; otherwise the swallow-owned value remains
the effective placement.

## Locks and destructive actions

Lock a Server to exclude it from automatic or destructive management. Unlock it
only after confirming no Workflow or external operator depends on the
protection. Power, release, delete, rescue/recovery, and placement actions are
also gated by provider capability and current state.

Deleting a Server is provider-backed: it removes the backing machine before the
projection, preventing reconcile from immediately recreating it.

## CLI examples

```bash
swallow servers list --site-id site1 --provisioning-state deployed
swallow servers get server1
swallow provisioning tags edit --server server1 --add amd-gpu
swallow infrastructure zones list --site-id site1
```

Use `swallow servers --help`, `swallow provisioning tags --help`, and
`swallow infrastructure --help` for exact flags.
