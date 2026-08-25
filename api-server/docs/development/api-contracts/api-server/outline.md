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

Consumer: `dashboard`.

| Contract | Status | Endpoint | Purpose |
| --- | --- | --- | --- |
| [auth-login.md](auth-login.md) | Active | `POST /api/v1/auth/login` | Exchange username and password for an access token. |
| [auth-me.md](auth-me.md) | Active | `GET /api/v1/auth/me` | Return the authenticated caller's identity and role. |
| [servers-list.md](servers-list.md) | Active | `GET /api/v1/servers/` | List Servers with filtering and pagination. |
| [site-automation.md](site-automation.md) | Active | `GET/PUT /api/v1/sites/{siteId}/automation` | Configure embedded Ansible execution and write-only credentials. |
| [operations.md](operations.md) | Active | `/api/v1/operations` | Create and observe Swallow-owned playbook executions and logs. |

There is **no** `POST /api/v1/servers/` and **no** `DELETE /api/v1/servers/{id}`. Servers
are produced by reconciling provisioner inventory, not registered or deleted by a caller;
the two contract files that described them were removed because the endpoints do not
exist. See [decision 002](../../../../../docs/decisions/002-server-identity.md) and the
platform glossary term for Server.

### Implemented, contract file pending

These endpoints are implemented and exercised by the dashboard, but do not yet have a
per-endpoint contract file. Their authoritative description is the provisioning glossary
([`docs/glossaries/provisioning.md`](../../../../../docs/glossaries/provisioning.md)) and
[decision 001](../../../../../docs/decisions/001-system-ownership-boundaries.md); extract
each into its own file from [../template.md](../template.md) when the contract is worth
pinning.

| Endpoint | Purpose |
| --- | --- |
| `GET /api/v1/servers/{id}` | One server projection. |
| `GET /api/v1/servers/{id}/provisioner-detail` | Live, provider-neutral detail for one machine, plus provisioner capabilities. |
| `POST /api/v1/servers/{id}/deploy` | Start an OS deployment; `ephemeral` optional and refused if unsupported. |
| `POST /api/v1/servers/{id}/release` | Return the machine to the provisioner's pool. |
| `POST /api/v1/servers/{id}/power-on` \| `power-off` | Power control. |
| `GET /api/v1/servers/{id}/power-state` | Live BMC power state (read-only). |
| `POST /api/v1/servers/{id}/commission` \| `test` \| `abort` \| `override-failed-testing` | Hardware validation. |
| `POST /api/v1/servers/{id}/lock` \| `unlock` \| `mark-broken` \| `mark-fixed` \| `rescue-mode` \| `exit-rescue-mode` | Operator state. |

Each action beyond deploy/release is an optional provider capability: a provisioner that
does not support one refuses the request rather than silently dropping it, and the
capability set returned by `provisioner-detail` says which exist.

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
the provisioning axis). Server detail, clusters (Kubernetes/Slurm registration),
operations, and monitoring alerts are now implemented rather than planned — see the
tables above and the platform's current API surface.

To implement any of them: extract that endpoint from `planned-surface.md` into
its own contract file using [../template.md](../template.md), reconcile it with
the platform glossary, add it to the tables above as Active, and remove it from
this list.

## Maintenance

- Update this file whenever a contract owned by `api-server` is added, changed,
  deprecated, deleted, renamed, promoted from Planned to Active, or moved.
- Update [../outline.md](../outline.md) only when the set of API-owning
  components changes.
