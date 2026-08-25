# OS Provisioning

## Definition

**OS provisioning** is installing an operating system onto bare-metal hardware. swallow does
not do this. It delegates to a system that already owns hardware discovery, PXE boot, and
OS installation, and presents that system through one interface.

## OS Provisioning Provider

An external system that enumerates provisionable hardware and installs operating systems
onto it. Ubuntu MAAS is the first and currently only provider.

A provider is registered as an [Integration](site.md#integration) of kind `provisioner`,
scoped to one [Site](site.md). A fleet has **many** provider instances — typically one
MAAS per site — so provider connection details are data, not configuration.

Each provider is identified by its `integrationId` and a stable `providerKind` string
(`maas`). `providerKind` selects the adapter; `integrationId` selects the instance.
Both are persisted in every [Server](server.md) source, so neither may be reissued to mean
something else.

## Machine

An entry in a provider's inventory.

**A machine is not a [Server](server.md).** The distinction survives this redesign, but
its meaning has narrowed: a machine is now the *provider's view*, and a server is *swallow's
projection of it*. They are one-to-one while both exist.

| | `Machine` | `Server` |
|---|---|---|
| Owned by | The provider | swallow |
| Identifier | Provider-side (`system_id`) | `serverId`, swallow-issued |
| Lifetime | Until re-enrolled or removed from the provider | The physical machine's whole life in the platform |
| Status describes | Provisioning readiness | Three separate axes |

The one-to-one link can be re-established: if a machine is re-enrolled and gets a new
provider ID, the reconciler recognises the hardware and re-points the existing server at
it rather than creating a second one. That is the reason `Machine` remains a distinct
concept instead of collapsing into `Server` — the provider's identifier is not stable
enough to be swallow's.

Machine data reaches swallow only through the reconciler. Nothing reads a provider inline to
serve a request.

## MachineStatus

The normalized provisioning lifecycle state, and the value of a server's `provisioning`
axis.

Values: `new | commissioning | ready | allocated | deploying | deployed | releasing | testing | rescue | broken | failed | retired | unknown`

| Value | Meaning |
|-------|---------|
| `new` | Discovered but not yet ready to deploy |
| `commissioning` | The provider is inspecting the hardware |
| `ready` | Can accept a deployment |
| `allocated` | Reserved for a deployment that has not started |
| `deploying` | An OS deployment is in progress |
| `deployed` | An OS is installed and running |
| `releasing` | Being returned to the provider's available pool |
| `testing` | The provider is running hardware tests |
| `rescue` | In a provider rescue mode |
| `broken` | The provider has marked it unusable |
| `failed` | The last lifecycle operation failed |
| `retired` | Withdrawn from service |
| `unknown` | The provider reported a state this version does not recognise |

The set is deliberately **coarse**: it carries only the distinctions swallow acts on. A
provider's richer vocabulary is collapsed into these values, with the original label kept
alongside as `providerStatus` for display. When a provider gains a new state,
`providerStatus` widens and `MachineStatus` does not.

`MachineStatus` is not `ServerStatus` — there is no longer any such thing. It is one of
three axes, and it answers only "can this be deployed". See
[Server](server.md#the-three-status-axes).

## OS Image

An operating system a provider can currently deploy.

- `id` — the value passed back when requesting a deployment (e.g. `ubuntu/jammy`)
- `name` — the label the provider presents to operators
- `osSystem`, `release`, `architecture`

An OS image carries **no packages and no scripts**. Post-install configuration is an
[Operation](../decisions/004-automation-via-awx.md) executed by AWX from a playbook in
git. The earlier `ProvisioningProfile` concept — an image plus packages plus scripts,
stored in swallow — is retired: it made swallow an owner of automation content, which
[decision 001](../decisions/001-system-ownership-boundaries.md) forbids.

## Deployment

Requesting that a provider install an operating system onto a machine.

This term is reserved for the server/OS action. Installing, starting, upgrading, or
uninstalling the Swallow control plane is an
[Installation](../development/glossaries/terms/installation.md), not a deployment.

Deployment is **asynchronous**. A request returns when the provider accepts it, not when
the OS is installed. Progress is observed by the reconciler updating the server's
`provisioning` axis through `deploying` to `deployed` or `failed`.

There is no swallow-side job record for a deployment: the provider owns the work, and the
axis is the progress signal. Long-running work that swallow *does* track is an
[Operation](../decisions/004-automation-via-awx.md), which is a different thing — an
operation is swallow's intent executed by AWX, whereas a deployment is entirely the
provider's.

A machine generally must be `ready` to be deployed, and a `deployed` machine must be
released first.

Installing an OS is only the first half of making a GPU host useful. Drivers, fabric
manager, InfiniBand stack, and kernel tuning are operations that follow, subject to the
cluster's `gpuStackOwner` policy
([decision 003](../decisions/003-metrics-label-contract.md#the-gpustackowner-policy)).

### Ephemeral deployment

A deployment can be **ephemeral**: the OS runs from memory and the machine's disks are
left untouched, so the whole root filesystem is lost on reboot.

swallow treats ephemerality as two separate things, and both matter:

- **An intent**, on the deploy request. A provisioner that cannot do it must refuse the
  request rather than deploy normally. This is the one deploy option where being ignored
  produces the *opposite* of the instruction: the operator asked for a machine that keeps
  nothing, and a disk installation keeps everything.
- **A fact**, on the `provisioning` axis, read back from the provisioner on every pass.
  It is not a memory of what was requested, because a machine can be redeployed the other
  way round without swallow being involved.

The fact has to be visible wherever provisioning state is shown. An ephemeral machine and
a disk-installed one are identical in state, OS, and release, and the difference only
becomes apparent when something is lost.

It also changes what an operation means. Anything an operation configures on an ephemeral
machine — a driver, a package, a tuned kernel parameter — reports success and then
silently un-happens at the next boot. swallow records the ephemerality but does not yet
refuse or warn about operations targeting such a machine; see
[decision 004](../decisions/004-automation-via-awx.md).

Only options meaningful to any provisioner belong on a deploy request. A provisioner's
own switches stay out: mirroring one product's parameter list into a provider-neutral
port would leave every other adapter implementing no-ops.

## Release

Returning a machine to the provider's available pool, making it `ready` again.

Release acts on the provider only. It does not remove the corresponding server: the
physical machine still exists and swallow still manages it. Release changes the
`provisioning` axis, nothing else.

## Deepened provider integration

Deploy and release are the minimum. A provider that does more, swallow surfaces more of —
following the integrate-or-own test in [decision 001](../decisions/001-system-ownership-boundaries.md):
mirror what the fleet is queried by, proxy live what is read one machine at a time, and
drive the provider's own actions rather than reimplementing them.

### Mirrored hardware facts

Beyond the coarse lifecycle state, the reconciler mirrors the hardware facts a fleet is
routinely grouped and filtered by: the machine's **GPU inventory**, its system vendor and
product, its CPU model, its tags, the VM host it belongs to, and its lock and
commissioning/testing status. These are mirrored, not owned — each rides the same
`observedAt` freshness as the rest of the projection, and the provider stays the source
of truth.

The GPU inventory is refreshed on its **own slower cadence**, not on every reconcile
pass. Attached devices cost a call per machine and change only at commissioning, so
folding them into the 60-second reconcile would multiply its request count for
near-static data. The reconcile pass is careful to leave the swept GPU list untouched,
the same way it leaves the cluster-owned membership axis alone.

### Live provisioner detail

The full picture of one machine — firmware, per-disk layout, NUMA topology, the PCI
device map — is **read live from the provider on demand**, never mirrored. It is only
ever looked at one machine at a time, so a live read is always fresh and swallow carries no
schema for it and no staleness to explain. It is exposed as a provider-neutral set of
labelled sections and tables, so a second provider fills the same shape with its own
content.

### Provisioner actions

Actions beyond deploy and release — powering a machine on or off, re-running
commissioning or hardware tests, locking a machine or marking it broken, entering rescue
mode — are the **provider's own operations**, triggered through swallow and mirrored back.
swallow does not reimplement them.

They are **optional capabilities**: an adapter declares which it supports, and an action a
provider cannot do is **refused, not silently dropped**, exactly as an unsupported
ephemeral deploy is. A client reads the capability set and offers only the actions that
exist, rather than presenting a button that always fails. This is the same principle as
ephemerality: an instruction the provider cannot honour must fail loudly, because silently
doing nothing — or the opposite — is worse than an error.

## Retired Concepts

Named here because they appear in older documents and in the frontend:

| Concept | Why it is gone |
|---------|----------------|
| `ProvisioningProfile` | Automation content in swallow. Belongs in a playbook in git |
| `ProvisioningJob` | Execution state in swallow. Deployments are tracked by the provisioning axis; everything else is an Operation |
| Machine **import** | There is nothing to import. Every machine is already projected as a server by the reconciler. What import meant is now tenant allocation |
| `provisioningSource` on a server | Replaced by the server's `source`, which is now identity rather than provenance |

## Out of Scope

- How a provider discovers hardware, boots it, or installs an OS.
- Post-install configuration, which is an Operation.
- Network, DHCP, and DNS management, which the provider owns.

## Related Concepts

- [Server](server.md) — the projection of a machine.
- [Site and Integration](site.md) — how a provider instance is registered.
- [decision 001](../decisions/001-system-ownership-boundaries.md) — the ownership rules
  this document follows.
