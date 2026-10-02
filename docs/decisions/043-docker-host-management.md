# 043. Docker host management: an enabled Engine API and a live, api-server-mediated explorer

- Status: Accepted
- Date: 2026-10-02

## Context

Software deployment ([ADR 038](038-software-deployment.md)) installs Docker CE on deployed
Servers and records a swallow-owned Software Assignment. Operators then want to manage that
Docker from swallow itself — images first, then containers, volumes, and networks — on the
Server detail page, instead of logging in to each host.

Swallow has no node agent ([ADR 001](001-system-ownership-boundaries.md)); everything it does on
a host runs as versioned Ansible over SSH. Running a Workflow per click (list images, stop a
container) would be slow and would turn interactive reads into durable operations. The Docker
Engine itself exposes an HTTP API that can listen on TCP. The deployment targets are internal
datacenter networks, and the user explicitly deferred hardening (TLS, authentication) for this
version.

Three questions follow and are easy to ask again later: who talks to the Docker Engine API (the
browser or swallow), what swallow persists about the Docker objects, and how the API gets
enabled without inventing a second software lifecycle.

## Decision

**Docker CE gains an `enableApi` variant.** The `docker-ce` install spec accepts a boolean
`enableApi`, defaulting to `true`. When enabled, the install playbook makes `dockerd` listen on
`tcp://0.0.0.0:2375` (plain HTTP, unauthenticated) in addition to its local socket; when
disabled, or on uninstall, swallow's listener configuration is removed. The recorded assignment
always carries the explicit value; an assignment recorded before this decision has none and is
treated as disabled, because nothing configured a listener on that host. Turning the API on or
off for an installed Server is the same convergent install with a changed spec — no separate
Job or Workflow kind. Changing it restarts `dockerd`.

**A Docker Host Explorer, mediated by api-server.** For a Server where swallow installed Docker
CE with the API enabled, api-server exposes an opinionated REST surface under
`/api/v1/servers/{id}/docker/...` (summary, images, containers, volumes, networks) and calls the
host's Engine API itself. The browser never connects to a host. This is the explorer plane of
[ADR 032](032-self-deployed-platform-management.md) applied to one Server: same-origin, admin
authorized, behind the provider-owned contract, with Docker-native names rather than a generic
workload vocabulary.

**Eligibility comes only from swallow-owned facts.** The explorer is available when the Server
is deployed with a known primary address, its `docker-ce` assignment is `installed`, and the
assignment's `enableApi` is `true`. Swallow does not probe for, and does not use, a Docker API
the operator enabled by other means.

**Nothing Docker-owned is persisted.** Images, containers, volumes, and networks are facts the
Docker Engine owns; every read and write goes to the Engine at request time and stores nothing
in Mongo, consistent with ADR 001. Writes are synchronous calls, not Workflows. Swallow's only
Docker-related state remains the Software Assignment.

**Writes respect the Server Lock.** Pulling or removing images and creating, starting,
stopping, or removing containers, volumes, and networks change the host, so they are refused
on a locked Server ([ADR 013](013-server-lock-protection.md)); reads stay available.

**Security posture of this version.** The listener is unauthenticated plain HTTP bound to every
interface, acceptable only on internal networks. The dashboard states this risk where the
option is set and where the explorer asks to enable it, and recommends disabling it on Servers
reachable from the Internet.

**Anticipated evolution (not decided here).** If swallow later manages Docker Swarm, or Docker
across a whole cluster or fleet (for example an aggregate inventory or cross-Server views), that
feature may need swallow-owned Docker data in its database. That would revisit the
"nothing persisted" clause above and needs its own ADR. By ADR 038's boundary, a Swarm — a
runtime composed across hosts — would be modelled as a Platform rather than as Managed Software.

## Alternatives considered

- **Browser calls the host's Engine API directly:** rejected — the Engine API sends no CORS
  headers, an HTTPS dashboard cannot call a plain-HTTP host (mixed content), the operator's
  workstation often cannot route to datacenter hosts, and it would bypass swallow's
  authentication, authorization, and contract.
- **A transparent Docker API proxy (`/servers/{id}/docker/*` passthrough):** rejected for the same
  reason ADR 032 rejected a transparent Kubernetes proxy — it bypasses the contract workflow,
  the auth model, and the testing surface. A small opinionated surface covers the need.
- **Run each Docker action as an Ansible Workflow over SSH:** rejected — interactive reads would
  become durable operations taking seconds to minutes each, and the operator-visible Workflow
  history would fill with list calls.
- **Reach the local socket through an SSH tunnel from api-server:** deferred — it avoids an open
  TCP listener but adds per-request SSH session management; it is a candidate for the hardening
  slice together with TLS.
- **TLS / mutual TLS on the listener now:** deferred by the user for this version (internal
  datacenters); the option and its warning keep the exposure explicit until then.
- **A generic `/servers/{id}/containers` workload surface (planned-surface):** not adopted —
  `Workload` and `Container` are still pending glossary terms with an unverified state set;
  naming the surface after Docker keeps the provider's language, as the Kubernetes explorer does.
- **Mirror Docker objects into Mongo:** rejected — the Docker Engine owns them (ADR 001); a mirror
  would go stale and adds no value for a per-Server live view.
- **Detect an operator-enabled Docker API:** rejected for this version — eligibility must be
  derivable from swallow-owned intent, not from probing hosts.

## Consequences

- The `docker-ce` catalog entry lists `enableApi`; installs carry the trusted vars
  `swallow_docker_enable_api` and `swallow_docker_api_port`. The port is owned by api-server so
  the playbook and the explorer cannot disagree.
- A new Active contract `servers-docker.md` and glossary term Docker Host Explorer; the
  `software` feature in api-server owns the explorer next to the assignment it gates on.
- The dashboard Server detail gains a "Containers" tab for Servers with an installed Docker CE,
  and the install dialog gains the option with its risk notice.
- Enabling the API on an existing installation restarts `dockerd`; containers without a restart
  policy stop. The UI and docs say so.
- Hosts with the API enabled expose an unauthenticated Docker daemon to their network — root-
  equivalent access for anyone who can reach port 2375. This is accepted for internal networks
  only and is the main item for a future hardening slice.

## Current status

Implemented: the `enableApi` spec, the playbook listener and firewalld handling, the explorer
contract and api-server routes, and the dashboard Containers tab. TLS/authentication, Swarm,
and fleet-wide Docker views are future work.

## Related

- [ADR 001](001-system-ownership-boundaries.md) — integrate, don't rebuild; Docker objects are a
  live read.
- [ADR 013](013-server-lock-protection.md) — the lock that gates explorer writes.
- [ADR 032](032-self-deployed-platform-management.md) — the explorer plane pattern this follows.
- [ADR 038](038-software-deployment.md) — Docker CE as Managed Software and the Software Assignment.
- [`docs/development/software-deployment.md`](../development/software-deployment.md) — the
  Docker CE spec, trusted vars, and playbook behavior.
