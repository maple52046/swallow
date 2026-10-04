# Servers and infrastructure

[繁體中文](../../zh-TW/guides/servers-and-infrastructure.md) · [Documentation home](../README.md)

Servers are reconciled projections of provisioner machines. Operators inspect,
organize, protect, and act on them; they do not create Server records.

## Inventory and details

The Servers list supports Site scope, pagination, search, status and capability
filters, saved views, live row updates, and bulk selection. A Server detail
workspace separates summary, activity, monitoring, network, storage, and PCI
observations. A Server where swallow installed Docker CE also gets a
**Containers** tab for its Docker images, containers, volumes, and networks (see
[Managed Software](managed-software.md#docker-engine-api-and-the-containers-tab)).

Always use the opaque Server ID in links and automation. Duplicate hostnames and
addresses are valid across Sites.

## Read the Deployment column

The **Deployment** column shows the Server's OS deployment state. The same
presentation appears on mobile Server cards and in the Server detail header. It
combines the latest swallow deployment result with the provider-neutral
[`provisioning.state`](../../development/glossaries/terms/os-provisioning-state.md)
in this order:

1. Running provider work that is not an OS installation: **Releasing**,
   **Inspecting**, or **Testing**.
2. A swallow deployment that is running or did not succeed: **Deploying**,
   **Verifying**, **Failed**, **Attention**, or **Canceled**.
3. When an OS is installed, its image name as plain text.
4. Otherwise, the OS provisioning state by its own name.

The Server detail header names the outcome instead of the image: **Deployed**
when swallow deployed and verified the installed OS, or **Unknown** when the OS
was installed without a swallow deployment result. The Summary card shows the
installed image.

| Display | Operator meaning |
| --- | --- |
| Inspecting / Testing | The provider is inventorying hardware or running hardware tests. |
| Releasing | The provider is returning the Server to its available pool, including any requested disk erasure. |
| Deploying / Verifying | The provider is installing an OS, or swallow is verifying the installation. |
| Ready | The Server is in the provider's available pool and can accept an OS deployment. This replaces the former “Not deployed” label. |
| Allocated / New / Retired | The Server is reserved but not deployed, not yet hardware-inspected, or withdrawn from service. |
| Failed / Broken / Rescue | The last lifecycle action failed, the provider marked the machine unusable, or it is in a diagnostic environment. |
| Unknown | There is no current provider observation, or the adapter does not recognize the provider state. It does not mean Ready. |

An in-progress label has a small spinner after it. The text remains the state;
the spinner is only a visual cue. Hover the state for details such as the
provider's failure reason.

After Release is accepted, stay on the list: the row changes to **Releasing**
while the provider works and then to **Ready** when the Server returns to the
pool. Provider-only work uses the **Monitor server** row action. **View
workflow** is available when a swallow deployment is running or needs review,
and **Monitor workflow** is the row action while that deployment is running.

## Quick views and fleet signals

- **All** shows the current inventory.
- **Deployable** shows present, unlocked Servers in `ready`.
- **Active deployments** includes provider work in `inspecting`, `deploying`,
  `releasing`, or `testing`, plus swallow deployments that are deploying or
  verifying.
- **Needs attention** includes provider `failed`, `broken`, and `rescue`
  states, plus swallow deployments that failed or require attention.

The fleet overview's **OS deployment → Active / Attention** counts, default
operational sort, and row highlights use the same rules. Running work sorts
first, followed by conditions that need attention.

## Inspect hardware

Choose **Take action → Hardware checks → Inspect hardware** to ask the
provisioner to re-inventory a Server's hardware. MAAS calls this action
*Commission*. The Deployment state is **Inspecting** while it runs. The Server
detail Summary card's **Inspection** field continues to show the provider's
last result label, such as *Passed*.

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
swallow servers list --site-id site1 --provisioning-state inspecting
swallow servers get server1
swallow servers inspect server1
swallow provisioning tags edit --server server1 --add amd-gpu
swallow infrastructure zones list --site-id site1
```

Use `swallow servers --help`, `swallow provisioning tags --help`, and
`swallow infrastructure --help` for exact flags.
