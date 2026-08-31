# 007. Cluster deployment ownership

- Status: Accepted
- Date: 2026-08-26

Topology note: ADR 011 amends the fixed high-availability topology in this decision while
retaining its ownership, trusted role assignment, credential, membership, and retry
boundaries.

## Context

`Cluster` has so far modelled the registration of a cluster that already exists. Building
one was a manual step outside the platform, which left the `deploy-kubernetes` operation
kind defined but unimplemented and gave an operator no way to go from a set of deployed
servers to a working cluster.

Swallow already owns Ansible execution, stable server identity, dynamic inventory, and
encrypted credential storage (decision 006). What it lacked was an intent that says
"build a cluster out of these servers in these roles", and a way to end up holding a
credential for the result.

The target topology is a highly available control plane: three dedicated k0s controllers
with co-located etcd, plus workers.

## Decision

Swallow owns the intent to build a cluster. A cluster may be created with a deployment
specification; the cluster context validates the topology and delegates execution to the
operation context, which runs a manifest-listed k0s playbook through the existing
embedded runner. The cluster exists with no integration until the deployment succeeds,
which is the state `Cluster.integrationId` already allowed for.

Role assignment is built server-side and passed as trusted variables. Operator-supplied
variables cannot influence which host becomes a controller.

High availability uses k0s control plane load balancing — a keepalived VRRP virtual IP
with k0s's in-process reverse proxy — together with node-local load balancing, rather
than an external load balancer. Controllers are dedicated: they are installed without
`--enable-worker` and do not register as Kubernetes nodes. Controllers join through k0s
controller tokens, which carry the CA material, so no certificate is copied between
hosts.

Because dedicated controllers are not nodes, cluster membership is read from two places:
`/api/v1/nodes` for workers and `k0s-ctrl-*` leases in `kube-node-lease` for controllers.
The cluster remains authoritative about its own membership, as decision 002 requires.
Lease-based control-plane discovery is enabled per integration, because the lease naming
is a k0s implementation detail and must not be assumed of every Kubernetes cluster.

On success the playbook provisions a ServiceAccount token in the new cluster and returns
it through the run's private directory. Swallow stores it as an encrypted cluster-kind
integration addressed at the virtual IP. The credential never passes through run output
or retained artifacts, and the VRRP password is held in the same encrypted store rather
than in the operation's persisted variables.

Failed deployments are retried only when an operator asks. A retry is a new operation
carrying the same targets and variables and a reference to the original. The playbook
treats an existing k0s systemd unit as "already installed", which is what makes a rerun
safe and makes adding nodes to a cluster the same operation with a larger target set.

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

Swallow now owns cluster topology validation: controller counts, role assignment, and the
rule that pod and service ranges must not cover the nodes' own subnet. Getting that
validation wrong produces a cluster that cannot form, so it belongs in the use case
rather than in a playbook comment.

The platform holds a privileged credential for every cluster it builds. It is encrypted
at rest with the same key as other integration credentials, so that key's backup and
rotation now covers cluster access too.

Membership reads become topology-aware. A cluster whose controllers are also workers
reports them once, through nodes; a cluster with dedicated controllers reports them
through leases. Both produce the same member vocabulary.

Cluster upgrades and post-bootstrap configuration changes remain out of scope. Only the
first controller's configuration is authoritative at bootstrap, so changing cluster-wide
settings later is not simply a rerun.

## Current status

Implemented as the first release's cluster deployment path. Its fixed-HA topology was
later expanded by ADR 011.
