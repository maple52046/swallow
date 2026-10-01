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
  "credentialSource": "site",
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

Both fields are optional; the request replaces the whole site credential. A non-empty
`sshPrivateKey` must parse as an unencrypted private key (`400 validation_error`
otherwise) and overrides the installation's Deployment Key for this Site. Omitting it (for
example sending only `becomePassword`) clears the override, so the Site uses the
Deployment Key (see [ssh-keys.md](ssh-keys.md)). The become password is always
site-scoped.

Success is `204 No Content`. No API returns either secret; configuration reads expose
only `hasCredential` (a site credential record exists) and `credentialSource`, the key
automation uses for this Site:

- `site` — the site `sshPrivateKey` override.
- `deploymentKey` — no override; the Deployment Key is used.
- `none` — no override and no Deployment Key exists (the installation step that creates it has not run).

## Errors

The common error envelope applies. Unknown sites/configurations return `not_found`;
invalid ports, missing required fields, and unregistered playbooks return
`validation_error`.

## Compatibility Notes

[Decision 039](../../../../../docs/decisions/039-ssh-key-management-and-default-user.md)
made `sshUser` and `sshPrivateKey` optional and added `credentialSource`; both are
backward compatible (existing Sites keep their user and key, which still take effect).
New schema-v3 Operations
consume it through the standalone Ansible executor; legacy schema-v2 Operations may still
consume it through the compatibility dispatcher while that queue drains.
