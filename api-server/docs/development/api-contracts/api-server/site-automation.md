# Site Automation Configuration

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)
- Swallow installation tooling

## Purpose

Manage the Swallow-owned Ansible configuration for a site without modelling Ansible as
an external integration. Secret material is write-only.

## Related Glossary Terms

- [Automation Configuration](../../../../../docs/development/glossaries/terms/automation-configuration.md)
- [Operation](../../../../../docs/development/glossaries/terms/operation.md)
- [Deployment Key](../../../../../docs/development/glossaries/terms/deployment-key.md)

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
exist in the release manifest. Enabled configurations require known-hosts content;
host-key verification cannot be disabled.

`sshUser` is optional. It is a fallback login user: for each host, automation first uses
the effective default user of the OS Image the Server is deployed with
(`provisioning.deployedImageDefaultUser`, see [provisioning.md](provisioning.md)) and uses
it exclusively. Only a Server without one tries `sshUser` (when set) and then the built-in
candidates `cloud-user` and `ubuntu`, using the first that authenticates.

The `install-exporters` / `uninstall-exporters` mappings are what let swallow install the
Prometheus exporter containers (node-exporter on every host, the RDC exporter on
`amd-gpu` hosts) and remove them again when a host is handed to a Kubernetes exporter
owner. `install-exporters` is also the playbook swallow auto-runs when a server
reaches the `deployed` provisioning state.

The `deploy-k8s-exporters` / `remove-k8s-exporters` mappings are the Kubernetes side of
exporter ownership: they apply or delete the exporter DaemonSets on a platform (run on a
control-plane target). Switching a platform's `exporterOwner` to `k8s` uninstalls the
members' Ansible exporters and deploys the DaemonSets; switching back removes them.

`uninstall-kubernetes` is selected explicitly by the Platform uninstall use case from the
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
  "credentialSource": "deploymentKey",
  "createdAt": "2026-08-25T00:00:00Z",
  "updatedAt": "2026-08-25T00:00:00Z"
}
```

`credentialSource` is the key automation uses for this Site. Every Site uses the
installation's Deployment Key ([decision 041](../../../../../docs/decisions/041-deployment-key-only-automation.md)):

- `deploymentKey` — the Deployment Key exists and is used.
- `none` — no Deployment Key exists (the installation step that creates it has not run).

`hasCredential` reports that the Site stores a become password.

Ansible Tasks for a Site run only when `enabled` is `true` and `credentialSource` is
`deploymentKey`; otherwise acceptance fails with `503 provider_unavailable` and a running
Task fails with `automation_configuration_unavailable`.

## Credential Request

```json
{
  "becomePassword": "optional"
}
```

The request replaces the Site's whole credential, which holds only the write-only become
password; sending `{}` clears it. A request that contains `sshPrivateKey` (with any value)
is `400 validation_error`: automation always logs in with the Deployment Key, so replace
that key with `PUT /api/v1/ssh-keys/deployment` instead (see [ssh-keys.md](ssh-keys.md)).

Success is `204 No Content`. No API returns the secret.

## Errors

The common error envelope applies. Unknown sites/configurations return `not_found`;
invalid ports, missing required fields, unregistered playbooks, and a credential request
carrying `sshPrivateKey` return `validation_error`.

## Compatibility Notes

[Decision 039](../../../../../docs/decisions/039-ssh-key-management-and-default-user.md)
made `sshUser` optional and added `credentialSource`.
[Decision 041](../../../../../docs/decisions/041-deployment-key-only-automation.md) removed
the Site private-key override (breaking): `sshPrivateKey` is refused, `credentialSource`
no longer reports `site`, and a key stored by an earlier release is ignored. New schema-v3
Operations consume this configuration through the standalone Ansible executor; legacy
schema-v2 Operations may still consume it through the compatibility dispatcher while that
queue drains.
