# Site Automation Configuration

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- Swallow installation tooling

## Purpose

Manage the single embedded Ansible configuration for a site without modelling Ansible as
an external integration. Secret material is write-only.

## Related Glossary Terms

- [Automation Configuration](../../../../../docs/development/glossaries/terms/automation-configuration.md)
- [Operation](../../../../../docs/development/glossaries/terms/operation.md)

## Endpoints

```text
GET /api/v1/sites/{siteId}/automation
PUT /api/v1/sites/{siteId}/automation
PUT /api/v1/sites/{siteId}/automation/credential
```

All endpoints require an admin JWT according to [conventions](conventions.md).

## Configuration Request

```json
{
  "enabled": true,
  "sshUser": "ubuntu",
  "sshPort": 22,
  "knownHosts": "host ssh-ed25519 AAAA...",
  "playbookMappings": {
    "install-gpu-driver": "install-gpu-driver",
    "install-exporters": "install-exporters",
    "uninstall-exporters": "uninstall-exporters"
  }
}
```

Every mapped operation kind must be a built-in non-custom kind. Every playbook value must
exist in the release manifest. Enabled configurations require a non-empty SSH user and
known-hosts content; host-key verification cannot be disabled.

The `install-exporters` / `uninstall-exporters` mappings are what let swallow install the
Prometheus exporter containers (node-exporter on every host, the RDC exporter on
`amd-gpu` hosts) and remove them again when a host is handed to a Kubernetes exporter
owner. `install-exporters` is also the playbook the platform auto-runs when a server
reaches the `deployed` provisioning state.

The `deploy-k8s-exporters` / `remove-k8s-exporters` mappings are the Kubernetes side of
exporter ownership: they apply or delete the exporter DaemonSets on a cluster (run on a
control-plane target). Switching a cluster's `exporterOwner` to `k8s` uninstalls the
members' Ansible exporters and deploys the DaemonSets; switching back removes them.

`uninstall-kubernetes` is selected explicitly by the Cluster uninstall use case from the
release manifest. It needs no Site mapping, so existing automation configurations remain
valid.

## Configuration Response

```json
{
  "siteId": "site-id",
  "enabled": true,
  "sshUser": "ubuntu",
  "sshPort": 22,
  "knownHosts": "host ssh-ed25519 AAAA...",
  "playbookMappings": {},
  "hasCredential": true,
  "createdAt": "2026-08-25T00:00:00Z",
  "updatedAt": "2026-08-25T00:00:00Z"
}
```

## Credential Request

```json
{
  "sshPrivateKey": "-----BEGIN OPENSSH PRIVATE KEY-----...",
  "becomePassword": "optional"
}
```

Success is `204 No Content`. No API returns either secret; configuration reads expose
only `hasCredential`.

## Errors

The common error envelope applies. Unknown sites/configurations return `not_found`;
invalid ports, missing required fields, and unregistered playbooks return
`validation_error`.

## Compatibility Notes

This is a breaking replacement for the former external automation-integration model.
Development and testing databases are recreated; no dual-schema migration is provided.
