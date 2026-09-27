# Swallow Product, Platform, and Documentation Roadmap

## 1. Purpose

Consolidate the product, platform, operator, and documentation work that shaped
the current swallow repository into one durable reference. This plan records
the intended behavior and boundaries for OS Image display-name propagation, the
operator CLI, Rocky Linux k0s support, Managed Software, and the bilingual
public documentation system.

The common goal is a system that operators can evaluate and drive through
stable Dashboard, CLI, and HTTP API entry points while contributors and agents
retain precise, single-source development contracts.

## 2. Source Scope

This consolidation incorporates the following manuscripts:

- `docs/plans/manuscripts/20260922-osimage-rename-propagation.md`
- `docs/plans/manuscripts/20260923-operator-cli.md`
- `docs/plans/manuscripts/20260923-rocky-k0s-and-cli-test.md`
- `docs/plans/manuscripts/20260924-software-deployment.md`
- `docs/plans/manuscripts/20260926-documentation-overhaul.md`

Together they cover the active `api-server`, `dashboard`, and `cli` components,
shared automation and deployment assets, public documentation, and the
development documents that govern cross-component contracts.

## 3. Consolidated Background

swallow owns orchestration and operator-facing projections around external
infrastructure systems. MAAS remains authoritative for machines and
provisioning actions; Temporal persists Workflow execution; Ansible performs
host automation; Prometheus, Alertmanager, and Grafana provide monitoring; and
MongoDB stores swallow-owned intent, projections, and credentials.

Several product gaps were addressed or identified as the repository expanded:

- OS Image display names were mirrored onto Server projections but could lag an
  overlay rename, while a transient Server-list refresh failure could discard a
  usable working set.
- The Dashboard was the only first-class HTTP API consumer. A separate operator
  CLI was needed to cover the active API without importing provider internals.
- k0s automation needed a single OS-conditional path for Ubuntu and Rocky Linux,
  including SELinux, firewalld, containerd, and deterministic Kubernetes node
  naming.
- Installing one host software package needed a domain boundary distinct from
  deploying a composed Kubernetes or Slurm Platform.
- Repository documentation primarily served agents and contributors. End users,
  evaluators, operators, integrators, and external contributors needed stable,
  bilingual product documentation.

The resulting direction keeps provider-owned API contracts and agent-oriented
development specifications authoritative, while public documentation explains
supported tasks and links into those sources when deeper detail is required.

## 4. Confirmed Decisions

### OS Image display-name propagation

- Keep the ADR 025 mirrored-name model; do not add catalog fan-out to Server
  list or detail reads.
- After an OS Image overlay set or clear, best-effort refresh affected Server
  projections using one catalog read plus overlay data, without polling MAAS
  machines.
- A propagation failure must not fail the overlay write. The normal reconcile
  loop remains the eventual repair path.
- Server SSE fingerprints include `DeployedImageName`, so name-only changes are
  observable.
- Dashboard background refresh failures retain last-good Server rows and allow
  SSE updates. A full-page error is reserved for an initial load with no data.

### Operator CLI and binaries

- `cli/` is a top-level component and a peer HTTP consumer to `dashboard/`.
- The operator binary is `swallow`; the API service binary is `swallow-api`.
  Role-based directory names, API subcommands, environment variables, install
  directories, and the `swallow-api.service` name remain unchanged.
- The CLI is a separate Go module using Cobra and Clean Architecture. It is a
  conformist consumer and must not import `api-server` internals.
- Complex request bodies are accepted as operator-owned JSON or YAML files so
  they continue to follow provider contracts.
- Only Active API groups are exposed. Planned endpoints and deprecated aliases
  are not presented as supported commands.

### Rocky Linux and k0s

- Ubuntu and Rocky Linux use one Ansible flow with OS-family conditions; the
  existing Debian behavior remains intact.
- Rocky hosts run with SELinux Enforcing, nftables-backed firewalld, persisted
  kernel modules/sysctls, and the required containerd SELinux configuration.
- k0s worker and workload-controller installs set kubelet
  `--hostname-override` to the swallow Server name. This prevents Rocky FQDNs
  from diverging from membership and readiness lookups.
