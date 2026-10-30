# 007. Cluster deployment ownership

- Status: Accepted
- Date: 2026-08-26
- 現行規格：[platform-deployment.md](../development/platform-deployment.md) §2 一次 platform deployment 的解剖（k0s reference）的「拓樸與角色」、§4.4 Result / credential 契約

> **Terminology note:** This ADR predates [ADR 014](014-platform-resource-language.md). Where it
> names the swallow aggregate *Cluster*, the canonical term is now **Platform**; *cluster* stays
> valid only for the external technology (see [ADR 015](015-platform-term-disambiguation.md)).
> [ADR 011](011-flexible-k0s-topologies.md) amends the fixed high-availability topology while
> retaining this decision's ownership, trusted role assignment, credential, membership, and retry
> boundaries.

## Context

`Cluster` had so far modelled the registration of a cluster that already exists. Building
one was a manual step outside the platform, which left the `deploy-kubernetes` operation
kind defined but unimplemented and gave an operator no way to go from a set of deployed
servers to a working cluster.

Swallow already owned Ansible execution, stable server identity, dynamic inventory, and
encrypted credential storage (decision 006). What it lacked was an intent that says
"build a cluster out of these servers in these roles", and a way to end up holding a
credential for the result. The target topology was a highly available control plane: three
dedicated k0s controllers with co-located etcd, plus workers.

## Decision

- Swallow owns the intent to build a cluster. The cluster context validates the topology and
  delegates execution to the operation context, which runs a manifest-listed k0s playbook through
  the embedded runner. The cluster exists with no integration until the deployment succeeds.
- Role assignment is built server-side and passed as trusted variables. Operator-supplied
  variables cannot influence which host becomes a controller.
- High availability uses k0s control plane load balancing (a keepalived VRRP virtual IP with
  k0s's in-process reverse proxy) together with node-local load balancing, not an external load
  balancer. Controllers are dedicated: installed without `--enable-worker`, they do not register as
  Kubernetes nodes. They join through k0s controller tokens, which carry the CA material, so no
  certificate is copied between hosts.
- Because dedicated controllers are not nodes, membership is read from nodes for workers and from
  k0s controller leases for controllers. The cluster remains authoritative about its own
  membership, as decision 002 requires. Lease-based discovery is enabled per integration, because
  the lease naming is a k0s implementation detail.
- On success the playbook provisions a ServiceAccount token in the new cluster and returns it
  through the run's private directory; Swallow stores it as an encrypted integration. The
  credential never passes through run output or retained artifacts, and the VRRP password is held
  in the same encrypted store rather than in the operation's persisted variables.
- Failed deployments are retried only when an operator asks. The playbook treats an existing k0s
  systemd unit as "already installed", which is what makes a rerun safe and makes adding nodes to a
  cluster the same operation with a larger target set.

## Alternatives considered

- k0sctl: rejected. It would add a second orchestration binary to the release, and its
  node-readiness timeout reports failure while the cluster is converging, then attempts a
  reset that cannot succeed against running services — a failure mode an unattended
  control plane should not inherit.
- An external TCP load balancer for the control plane: rejected for the first release. It
  requires operating another component, and `spec.api.externalAddress` disables the
  endpoint reconciler and is incompatible with node-local load balancing.
- Storing k0s's admin kubeconfig: rejected. It authenticates with a client certificate,
  while the cluster reader consumes bearer tokens; adopting it would change a port for no
  gain on a read-only membership query.
- Recording the controller list in swallow instead of reading leases: rejected. It would
  make swallow authoritative about membership it did not observe, contradicting decision
  002, and would drift the moment a controller was replaced outside the platform.
- All-in-one controllers that are also workers: rejected for this topology. It removes
  the etcd quorum that makes the control plane highly available.
- Automatic retry of failed deployments: rejected, consistent with decision 006. A failed
  bootstrap can leave partially joined nodes, and an operator should see the failure
  before it is replayed.

## Consequences

- Swallow owns cluster topology validation: controller counts, role assignment, and the rule that
  pod and service ranges must not cover the nodes' own addresses. Getting it wrong produces a
  cluster that cannot form, so it belongs in the use case rather than in a playbook comment.
- Swallow holds a privileged credential for every cluster it builds, encrypted at rest with the
  same key as other integration credentials, so that key's backup and rotation now covers cluster
  access too.
- Membership reads become topology-aware: controllers that are also workers are reported once,
  through nodes; dedicated controllers through leases. Both produce the same member vocabulary.
- Cluster upgrades and post-bootstrap configuration changes remain out of scope. Only the first
  controller's configuration is authoritative at bootstrap, so changing cluster-wide settings later
  is not simply a rerun.
