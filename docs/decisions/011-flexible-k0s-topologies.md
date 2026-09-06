# 011. Flexible k0s deployment topologies

- Status: Accepted
- Date: 2026-08-30

> **Terminology note:** This ADR predates [ADR 014](014-platform-resource-language.md).
> Where it names the swallow aggregate or public resource *Cluster*, the canonical term is
> now **Platform**; *cluster* stays valid only for the external technology (Kubernetes/k0s).
> See [ADR 015](015-platform-term-disambiguation.md).

## Context

ADR 007 introduced a deliberately narrow first deployment path: three dedicated k0s
control-plane Servers, at least one worker, and a keepalived virtual IP. That shape is
appropriate for production availability, but it prevents small labs from using one Server
and prevents resource-constrained sites from using one control-plane Server with multiple
workers.

The Node Role vocabulary is already shared by desired role assignment and observed
membership. Adding a synthetic combined role would make deployment placement leak into
the observed cluster model.

## Decision

Swallow supports three inferred Cluster Topologies: standalone, non-HA multi-node, and
high availability. A deployment has either one control-plane Server or an odd number of
at least three. A control-plane role assignment may opt into running workloads without
changing its Node Role; worker assignments always supply workload capacity.

Only high availability configures keepalived and requires an API virtual IP. A
one-control-plane deployment uses that Server's observed address as the stable API
endpoint. Existing requests remain compatible because the new workload co-location flag
defaults to false and existing HA VIP fields retain their behavior.

Role assignment and topology validation stay in the Cluster application use case. The
embedded playbook receives only server-built trusted variables describing HA state,
workload-capable control-plane Servers, and the resolved API endpoint.

## Alternatives considered

- Keep only the HA topology: rejected because it makes the operator provision unnecessary
  machines for labs and non-HA environments.
- Add a `control-plane-worker` Node Role: rejected because co-location is deployment
  placement, while observed membership should retain the closed `control-plane | worker`
  vocabulary.
- Allow two or any even count of control-plane Servers: rejected because it adds failure
  modes without a supported quorum advantage.
- Require a virtual IP for non-HA deployments: rejected because there is no alternate
  control-plane endpoint for it to fail over to.

## Consequences

The automation must conditionally install keepalived and must support k0s controllers with
`--enable-worker`. Verification counts workload-capable control-plane Servers as
Kubernetes nodes while still counting every control-plane Server as an etcd member.

Non-HA deployments are explicitly less resilient and the Dashboard must label that tradeoff
before submission. API and playbook compatibility remain additive for existing HA callers.

## Current status

Implemented in the Cluster deployment contract, Dashboard workflow, and embedded k0s
playbook.