- End-to-end platform validation is performed through the `swallow` CLI rather
  than bypassing product interfaces.

### Managed Software

- Managed Software is not a Platform aggregate. It installs one software kind
  and its variants on already-deployed Servers; a Platform is a runtime composed
  from multiple components.
- A durable Software Assignment keyed by `(serverId, kind)` is the source of
  truth for desired and last-applied software state. It is not a fourth Server
  status axis.
- Platform and software reuse is expressed by composing Jobs and shared roles,
  not by nesting an entire software Workflow or relying only on Ansible role
  inclusion.
- Docker CE and Podman are mutually exclusive and are rejected on Kubernetes
  Platform members. NFS may coexist with a Platform.
- Leaving the `deployed` provisioning state marks assignments `absent` so disk
  state is not overstated.
- The first implementation does not refactor Slurm's embedded NFS behavior.

### Documentation system

- GitHub Markdown is the publication format; no separate documentation site is
  introduced.
- English is the default public language. Traditional Chinese public documents
  provide equivalent content and mirrored navigation.
- Cross-project guides live in matching `docs/en/` and `docs/zh-TW/` trees.
  Colocated public references use `README.md` and `README.zh-TW.md`, or an
  equivalent `.zh-TW.md` companion.
- Development documents, API contracts, glossaries, ADRs, AGENTS files, and
  skills remain single-source and are corrected rather than translated.
- Public documentation may link to development sources of truth. Development
  documentation must not link back to the public user-document trees.
- swallow is described as active development. Compose assets can be documented;
  native installation remains preview/incomplete until Temporal packaging is
  complete.

## 5. Architecture and Design Principles

- Preserve component boundaries. `api-server` owns HTTP contracts and provider
  behavior; `dashboard` and `cli` consume those contracts without reaching into
  provider internals.
- Use Clean Architecture within every component and provider-owned contracts for
  cross-component communication.
- Keep external-system ownership explicit. swallow stores intent and projections
  but does not replace MAAS, Temporal, Ansible, Kubernetes, Slurm, Prometheus,
  Alertmanager, Grafana, or MongoDB ownership.
- Prefer convergent Workflows and explicit Jobs for orchestration. Shared
  automation roles do not erase the domain distinction between Platform and
  Managed Software.
- Treat `Workflow` as the canonical product term. Use `Operation` only for code
  compatibility or historical contexts.
- Treat `Platform` only as a managed Kubernetes or Slurm runtime.
- Servers are produced and reconciled by provisioner integrations; documentation
  must not imply a separate swallow Server-create flow.
- Keep denormalized projections responsive with targeted best-effort updates and
  reconcile-based eventual repair.
- Preserve last-good UI data during recoverable background failures.
- Keep detailed command, configuration, installation, and schema references at
  their owning locations; central guides explain tasks instead of duplicating
  complete reference tables.

## 6. Functional Scope

### Product and operator surface

- Authentication, Overview, Sites, integrations, automation, and credentials.
- Server inventory, watch/refresh, tags, locks, power and provider actions,
  provisioning detail/tasks/events, network and placement data, Zones, and
  Pools.
- OS Images, overlays, uploads, verification, templates, user data, tags,
  preflight, networks, deployment targets, deploy, release, and recovery.
- Kubernetes and Slurm Platform deploy, update, uninstall, delete, sync, and
  exploration.
- Managed Software catalog, assignments, install, and uninstall for Docker CE,
  Podman, and NFS server/client variants.
- Workflow list/detail/create, cancellation, rerun, timeline, Task retry, logs,
  stderr, events, and artifacts.
- Prometheus discovery and metrics plus Alertmanager alert acknowledgement.

### k0s Rocky Linux automation

- Install RHEL-family prerequisites including `iproute`, `iptables-nft`,
  `firewalld`, and worker SELinux utilities.
- Persist networking modules and bridge/forward sysctls.
- Assert SELinux Enforcing; enable containerd SELinux support; label and restore
  CNI and extracted k0s paths.
- Configure firewalld for controller/worker ports, pod/service CIDRs, VRRP, and
  worker masquerading with one reload.
- Apply deterministic short-name hostname overrides on workers and workload
  controllers.

### Public documentation

