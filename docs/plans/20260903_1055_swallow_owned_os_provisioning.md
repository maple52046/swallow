# Swallow-owned OS Provisioning and Network Configuration

## Purpose

Define the long-term ownership, contracts, operator workflows, and safety rules
for OS provisioning and Server network configuration in Swallow. Swallow owns
deployment intent and validation; provider adapters translate that intent into
provider-specific operations without leaking provider vocabulary into the
Dashboard or domain model.

## Source Scope

This consolidation covers one completed manuscript:

- `docs/plans/manuscripts/20260902-server-network-configuration.md`

It incorporates the original implementation plan and all completed follow-ups
through actionable provider network errors. The source describes the
cross-component behavior spanning the Go API service, MAAS adapter, MongoDB
coordination state, Active API contracts, and PatternFly Dashboard.

## Consolidated Background

OS deployment originally inherited MAAS network defaults and exposed
provider-specific concepts directly. That made Swallow behavior depend on MAAS
`AUTO`, obscured the distinction between physical link state and address
configuration, and left release-time IP cleanup outside a durable workflow.

The implementation moved intent ownership into Swallow, added typed network
inspection and mutation, and made deployment configure and verify networking
before asking the provisioner to deploy an image. Subsequent live validation
found and corrected several integration issues:

- The HTTP delivery layer initially dropped the batch `network` object, causing
  explicit Static requests to resolve to DHCP.
- Projection reads lagged live MAAS state, so deployment and release views now
  perform bounded targeted refreshes.
- Native browser select placeholders were unreadable in dark mode, so critical
  integration and image fields use a shared DOM-rendered PatternFly Select.
- MAAS network-operation 404 responses were incorrectly classified as missing
  Machines; mutation failures now retain the provider's actionable detail.
- A real deployment and release cycle on the designated `lab-compute-4` test VM
  confirmed Static assignment persistence and release cleanup convergence.

## Confirmed Decisions

- Swallow owns OS deployment network intent, defaults, validation, and workflow.
- Writable deployment modes are only `dhcp` and `static`; DHCP is the fallback
  when no explicit or inspection-derived Static intent exists.
- Existing MAAS `AUTO` is read as `provider_managed` and is never offered as new
  writable deployment intent.
- MAAS `LINK_UP` is read as `link_only`. Unlink is an action, not a mode.
- Physical link state and address-configuration state are separate concepts.
- Manual network writes require a present, unlocked Server in `ready` state.
- The boot NIC is the default interface. Operators can override interface and
  subnet per target; Static IP addresses are always per target.
- A single explicit Static link on the selected NIC becomes the deployment
  suggestion. Otherwise Swallow suggests DHCP.
- Deployment Templates may store mode, subnet, and default-route intent, but
  never NIC IDs or target-specific Static IP addresses.
- Default-route selection is valid only for Static intent and uses the gateway
  configured on the selected provider subnet.
- Release-time Static cleanup is optional and disabled by default.
- Durable Provisioning Tasks coordinate provider release cleanup and are not
  Ansible Operations.
- Provider validation details are preserved in per-target failures; a network
  mutation failure must not be mislabeled as a missing Server.
- UI terminology is `Ephemeral`; the compatible wire field remains
  `ephemeral`.

## Architecture and Design Principles

The Provisioning bounded context owns provider-neutral network types, use cases,
and narrow provider ports. MAAS terms and HTTP behavior stay inside the MAAS
infrastructure adapter.

A provider declares Network Configuration capability explicitly. Swallow rejects
unsupported intent before the first write rather than silently retaining provider
defaults. Dashboard code consumes only Active API contracts through application
ports.

Batch deployment has two boundaries:

1. Atomic preflight validates all target identities, lifecycle state,
   Integration ownership, image, network capability, NIC, subnet, address
   syntax, and within-batch Static uniqueness before any provider write.
2. Dispatch uses at most four workers. Each target configures and verifies its
   network before deployment and reports its own failure stage. Accepted peers
   are not rolled back when another target fails.

Release cleanup is persisted before provider release. A lease-based worker can
resume after service restart, waits for the Server to become Ready, compares the
saved Static-link snapshot with live state, removes only unchanged captured
links, and refreshes the Server projection.

Live provider reads own missing-Machine classification. Network mutation
responses retain the provider resource and validation message because an HTTP
status alone cannot safely identify which network resource failed.

## Functional Scope

### Server Network

- Read structured NICs, MAC addresses, boot-NIC flags, physical state,
  configuration state, raw provider mode, subnet links, IPs, gateways, and
  compatible subnets.
- Configure DHCP or Static addressing on a selected link.
- Attach a subnet as Link only.
- Unbind a selected link.
- Verify provider state after each mutation.
- Preserve unrelated NICs and subnet links; never use provider force options.

### Deploy OS

- Deploy one shared OS configuration to 1-100 Ready Servers from one
  provisioner Integration.
- Inspect target networks before configuration and suggest NIC, subnet, mode,
  existing Static address, and default-route intent.
- Allow per-target interface, subnet, and Static IP overrides.
- Validate and review final network assignments before dispatch.
- Return fully accepted batches to the scoped Servers list; keep partial failures
  in Results for diagnosis.
- Report each refusal with `network_configuration` or `deployment` stage.
- Refresh accepted targets until the live Server projection converges.

### Templates and Images

- Save reusable image, Ephemeral, cloud-init, and network intent in Deployment
  Templates while keeping cloud-init write-only.
- Resolve historical templates without network fields as DHCP.
- Read live deployable images, including MAAS uploaded custom images.
- Exclude PXE and bootloader artifacts.
- Refresh the provider-backed image catalog explicitly from the Dashboard.

### Release and Activity

