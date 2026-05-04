# Server

## Definition

A physical or virtual compute unit managed by the platform. A server is the primary
resource entity: it has hardware, a status, an optional owner, and an optional
location. It may host workloads (containers) and may have remote access configured.

## Key Fields

- `id` — unique identifier
- `hostname` — human-readable name; **required**, **must be unique** across all servers
- `ip` — primary IP address; **required**, **must be unique** across all servers
- `status` — operational health state (`ServerStatus`)
- `cpuCores`, `ramGB` — hardware specs
- `gpuType`, `gpuCount` — GPU configuration
- `ownerTeamId` — nullable; team that owns this server
- `ownerUserId` — nullable; user that owns this server
- `location` — optional physical placement (`ServerLocation`)
- `bmc` — optional BMC configuration
- `ssh` — optional SSH configuration
- `os` — operating system string
- `createdAt`, `updatedAt` — timestamps

## Relationships

- A server belongs to at most one owner at a time (team or user, not both simultaneously).
- A server has one `AllocationState` derived from its owner fields.
- A server may be placed within a `Rack`, which is within a `Room`, within a `Datacenter`.
- A server may run zero or more `Container` workloads.

## v1 Semantics

In backend v1, a server entry is a minimal **registry record** — it does not represent
a complete machine profile. Only `hostname` and `ip` are required at creation time.
Owner, location, BMC, SSH, and hardware specs are all optional and not enforced. The
`status` field defaults to `unknown` for all newly registered servers. Uniqueness of
`hostname` and `ip` is enforced at the repository level.

## Notes

`Host` is a deprecated alias for `Server` used in `domain/asset/types.ts` and parts
of the observability layer. New code must use `Server`. See [`_deprecated/host.md`](_deprecated/host.md).

## Related Enums

### ServerStatus

Values: `live | warning | error | maintain | offline | unknown`

| Value      | Meaning                                                                                                                                                                                                                                                                                                                                       |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `live`     | Server is reachable and operating normally                                                                                                                                                                                                                                                                                                    |
| `warning`  | Server is reachable but has non-critical issues                                                                                                                                                                                                                                                                                               |
| `error`    | Server has a critical fault                                                                                                                                                                                                                                                                                                                   |
| `maintain` | Server is in maintenance mode (intentional downtime)                                                                                                                                                                                                                                                                                          |
| `offline`  | Server is confirmed unreachable or unavailable                                                                                                                                                                                                                                                                                                |
| `unknown`  | Insufficient information to determine the server's health or availability. Common causes: newly registered server not yet probed; monitoring agent has not reported; data source is unavailable. `unknown` is distinct from `offline` (which implies confirmed unreachability) and from `maintain` (which is an intentional state). |

UI renders `maintain` as "Maintenance" — the domain value is `maintain`. `unknown`
is the correct default status for a newly registered server before any monitoring
data has been received.

## Related Concepts

- [Owner](owner.md) — derived from `ownerTeamId` / `ownerUserId`.
- [Allocation](allocation.md) — derived state from owner fields.
- [Team](team.md) — possible owner of a server.
- [User](user.md) — possible owner of a server.
- [Location](location.md) — composite reference to physical placement.
- [Datacenter](datacenter.md), [Room](room.md), [Rack](rack.md) — physical hierarchy.
- [Container](container.md) — workload type that can run on a server.
- [Workload](workload.md) — abstract type covering containers and future workload kinds.
- [BMC](bmc.md), [SSH Connection](ssh-connection.md) — access paths.