- Product landing pages and language selector.
- Introduction, getting started, installation choices, core concepts,
  troubleshooting, and compatibility/known limitations.
- Task guides for initial setup, Servers/infrastructure, OS provisioning,
  Kubernetes/Slurm Platforms, Managed Software, Workflows, and monitoring.
- API integration, configuration, CLI navigation, component, deployment, and
  contribution references.
- Deterministic documentation screenshots that avoid exposing BMC or other
  credentials.

## 7. Constraints and Rules

- Do not change runtime API schemas, CLI command semantics, config precedence,
  or product behavior solely to simplify documentation.
- Describe only working-tree Active contracts and automation manifests; do not
  promote Planned surfaces as implemented.
- Do not perform per-request OS Image catalog fan-out or replace the mirrored
  display-name storage model.
- Do not adopt a new Dashboard data library merely for Server refresh recovery.
- The standalone Managed Software entry requires deployed Servers.
- Clients cannot supply trusted `swallow_`-prefixed automation variables.
- Software playbook names are launcher-controlled rather than site-configurable
  mappings.
- Do not model Managed Software as a Platform or define the boundary by whether
  a flow includes `ensure-os`.
- The first Managed Software slice does not provide continuous host-package
  reconciliation or air-gapped package repositories.
- Keep development-to-public links forbidden to protect agent context windows;
  plain path descriptions are allowed where classification must be documented.
- Examples and screenshots must use local-only or redacted credentials and must
  never contain real secrets.

## 8. Data Model and Format Notes

### Server OS Image projection

`Server.Provisioning.DeployedImageName` is a denormalized display projection.
Its effective value is resolved by OS system, distro series, primary
architecture, and deployed state using catalog plus overlay data. Targeted
refreshes upsert only changed Servers; normal integration reconciliation remains
the fallback.

### Software Assignment

The `software_assignments` collection uses `(serverId, kind)` as its key.

| Field | Meaning |
| --- | --- |
| `kind` | `docker-ce`, `podman`, or `nfs` |
| `roles` | NFS `server` and/or `client`; empty for container runtimes |
| `spec` | Kind-specific desired version, export, client, or mount settings |
| `state` | `pending`, `installed`, `failed`, `uninstalling`, or `absent` |
| `lastWorkflowId` | Workflow that most recently changed the record |
| `lastAppliedAt` | Last successful ensure time |

The assignment represents swallow-owned intent and last-applied state, not
continuous observation of host packages.

### Documentation format

- Public documents use GitHub-flavored Markdown.
- English slugs are stable. Mirrored Traditional Chinese pages use the same
  relative path, while colocated translations use `.zh-TW.md`.
- Root and component landing pages stay concise and route readers to task guides
  or authoritative references.
- Mermaid may express the shared system relationship diagram; deterministic
  Playwright fixtures own canonical product screenshots.

## 9. CLI / API / Config Notes

### CLI

The `swallow` CLI resolves configuration in file, `SWALLOW_*` environment, then
flag order. Profiles are stored with owner-only permissions, and login persists
the token. The HTTP adapter owns `/api/v1` base-path handling, bearer/machine/no
authentication, shared error envelopes, SSE framing, and multipart upload.
Output supports table, JSON, and YAML over opaque decoded contract data.

Active command groups are `auth`, `overview`, `sites`, `integrations`, `servers`,
`provisioning`, `infrastructure`, `platforms`, `workflows`, `monitoring`, and
`discovery`. The complete command reference remains in `cli/docs/usage.md` with
its Traditional Chinese counterpart.

### API

The Managed Software provider surface is:

- `GET /api/v1/software/catalog`
- `GET /api/v1/software/assignments?serverId=&kind=`
- `POST /api/v1/software/assignments`
- `POST /api/v1/software/uninstall`

Standalone software Workflows use `configure-docker-ce`,
`uninstall-docker-ce`, `configure-podman`, `uninstall-podman`, `configure-nfs`,
and `uninstall-nfs`. Their core Jobs prepare hosts, execute the kind-specific
Ansible playbook, and record or clear the assignment. Failed install steps mark
the assignment failed through the step observer.

Public API guides explain authentication, pagination, errors, representative
curl requests, and how to locate active provider contracts. Request and response
schemas remain owned by those contracts.

