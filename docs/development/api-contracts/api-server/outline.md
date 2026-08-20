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
| [servers-create.md](servers-create.md) | Active | `POST /api/v1/servers/` | Register a Server with a unique hostname and IP. |
| [servers-list.md](servers-list.md) | Active | `GET /api/v1/servers/` | List Servers with filtering and pagination. |
| [servers-delete.md](servers-delete.md) | Active | `DELETE /api/v1/servers/{id}` | Remove a Server from the registry. |

## gRPC Agent Service

Consumer: `agent`.

| Contract | Status | RPC | Purpose |
| --- | --- | --- | --- |
| — | Planned | `agent.v1.AgentService/Connect` (bidirectional stream) | The outbound tunnel the agent uses to report identity and inventory and to receive server messages. |

`proto/agent/v1/agent.proto` is currently the only definition of this surface. It
describes the wire format but not the contract: stream lifecycle, authentication,
reconnect expectations, error semantics, and which side may close are all
undocumented. Write this contract from [../template.md](../template.md) before
changing or extending the agent protocol.

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
- Provisioning: Image, Profile, Job
- Alerts
- Management Planes: Kubernetes, Slurm
- GPU Observability
- Access and SSH Keys
- Server detail, update, assign/unassign, and container endpoints

To implement any of them: extract that endpoint from `planned-surface.md` into
its own contract file using [../template.md](../template.md), reconcile it with
the platform glossary, add it to the tables above as Active, and remove it from
this list.

## Maintenance

- Update this file whenever a contract owned by `api-server` is added, changed,
  deprecated, deleted, renamed, promoted from Planned to Active, or moved.
- Update [../outline.md](../outline.md) only when the set of API-owning
  components changes.
