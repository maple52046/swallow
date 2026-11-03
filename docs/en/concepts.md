# Core concepts

[繁體中文](../zh-TW/concepts.md) · [Documentation home](README.md)

The shared [glossary](../development/glossaries/README.md) is authoritative.
This page gives users the minimum model needed to use swallow safely.

## Site and Integration

A **Site** is a swallow-owned physical or logical infrastructure scope such as a
datacenter, colocation cage, or lab. An **Integration** connects exactly one Site
to an external system. A Site usually has one MAAS provisioner and one
Prometheus-compatible metrics integration.

Integration credentials are write-only. Sync metadata reports when a refresh
started, last succeeded, and last failed so users can judge staleness.

## Server

A **Server** is a projection of a machine in a provisioner's inventory.
Reconciliation creates Servers; there is no create-Server operation. To add one,
get the machine into the provisioner's inventory: boot it from the network so it
enlists, enroll a host that keeps its OS, or enroll a libvirt virtual machine
from its hypervisor (see [Servers and infrastructure](guides/servers-and-infrastructure.md)).
A Server's `origin` records how it arrived: `discovered`, `enrolled`, or
`imported`. Its opaque `id` is stable. Hostname, addresses, serial numbers, and
MAC addresses are observed attributes and are not safe identifiers.

Server status has independent axes:

| Axis | Owner | Example meaning |
| --- | --- | --- |
| Provisioning | Provisioner | ready, inspecting, deploying, releasing, failed |
| Membership | Platform runtime | member role or no observed membership |
| Health | Metrics backend | current observed health or unknown |

The provisioning axis uses swallow's provider-neutral
[`provisioning.state`](../development/glossaries/terms/os-provisioning-state.md)
value set. Each provisioner adapter maps its own lifecycle to those values. A
provider's own wording, such as MAAS *Commissioning*, appears only in the
display-only `providerState`; clients must not branch on it.

`null` means unknown, not unhealthy. Each observation can have its own
`observedAt`.

## Desired and observed state

A resource that swallow converges has a `spec`, what you asked for, and a
`status`, what swallow last observed together with its `conditions`:

| Condition | `true` means |
| --- | --- |
| `realized` | The observed state matches the current spec. |
| `drifted` | Something changed outside swallow and no longer matches the spec. |
| `requires_attention` | swallow stopped retrying; a user must act. |
| `held` | Convergence is paused, for example while the Server is locked or a Workflow owns it. |

Writing a spec returns once swallow has stored it; the change is applied
afterwards, so read the conditions and `status.observedGeneration` to see
whether it took effect. When a provisioner cannot realize a field, `realized` is
`false` with the reason `capability_missing`, and your spec is kept. Server tags
and placement, Zones and Pools, SSH keys, and the provisioner's HTTP proxy and
Subnet DNS settings work this way; other resources keep the behavior their
guides describe.

## Zone, Pool, tag, and lock

Zones and Pools are user-defined infrastructure grouping. Tags add
capability or policy metadata. A Server's manual tags and its Zone and Pool are
part of its spec, and the provisioner realizes them; tags the provisioner
computes itself are observed only. A provisioner without the capability reports
`capability_missing` instead of swallow keeping a substitute copy.

A Server lock protects it from automation and destructive actions. It is not a
replacement for provider state or authorization.

## Platform

A **Platform** is a Kubernetes or Slurm runtime environment deployed by swallow.
It may be single-node or multi-node. swallow owns deployment policy and
lifecycle intent, while the runtime API owns observed membership and live state.
Existing third-party runtimes cannot be registered as managed Platforms.

## Workflow, Job, Task, and Runner

A **Workflow** is durable user intent. It is composed of reusable **Jobs**
and atomic idempotent **Tasks**. A **Runner** executes each Task through a
provisioner, Ansible, or internal mechanism. swallow's store records every
Workflow and Task state; Temporal executes them. The worker, which dispatches
Workflows, and the Ansible executor must be running for progress.

A few API field names still use the older words Operation and Step, such as
`operationId`; Workflow and Task are the canonical public terms.

## Managed Software and Software Assignment

**Managed Software** is one host-level software product, currently Docker CE,
Podman, NFS, Prometheus, Grafana, the AMD Device Metrics Exporter, or the Node
Exporter, which swallow keeps installed on every deployed Server it automates. A
**Software Assignment** records desired and last-applied state for one software
kind on one Server. It is not a Server status axis and is
different from a multi-component Platform.

## Staleness

Most views combine cached provider observations with live queries. A failed sync
does not invalidate the last successful data, but it makes that data stale.
Always read timestamps and sync errors before acting on inventory or health.