### Configuration

- The normal reconciliation interval remains unchanged; targeted OS Image name
  refreshes supplement rather than replace it.
- k0s automation uses trusted variables and OS facts to select Debian or
  RHEL-family work.
- Managed Software trusted variables include `swallow_software_kind`, a
  server-to-role mapping, and kind-specific `swallow_nfs_*`,
  `swallow_docker_*`, or `swallow_podman_*` values.
- Documentation examples must match config source, example config, Compose
  environment values, and the current release assets.

## 10. Implementation Plan

1. Keep OS Image effective-name resolution reusable for both Machine-driven
   reconciliation and Server-projection refresh. Invoke the refresh best-effort
   after overlay writes, include the name in SSE fingerprints, and preserve
   Dashboard working sets across background failures.
2. Maintain `cli/` as a separate Go/Cobra HTTP consumer, keep `swallow-api` as
   the backend binary, and ensure every Active API group has an operator command
   or explicit task guide.
3. Maintain one k0s automation path with tested Debian and RHEL branches. Verify
   Rocky prerequisites, SELinux/firewalld behavior, hostname overrides, Platform
   activation, Kubernetes health, and membership sync through the CLI.
4. Maintain Managed Software as a separate bounded context with durable
   assignments, catalog/install/uninstall APIs, Workflow launchers, Ansible
   roles, compatibility checks, cleanup on provisioning transitions, and
   Dashboard/CLI consumers.
5. Keep the bilingual public documentation architecture complete: root landing
   pages, mirrored language trees, colocated references, and contributor guides.
   Keep development sources single-language and accurate.
6. Run the repository documentation checker in CI for local links, language-tree
   parity, required translation companions, forbidden reverse links, and
   screenshot synchronization.
7. Validate changes with component Go formatting/build/vet/tests, Dashboard
   lint/build and deterministic Playwright fixtures, CLI build/tests, Compose
   config checks, and release validation.

## 11. Non-goals

- Replace Server OS Image projections with live catalog joins or change the
  reconcile interval.
- Adopt react-query solely for Server-list recovery.
- Expose Planned or deprecated API surfaces through the CLI or public claims.
- Support mixed per-host SSH users in one k0s deployment without a contract
  change.
- Add cgroup v1 fallback, cri-dockerd, or an RHEL DNS NetworkManager fallback as
  part of the validated Rocky work.
- Model Managed Software as a Platform, refactor Slurm NFS in the first slice,
  add PostgreSQL/LDAP kinds, or provide continuous package reconciliation.
- Introduce MkDocs, Docusaurus, or another documentation-site generator.
- Translate ADRs, glossaries, API contracts, architecture specifications,
  AGENTS files, or skills.
- Add LICENSE, SECURITY, SUPPORT, or CODE_OF_CONDUCT policy files without the
  required legal, ownership, and contact decisions.
- Claim production readiness or formal support commitments.

## 12. Open Questions

- Should the CLI later add a no-echo interactive password prompt through
  `golang.org/x/term`, while retaining `--password` and `--password-stdin` for
  automation?
- What contract should represent per-host SSH users if a future Platform mixes
  operating-system families in one cluster?
- When should generic NFS Jobs replace Slurm's specialized embedded NFS path,
  and what migration guarantees are required for existing Slurm deployments?
- What release milestone completes Temporal packaging sufficiently to move the
  native installation path beyond preview?

## 13. Future Work

- Extract generic `configure-nfs` Jobs and roles for Platform composition while
  retaining Slurm-specific HA state, mount guards, and controller-only export
  behavior. Until then, document generic NFS and Slurm managed-state NFS as
  distinct implementations.
- Consider additional Managed Software kinds such as PostgreSQL or LDAP clients
  only after the assignment and compatibility contracts prove stable.
- Define a cross-OS, per-host SSH identity contract before claiming mixed Ubuntu
  and Rocky membership in one Platform.
- Add an optional secure interactive CLI password prompt if dependency and UX
  tradeoffs justify it.
- Continue extending public task guides and deterministic screenshots as new
  Active Dashboard routes, CLI groups, and provider contracts are introduced.
