# 032. Swallow manages only self-deployed Platforms, with a live cluster explorer

- Status: Accepted
- Date: 2026-09-19

## Context

Swallow's Platform management grew up around one capability: deploy a k0s (Kubernetes)
Platform, then observe its membership. The public API also let a user *register* an
already-existing Kubernetes or Slurm Platform (`POST /api/v1/platforms/`, producing
`origin=registered`), on the theory that Swallow could manage clusters it did not build.

Two problems followed. First, the product intent is now explicit: Swallow manages only the
runtimes it deploys itself; anything Swallow did not deploy is out of scope. The one
retained exception is OS provisioning's *import existing Server* (`existing_os`), which is a
provisioning capability, not Platform management. Second, "manage" for a Kubernetes console
(as in Rancher and Portainer) means two distinct planes — cluster lifecycle and in-cluster
resource browsing — and Swallow had neither the in-cluster plane nor a clear boundary
between them. The `KubernetesReader` only performed a read-only `GET /api/v1/nodes`.

Registering a foreign cluster also sat awkwardly against [ADR 069](069-swallow-owns-desired-state.md):
Swallow does not own workloads, node state, or membership; it reads them live. Owning a
registration for a cluster Swallow cannot otherwise account for added surface without adding
value.

## Decision

**Swallow manages only Platforms it deploys.** The public API no longer registers an existing
Platform. A Platform record is produced only by a durable deployment
(`POST /api/v1/platforms/deploy`). This applies to both Kubernetes and Slurm.

Concretely:

- `POST /api/v1/platforms/` (register-existing) is removed from the active surface.
- `POST /api/v1/integrations` rejects `kind=platform` (and its `cluster` deprecated alias):
  a `platform` Integration is created only by a successful deployment's completion hook,
  which calls the repository directly and is unaffected.
- `PATCH /api/v1/platforms/{id}` no longer accepts `integrationId`, so a user cannot
  point a Platform at a user-owned Integration as a registration back door. Name and
  `gpuStackOwner` remain editable.
- Existing `origin=registered` records keep working for one release: readable and deletable,
  never uninstallable, and never eligible for the cluster explorer. No new `registered`
  record can be produced through the public API.

**A Platform has two planes.** *Lifecycle* is Swallow-owned intent executed as a durable
Workflow (deploy, uninstall, repair today; scale and upgrade later). *Explorer* is a live
read/write session against the deployed cluster's own API, using the Swallow-owned credential
the deployment produced. The two planes never merge into one Operation.

**The cluster explorer is a live view, never persisted.** For a deployed Kubernetes Platform
Swallow exposes an on-demand, Kubernetes-native surface (`/api/v1/platforms/{id}/kubernetes/...`):
a cluster summary, namespaces, an Application aggregation (Deployment / DaemonSet /
StatefulSet / owner-less Pod, resolved from Pod `ownerReferences`), pods and pod logs,
subsidiary resource lists (Services, Ingresses, ConfigMaps, Secrets, PVCs), YAML `apply`
(with optional dry-run), and node cordon/uncordon. Reads and writes go straight to the
Kubernetes API; nothing is stored in Mongo, consistent with ADR 069. Writes are synchronous
against the cluster API and do not create Workflows; only long-running lifecycle changes are
Workflows. This is on-demand and separate from membership sync: it never writes the Server
membership axis, never touches the sync counters, and never runs on the reconcile interval,
consistent with [ADR 021](021-platform-type-specific-management.md).

**The explorer reuses the deployment credential.** The token a deployment provisions is a
`cluster-admin` ServiceAccount token stored as a Swallow-owned `platform` Integration. The
explorer authenticates with the same token; its role is therefore upgraded from
membership-read to cluster management. A least-privilege split (a separate, narrower token
for the explorer) is deliberately deferred.

## Alternatives considered

- **Keep register-existing for foreign clusters:** rejected — it contradicts the confirmed
  product boundary (manage only self-deployed) and adds ownership Swallow cannot justify
  under ADR 069.
- **A generic Steve-style `/k8s/clusters/{id}` proxy (Rancher):** rejected — a transparent
  proxy bypasses Swallow's provider-owned API contract workflow, its auth model, and its
  testing surface. An opinionated REST surface plus YAML `apply` covers the same needs while
  staying inside the contract.
- **Model in-cluster resources as Swallow entities in Mongo:** rejected — workloads and node
  state are owned by the Kubernetes API; ADR 069 requires a live read, not a mirror.
- **Import kubeconfig / agent (Portainer):** rejected — Swallow already holds a deployment
  credential; an import wizard exists only to attach foreign clusters, which are out of scope.
- **Mint a separate least-privilege explorer token now:** rejected for the first wave — the
  deployment already yields a `cluster-admin` token; a narrower token is a later refinement,
  not a blocker for the console.

## Consequences

- The public Platform surface shrinks: no register, no `integrationId` PATCH, no `platform`
  Integration create. Deployment remains the only way a Platform and its credential appear.
- One-release compatibility keeps existing `registered` records visible and deletable; the
  Dashboard shows them but offers no explorer and no uninstall.
- A deployed Kubernetes Platform gains an in-cluster management console (namespaces,
  applications, pods/logs, apply, node cordon), correlating nodes to Server identity — the
  differentiator a generic tool cannot provide.
- The `cluster-admin` deployment token now authorizes cluster writes. Its backup and rotation
  posture (already covered by integration-credential handling) now also covers cluster
  management, until a least-privilege split lands.
- Day-2 lifecycle (scale nodes, upgrade k0s, etcd snapshot) is explicitly out of the first
  wave but bounded here as lifecycle-plane Workflows, not explorer RPCs, so it is not built
  ad hoc later.
- The planned `/planes` surface (kubeconfig-registered management planes) is superseded: it
  described registering foreign planes, which this decision removes.

## Related

- [ADR 069](069-swallow-owns-desired-state.md) — integrate-don't-rebuild; workloads are a
  live read, refined here for the explorer's live read/write boundary.
- [ADR 007](007-cluster-deployment-ownership.md) — Swallow owns k0s deployment and the
  resulting credential the explorer reuses.
- [ADR 010](010-cluster-lifecycle-actions.md) — Uninstall vs Delete; registered Platforms
  can only be deleted, which this decision keeps for the compatibility window.
- [ADR 015](015-platform-term-disambiguation.md) — Platform is the canonical aggregate.
- [ADR 021](021-platform-type-specific-management.md) — type-specific views and on-demand
  Slurm cluster read, whose read/write boundary the Kubernetes explorer follows.
- [`docs/development/platform-deployment.md`](../development/platform-deployment.md) — the
  lifecycle plane (Workflow/Job/Task/Runner) the explorer sits beside.

## Revision history

- 2026-11-02: referred to the people who use swallow as users instead of operators; the decision is unchanged.
- 2026-11-03: `exporterOwner` is no longer a Platform field (decision 003 retired the exporter owner), so it left the editable fields; the decision is unchanged.
