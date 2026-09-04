# 014. Platform is the canonical managed runtime aggregate

- Status: Accepted
- Date: 2026-09-03

## Context

Swallow originally named every registered or deployed Kubernetes and Slurm runtime a
Cluster. That name implied a multi-node topology even though k0s supports a useful
single-node installation, and it would force future runtime types into Kubernetes-shaped
language. The term also appeared in API routes, Mongo references, Operations, monitoring,
and the Dashboard, so a presentation-only rename would leave two competing models.

## Decision

Platform is the canonical aggregate across the Platform Management bounded context,
persistence, published API, and Dashboard. A Platform has a type such as Kubernetes or
Slurm and may contain one or many Servers. Membership remains an observed projection from
the Platform's own API.

The word cluster remains valid only when an external technology owns that term, such as a
Kubernetes cluster API, a k0s cluster credential, or an Ansible variable consumed by that
technology. `deploy-kubernetes` and `uninstall-kubernetes` therefore remain precise
Operation kinds.

The former `/clusters` routes, `clusterId` fields, and monitoring labels are compatibility
aliases for one release. Canonical storage and new code use `platforms` and `platformId`;
the migration preserves every existing identifier and relationship.

## Alternatives considered

- Rename only Dashboard labels: rejected because backend contracts and future workflows
  would continue teaching two meanings for the same aggregate.
- Treat standalone Kubernetes as something other than a Cluster: rejected because the
  lifecycle, membership, and policy model is otherwise identical.
- Permanently support both names: rejected because every new feature would double its
  contract and testing surface.

## Consequences

The schema migration and one-release compatibility adapter are larger than a UI rename,
but later orchestration code has one stable vocabulary. Existing clients have a bounded
migration window, while Kubernetes-specific implementation details keep their accurate
technical names.

## Current status

Implemented across domain code, Mongo schema v3, API contracts and compatibility aliases,
monitoring labels, the PatternFly Dashboard, and tests. The deprecated Cluster surface remains
available for its documented one-release compatibility window.
