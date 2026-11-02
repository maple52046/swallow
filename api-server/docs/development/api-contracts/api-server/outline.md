# API Server Contract Outline

This file indexes the API contracts owned by the `api-server` component. Find the
contract you need here and open only that one. A task that changes a contract reads
[../README.md](../README.md) first; consuming or explaining a contract does not.

Only **Active** contracts are implementation-ready. A **Planned** entry means the
API area is expected but has no authoritative contract yet: create or promote the
contract before implementing or integrating against it.

## Shared Conventions

| Document | Status | Purpose |
| --- | --- | --- |
| [conventions.md](conventions.md) | Active | Base path, authentication scheme, role model, error envelope and code-to-status map, timestamp format, pagination envelope, and the operational endpoints (`/livez`, `/readyz`, `/healthz`, `/metrics`). Every HTTP contract below references it instead of repeating these rules. |
| [../openapi/](../openapi) | Active | The OpenAPI 3.0.3 description of every route below: the authoritative wire shape, split into `common.yaml` and `areas/*.yaml` and bundled into `openapi.yaml` (see [../README.md](../README.md)). |

## HTTP REST API

Possible consumers: `dashboard`, the `cli` operator client (`../../../../../cli/`),
and external integrators; neither component owns a contract here. Each contract
lists the components that call it today.

