# 009. Deployment templates own intent, not automation

- Status: Accepted
- Date: 2026-08-28
- 現行規格：[os-provisioning.md](../development/os-provisioning.md) §3 Deployment Template

## Context

Operators need to apply one OS deployment configuration to several Servers and
reuse it when the same provisioner fleet scales out. The retired
`ProvisioningProfile` combined an OS image with packages and scripts, which made
Swallow an owner of mutable automation content and crossed the boundary recorded
by ADR 001 and ADR 006.

OS Images and deployment execution remain owned by the provisioner. No external
system, however, owns the operator's reusable selection of an image, ephemeral
mode, and cloud-init for a named Swallow Integration.

## Decision

Swallow owns an integration-scoped `DeploymentTemplate` as reusable deployment
intent. It may contain an OS Image reference, the deploy mode, provider-neutral
network intent, and encrypted write-only cloud-init. It must not contain a
provider NIC identity, target-specific static IP, packages, scripts, playbooks,
or an execution lifecycle.

A multi-Server deployment stores no durable Swallow job of its own: Swallow
validates the batch and submits the same resolved request to the owning
provisioner, and progress is observed independently on each Server provisioning
axis. (Clients now submit OS deployments as a durable `deploy-os` Workflow, and
this direct batch remains as a deprecated endpoint; see the current spec. The
template owns no execution lifecycle on either path.)

OS Images are read live from each Integration and are never mirrored into the
template collection. Creating, changing, or using a template validates its image
against the current provider catalog.

One narrow exception is the mirrored display name of a Server's currently
deployed image. The reconciler already reads each Integration's catalog once per
pass, so it resolves each deployed machine's image to its effective name (the
provider title overlaid with any Swallow custom name per
[ADR 025](025-provider-data-overlay.md)) and mirrors that single string onto the
Server provisioning axis with an `observedAt`. This is the ADR 001 mirror
mechanism, and it exists so the fleet Server list can show a meaningful image
name without a per-request fan-out. The catalog itself remains live-read and
un-mirrored.

## Alternatives considered

- Restore `ProvisioningProfile`: rejected because mutable automation content
  belongs to signed release playbooks, not MongoDB.
- Persist a deployment run: rejected because the provider owns execution and the
  Server provisioning axis already carries progress.
- Make templates fleet-wide: rejected because image identifiers and capabilities
  are specific to one provisioner Integration.
- Store cloud-init as editable plaintext: rejected because user data commonly
  contains credentials and bootstrap tokens.

## Consequences

Templates are authoritative Swallow data, while their image references can become
temporarily unavailable when provider catalogs change. The UI must show that
condition without copying image metadata into the template.

Encrypted cloud-init can be inherited, replaced, or omitted for one deployment,
but never read back. Provider calls can partially succeed after preflight and
cannot be rolled back safely.
