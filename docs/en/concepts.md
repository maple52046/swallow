# Core concepts

[繁體中文](../zh-TW/concepts.md) · [Documentation home](README.md)

The shared [glossary](../development/glossaries/README.md) is authoritative.
This page gives operators the minimum model needed to use swallow safely.

## Site and Integration

A **Site** is a swallow-owned physical or logical infrastructure scope such as a
datacenter, colocation cage, or lab. An **Integration** connects exactly one Site
to an external system. A Site usually has one MAAS provisioner and one
Prometheus-compatible metrics integration.

Integration credentials are write-only. Sync metadata reports when a refresh
started, last succeeded, and last failed so operators can judge staleness.

## Server

A **Server** is a projection of a machine in a provisioner's inventory.
Reconciliation creates Servers; there is no create-Server operation. Its opaque
`id` is stable. Hostname, addresses, serial numbers, and MAC addresses are
observed attributes and are not safe identifiers.

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

## Zone, Pool, tag, and lock

Zones and Pools are operator-defined infrastructure grouping. Tags add
capability or policy metadata. When the provisioner supports a fact, it remains
the source of truth; otherwise swallow supplies an owned fallback.

A Server lock protects it from automation and destructive actions. It is not a
replacement for provider state or authorization.

## Platform

A **Platform** is a Kubernetes or Slurm runtime environment deployed by swallow.
It may be single-node or multi-node. swallow owns deployment policy and
lifecycle intent, while the runtime API owns observed membership and live state.
Existing third-party runtimes cannot be registered as managed Platforms.

## Workflow, Job, Task, and Runner

A **Workflow** is durable operator intent. It is composed of reusable **Jobs**
and atomic idempotent **Tasks**. A **Runner** executes each Task through a
provisioner, Ansible, or internal mechanism. Temporal provides durability;
worker and Ansible executor processes must be running for progress.

`Operation` and `Step` remain in some implementation and compatibility
surfaces, but Workflow and Task are the canonical public terms.

## Managed Software and Software Assignment

**Managed Software** is one host-level software product, currently Docker CE,
Podman, or NFS. A **Software Assignment** records desired and last-applied state
for one software kind on one Server. It is not a Server status axis and is
different from a multi-component Platform.

## Staleness

Most views combine cached provider observations with live queries. A failed sync
does not invalidate the last successful data, but it makes that data stale.
Always read timestamps and sync errors before acting on inventory or health.