| Contract | Status | Endpoint | Purpose |
| --- | --- | --- | --- |
| [auth-login.md](auth-login.md) | Active | `POST /api/v1/auth/login` | Exchange username and password for a Session: a short-lived access token and a refresh token (cookie for browsers, body for the CLI). |
| [auth-refresh.md](auth-refresh.md) | Active | `POST /api/v1/auth/refresh` | Exchange a refresh token for a new access token, rotating the refresh token (30-second grace; reuse revokes the Session). |
| [auth-logout.md](auth-logout.md) | Active | `POST /api/v1/auth/logout` | End a Session so its refresh token stops working; idempotent. |
| [auth-me.md](auth-me.md) | Active | `GET /api/v1/auth/me` | Return the authenticated caller's identity, role, and authentication method. |
| [api-keys.md](api-keys.md) | Active | `/api/v1/api-keys` | Create, list, and delete the caller's API Keys for non-interactive clients (secret shown once, optional expiry). |
| [modules.md](modules.md) | Active | `GET /api/v1/modules` | List the Extension Modules compiled into the build with the spec schema each declares, from which a client renders generic settings forms and details; a Software module's install spec is checked against its schema. |
| [change-plans.md](change-plans.md) | Active | `/api/v1/change-plans` | Read, approve, and reject the Change Plans of destructive changes; the resource's controller applies an approved plan as a Workflow ([decision 076](../../../../../docs/decisions/076-change-plans.md) (change plans)). |
| [overview.md](overview.md) | Active | `GET /api/v1/overview` | Site-scoped inventory, compute-GPU capacity, integration, platform, operation, and monitoring summary. |
| [sites-integrations.md](sites-integrations.md) | Active | `/api/v1/sites`, `/api/v1/integrations` | Manage Sites and their write-only provider integrations. |
| [servers-list.md](servers-list.md) | Active | `GET /api/v1/servers` | List complete Server projections with filtering, pagination, compute/display GPU Inventory, swallow-defined `provisioning.state` values, and the `platformClaim`. |
| [servers-stream.md](servers-stream.md) | Active | `GET /api/v1/servers/stream` | Live Server projection changes as Server-Sent Events, so the list is patched per row instead of re-read. |
| [server-detail-actions.md](server-detail-actions.md) | Active | `/api/v1/servers/{id}`, `/boot-media/ipxe/{isoId}/swallow-ipxe.iso` | Read a Server, allowlisted live BMC connection detail, provider-backed actions (power, hardware inspection and tests, operator state), read and write its Power Configuration (driver families `bmc` and `virsh`, password write-only), set or clear its Server Default User (optionally installing the Deployment Key with a one-time password), and Boot Media (Redfish for a BMC, libvirt for a VM on a swallow hypervisor): probe, enable with a chosen Boot ISO (preflight) or disable, and the unauthenticated Boot ISO file. |
| [server-enrollment.md](server-enrollment.md) | Active | `/api/v1/provisioning/integrations/{id}/enroll-bundle`, `/api/v1/provisioning/virtual-machine-enrollments`, `/api/v1/servers/{id}/virtual-machines`, `/downloads/swallow-enroll.sh`, `/downloads/swallow` | Server Enrollment: automatic and manual `inspect-hardware` Workflows (wait for the provider's enrollment — stopping for a missing Power Configuration — Boot Media, bounded commission attempts); libvirt virtual machines enrolled by domain name on a swallow hypervisor (`enroll-virtual-machines` Workflow); and the one-line command, unauthenticated script, and CLI download a host uses to enroll with its OS kept. |
| [provisioner-http-proxy.md](provisioner-http-proxy.md) | Active | `GET/PUT /api/v1/provisioning/integrations/{id}/http-proxy` | Read and set a provisioner Integration's HTTP proxy for deployed machines (mode `none`/`builtin`/`external`/`peer` plus URL). |
| [provisioner-subnet-dns.md](provisioner-subnet-dns.md) | Active | `GET /api/v1/provisioning/integrations/{id}/subnets`, `PUT .../subnets/{subnetId}/dns-servers` | List a provisioner Integration's subnets (with whether the provisioner serves their DHCP) and read and set each subnet's DNS servers for `automatic`/`static` deployments. |
| [boot-isos.md](boot-isos.md) | Active | `/api/v1/provisioning/boot-isos`, `/boot-media/ipxe/{id}/swallow-ipxe.iso` | Build, rebuild in place, list, and delete iPXE Boot ISOs (one provisioner Integration each, booting through its MAAS rack: iPXE to `ipxe.cfg` on BIOS, the ISO's own GRUB to MAAS's per-MAC GRUB configuration on UEFI) that a Server's Boot Media mounts. |
| [provisioning.md](provisioning.md) | Active | `/api/v1/provisioning` | List, upload, and delete OS Images, manage Deployment Templates, and submit multi-Server OS Deployments with `automatic`, `dhcp`, or `static` network intent; Release and Recover refuse a Platform-claimed Server. |
| [infrastructure.md](infrastructure.md) | Active | `/api/v1/infrastructure`, `PUT /api/v1/servers/{id}/placement` | Manage Zones and Pools and assign a Server to them. |
| [server-tags.md](server-tags.md) | Active | `GET/POST /api/v1/provisioning/tags` | List a Site's known tags and edit Server tags (single or batch, tri-state). |
| [site-automation.md](site-automation.md) | Active | `GET/PUT /api/v1/sites/{id}/automation`, `PUT /api/v1/sites/{id}/automation/credential` | Configure embedded Ansible execution and its optional write-only become password; every Site logs in with the installation Deployment Key. |
| [ssh-keys.md](ssh-keys.md) | Active | `/api/v1/ssh-keys` | Manage the system Deployment Key and the caller's public-key-only Access Keys, realized into key-capable provisioners. |
| [workflows.md](workflows.md) | Active | `/api/v1/workflows` | Create, list, observe, retry, rerun, and cancel Workflows (Jobs and Tasks), with timeline, logs, and per-Task events. |
| [platforms.md](platforms.md) | Active | `/api/v1/platforms` | Create, change, and delete Kubernetes and Slurm Platforms by their spec (destructive changes through Change Plans), read their status and membership, manage the Slurm deployment requirement and Monitoring Links. |
| [software.md](software.md) | Active | `/api/v1/software` | List the Managed Software catalog, install and uninstall a kind on deployed Servers, and read Software Assignments. |
| [platforms-kubernetes.md](platforms-kubernetes.md) | Active | `/api/v1/platforms/{id}/kubernetes` | Live in-cluster explorer for a deployed Kubernetes Platform: namespaces, applications, pods/logs, subsidiary resources, YAML apply, and node cordon. |
| [registry-credentials.md](registry-credentials.md) | Active | `/api/v1/software/docker-ce/registry-credentials` | Manage the sealed, installation-wide Registry Credentials the Docker Host Explorer attaches to private image pulls (password write-only). |
| [servers-docker.md](servers-docker.md) | Active | `/api/v1/servers/{id}/docker` | Live Docker Host Explorer for a Server where swallow installed Docker CE with `enableApi`: images (pull/remove), containers (create/start/stop/restart/logs/remove), volumes, and networks; nothing persisted. |
| [server-metrics.md](server-metrics.md) | Active | `GET /api/v1/monitoring/metrics`, `GET /api/v1/monitoring/metrics/range` | Read current metric values and bounded usage history (Servers, Pool, Zone, or Platform; aggregate or per Server) from the metrics backend, and list the fixed metric-name set. |
| [monitoring-alerts.md](monitoring-alerts.md) | Active | `/api/v1/monitoring/alerts` | List correlated alerts and create Alertmanager silences. |
| [discovery-prometheus.md](discovery-prometheus.md) | Active | `GET /api/v1/discovery/prometheus`, `GET /api/v1/discovery/ansible` | Prometheus `http_sd` target list with the metrics label contract (`tag` selects one server type), and the dynamic Ansible inventory for diagnostics and external tools. |

There is **no** `POST /api/v1/servers/`: Servers are produced by reconciling provisioner
inventory rather than registered by a caller. `DELETE /api/v1/servers/{id}` is the explicit
exception for removal and is provider-backed: it deletes the backing Machine before the
projection, so a later reconcile cannot recreate the Server. See
[decision 002](../../../../../docs/decisions/002-server-identity.md) (server identity) and swallow's
glossary term for Server.

Server detail, machine actions, Sites, Integrations, and monitoring alerts are
published as Active contracts above. Provider-specific capabilities remain optional and
are advertised by `provisioner-detail`.


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

Note: some groups in `planned-surface.md` are no longer planned. Provisioning `Profile`
and `Job` are retired (automation content belongs in a playbook; deployment progress is
the provisioning axis). Server detail, platforms (Kubernetes/Slurm registration),
operations, monitoring alerts, and SSH Keys are now implemented rather than planned — see
the tables above and swallow's current API surface.

To implement any of them: extract that endpoint from `planned-surface.md` into
its own contract file using [../template.md](../template.md), reconcile it with
swallow's glossary, add it to the tables above as Active, and remove it from
this list.

## Maintenance

- Update this file whenever a contract owned by `api-server` is added, changed,
  deprecated, deleted, renamed, promoted from Planned to Active, or moved.
- Keep each summary to what the contract is for; ownership and behavior live in
  the contract itself.