- Offer an opt-in `Remove static IP bindings after release` action.
- Persist a cleanup task and pre-release Static-link snapshot before release.
- Wait for Ready and remove only links that still match the snapshot.
- Preserve DHCP, provider-managed, Link-only, and post-release changes.
- Show task status, phase, request correlation, full failure detail, and
  cleanup-only retry in Server Activity.
- Refresh provisioning state, addresses, and Ephemeral state without requiring a
  page reload.

## Constraints and Rules

- Existing `/api/v1` behavior remains backward compatible.
- The legacy single-Server deploy endpoint keeps omission behavior; the Dashboard
  uses the batch workflow and sends explicit intent.
- Static addresses must be valid IPv4 addresses within the selected subnet and
  unique within the submitted batch.
- DHCP cannot carry default-route intent.
- Provider network configuration support is mandatory for the new batch
  workflow; unsupported providers fail before mutation.
- Missing metrics or provider observations are displayed as unavailable, not as
  fabricated values.
- Cloud-init never appears in read APIs, browser storage, URLs, logs, toast
  details, or provider error text.
- Provider errors may include sanitized actionable detail but never credentials
  or request headers.
- A failed cleanup retry repeats cleanup only and never repeats release.
- Continuous reconciliation of deployed-host networking is prohibited.
- Destructive live network, deployment, release, or unbind testing requires an
  explicitly designated test Server.

## Data Model and Format Notes

Provider-neutral configuration states are:

- `dhcp`
- `static`
- `link_only`
- `unconfigured`
- `provider_managed`
- `unknown`

Deployment Template network data contains `mode`, optional `subnetId`, and
`defaultGateway`. It excludes interface IDs and Static addresses. Existing
MongoDB records require no migration because absent network fields resolve to
DHCP.

Provisioning Tasks store Server ownership, status, phase, attempt, error,
request correlation, timestamps, leases, and an internal Static-link snapshot.
Public responses never expose the cleanup snapshot.

Task statuses are `pending`, `running`, `succeeded`, and `failed`. Cleanup
phases are `waiting_for_release`, `waiting_for_ready`, `cleaning_network`,
and `complete`.

Subnet presentation uses a provider name only when it differs from its CIDR:
`management (192.168.100.0/24)`; an identical name and CIDR render only once.

## CLI / API / Config Notes

The Active API surface includes:

- `GET /api/v1/servers/{id}/network`
- `POST /api/v1/servers/{id}/network/interfaces/{interfaceId}/links`
- `PUT /api/v1/servers/{id}/network/interfaces/{interfaceId}/links/{linkId}`
- `DELETE /api/v1/servers/{id}/network/interfaces/{interfaceId}/links/{linkId}`
- `POST /api/v1/provisioning/networks/inspect`
- `POST /api/v1/provisioning/deployments/preflight`
- `POST /api/v1/provisioning/deployments`
- `GET /api/v1/provisioning/tasks/{id}`
- `POST /api/v1/provisioning/tasks/{id}/retry`
- `GET /api/v1/servers/{serverId}/provisioning-tasks`

Batch network input contains shared `mode`, optional `subnetId`,
`defaultGateway`, and per-target assignments. Per-target deployment failures
contain `serverId`, `code`, `message`, and `stage`.

Release accepts `unbindStaticIPs`; an accepted response may include `taskId`.
No new standalone IPAM configuration or external provider credentials are added.

The existing dev installation is managed with
`deploy/dev/docker compose up -d --build`. Dashboard and API verification use
the established ports and configuration rather than ad hoc development servers.

## Implementation Plan

All planned implementation phases are complete:

1. Added domain types, provider capability, typed ports, and MAAS translation.
2. Added structured network reads and Ready-only link mutation APIs.
3. Added bounded network inspection, atomic batch preflight, explicit DHCP and
   Static dispatch, and per-target failure stages.
4. Extended templates with backward-compatible network intent.
5. Added Mongo-backed cleanup tasks, worker leases, restart recovery, exact
   snapshot comparison, retry, and projection refresh.
6. Rebuilt the Dashboard Network, Deploy OS, Template, Release, and Activity
   workflows with PatternFly.
7. Added live image refresh, inspection-derived defaults, DOM-based selects,
   compact subnet rendering, and explicit default-route language.
8. Corrected delivery mapping, live projection polling, navigation convergence,
   Ephemeral terminology, and provider network error classification.
9. Added backend unit/integration coverage and Playwright interaction coverage.
10. Completed Go test, vet, build, Dashboard lint/build/E2E, dev compose rebuild,
    admin-login smoke tests, and authorized live deployment/release validation.

Future changes must extend the same provider-neutral contracts and preserve these
safety and compatibility guarantees.

## Non-goals

- VLAN, bond, bridge, MTU, DNS, route-table, or independent IPAM lifecycle
  management.
- Continuous mutation or drift correction of networking after deployment.
- Provider-specific `AUTO`, force, or keep-current deployment options.
- Automatic rollback after a provider has accepted deployment work.
- Persisting live OS image catalogs in Swallow.
- Treating provisioning cleanup as an Ansible job.
- Storing NIC IDs or target Static IPs in reusable templates.

## Open Questions

No unresolved decision remains from the source manuscript. Supporting additional
provisioners will require explicit capability and translation decisions, but no
provider beyond the implemented MAAS adapter is currently specified.

## Future Work

Potential later scope, requiring new contracts and explicit decisions, includes:

- Provider-neutral VLAN, bond, bridge, DNS, route, and MTU management.
- A standalone IPAM lifecycle or integration with an external IPAM authority.
- Provider-side Static IP availability inspection before link replacement when
  the provider offers a reliable read-only allocation contract.
- Additional OS provisioner adapters implementing the same Swallow-owned intent.
- Richer durable deployment jobs if per-Server provisioning axes and provider
  events no longer provide sufficient operational history.
