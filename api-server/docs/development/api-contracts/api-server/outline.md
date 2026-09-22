# API Server Contract Outline

This file indexes the API contracts owned by the `api-server` component. Read it
after [../README.md](../README.md) and [../outline.md](../outline.md), then open
only the contract you need.

Only **Active** contracts are implementation-ready. A **Planned** entry means the
API area is expected but has no authoritative contract yet: create or promote the
contract before implementing or integrating against it.

## Shared Conventions

| Document | Status | Purpose |
| --- | --- | --- |
| [conventions.md](conventions.md) | Active | Base path, authentication scheme, role model, error envelope and code-to-status map, timestamp format, pagination envelope. Every HTTP contract below references it instead of repeating these rules. |

## HTTP REST API

Consumers: `dashboard` and the `cli` operator client (`../../../../../cli/`). Both are
conformist HTTP consumers of this surface; neither owns any contract here.

| Contract | Status | Endpoint | Purpose |
| --- | --- | --- | --- |
| [auth-login.md](auth-login.md) | Active | `POST /api/v1/auth/login` | Exchange username and password for an access token. |
| [auth-me.md](auth-me.md) | Active | `GET /api/v1/auth/me` | Return the authenticated caller's identity and role. |
| [overview.md](overview.md) | Active | `GET /api/v1/overview` | Site-scoped inventory, integration, platform, operation, and monitoring summary. |
| [sites-integrations.md](sites-integrations.md) | Active | `/api/v1/sites`, `/api/v1/integrations` | Manage Sites and their write-only provider integrations. |
| [servers-list.md](servers-list.md) | Active | `GET /api/v1/servers/` | List complete Server projections with filtering and pagination. |
| [servers-stream.md](servers-stream.md) | Active | `GET /api/v1/servers/stream` | Live Server projection changes as Server-Sent Events, so the list is patched per row instead of re-read. |
| [server-detail-actions.md](server-detail-actions.md) | Active | `/api/v1/servers/{id}` | Read a Server, allowlisted live BMC connection detail, and provider-backed actions. |
| [provisioning.md](provisioning.md) | Active | `/api/v1/provisioning` | List, upload, and delete provider-owned OS Images, manage Deployment Templates, and submit multi-Server OS Deployments. |
| [infrastructure.md](infrastructure.md) | Active | `/api/v1/infrastructure`, `PUT /api/v1/servers/{id}/placement` | Manage swallow-owned Zones and Pools and assign a Server to them, realized in the provisioner when grouping-capable. |
| [server-tags.md](server-tags.md) | Active | `GET/POST /api/v1/provisioning/tags` | List a Site's known tags and edit Server tags (single or batch, tri-state), driving the provisioner when it owns tags and swallow-owned otherwise. |
| [site-automation.md](site-automation.md) | Active | `GET/PUT /api/v1/sites/{siteId}/automation` | Configure embedded Ansible execution and write-only credentials. |
| [workflows.md](workflows.md) | Active | `/api/v1/workflows` | Create, observe, and retry Swallow-owned Workflows (DAG of Tasks), with timeline, logs, and per-Task events. |
| [operations.md](operations.md) | Deprecated | `/api/v1/operations` | One-release compatibility alias of `workflows.md`; served by the same handlers with a `Deprecation` header. |
| [platforms.md](platforms.md) | Active | `/api/v1/platforms` | Deploy Kubernetes and Slurm Platforms, manage Slurm deployment requirements, lifecycle, uninstall/delete, and observed state (self-deployed only). |
| [platforms-kubernetes.md](platforms-kubernetes.md) | Active | `/api/v1/platforms/{id}/kubernetes` | Live in-cluster explorer for a deployed Kubernetes Platform: namespaces, applications, pods/logs, subsidiary resources, YAML apply, and node cordon. |
| [server-metrics.md](server-metrics.md) | Active | `GET /api/v1/monitoring/metrics` | Read current metric values for servers from the metrics backend, and list the fixed metric-name set. |
| [monitoring-alerts.md](monitoring-alerts.md) | Active | `/api/v1/monitoring/alerts` | List correlated alerts and create Alertmanager silences. |
| [discovery-prometheus.md](discovery-prometheus.md) | Active | `GET /api/v1/discovery/prometheus` | Prometheus `http_sd` target list with the metrics label contract; `tag` selects one server type. |

There is **no** `POST /api/v1/servers/`: Servers are produced by reconciling provisioner
inventory rather than registered by a caller. `DELETE /api/v1/servers/{id}` is the explicit
exception for removal and is provider-backed: it deletes the backing Machine before the
projection, so a later reconcile cannot recreate the Server. See
[decision 002](../../../../../docs/decisions/002-server-identity.md) and swallow's
glossary term for Server.

Server detail, machine actions, Sites, Integrations, and monitoring alerts are
published as Active contracts above. Provider-specific capabilities remain optional and
are advertised by `provisioner-detail`.

## Agent Service

The `agent` component and its gRPC service were **removed** in the refoundation
([decision 001](../../../../../docs/decisions/001-system-ownership-boundaries.md)):
inventory and liveness are read from the provisioner and from `node_exporter`, so there
is no bespoke agent protocol. `proto/agent/v1/agent.proto` no longer exists.

## Planned HTTP Surface

| Document | Status | Purpose |
| --- | --- | --- |
| [planned-surface.md](planned-surface.md) | Planned | The full frontend-facing API surface designed with the dashboard: resource model, naming and design principles, planned resource groups, and open questions. |

`planned-surface.md` is a **design reference, not an implementation-ready
contract**. The resource groups below are described there but have no registered
routes and no active contract:

- Teams
- Users (management)
- Topology: Datacenter, Room, Rack
- GPU Observability
- Access and SSH Keys

Note: some groups in `planned-surface.md` are no longer planned. Provisioning `Profile`
and `Job` are retired (automation content belongs in a playbook; deployment progress is
the provisioning axis). Server detail, platforms (Kubernetes/Slurm registration),
operations, and monitoring alerts are now implemented rather than planned — see the
tables above and swallow's current API surface.

To implement any of them: extract that endpoint from `planned-surface.md` into
its own contract file using [../template.md](../template.md), reconcile it with
swallow's glossary, add it to the tables above as Active, and remove it from
this list.

## Maintenance

- Update this file whenever a contract owned by `api-server` is added, changed,
  deprecated, deleted, renamed, promoted from Planned to Active, or moved.
- Update [../outline.md](../outline.md) only when the set of API-owning
  components changes.
