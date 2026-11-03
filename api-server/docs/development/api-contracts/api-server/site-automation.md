# Site Automation Configuration

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` command-line client)
- swallow installation tooling

## Purpose

Manage the swallow-owned Ansible configuration for a site without modelling Ansible as
an external integration. Secret material is write-only.

## Related Glossary Terms

- [Automation Configuration](../../../../../docs/development/glossaries/terms/automation-configuration.md)
- [Operation](../../../../../docs/development/glossaries/terms/operation.md)
- [Deployment Key](../../../../../docs/development/glossaries/terms/deployment-key.md)

## Endpoints

```text
GET /api/v1/sites/{id}/automation
PUT /api/v1/sites/{id}/automation
PUT /api/v1/sites/{id}/automation/credential
```

`{id}` is the Site's `siteId`. All endpoints require role `admin` according to
[conventions](conventions.md).

## Configuration Request

```json
{
  "enabled": true,
  "sshUser": "ubuntu",
  "sshPort": 22,
  "knownHosts": "host ssh-ed25519 AAAA...",
  "playbookMappings": {
    "deploy-kubernetes": "deploy-kubernetes"
  }
}
```

Every mapped operation kind must be a built-in non-custom kind. Every playbook value must
exist in the release manifest. Enabled configurations require known-hosts content;
host-key verification cannot be disabled.

`PUT` creates the configuration or replaces every non-secret field, so an omitted field takes
its zero value: `enabled` `false`, `sshUser` and `knownHosts` `""`, no `playbookMappings`.
`sshPort` is `22` when omitted or `0` and must otherwise be between 1 and 65535. `sshUser` and
`knownHosts` are trimmed. The become password is not touched.

`sshUser` is optional. It is a fallback login user: for each host, automation first uses
the effective default user of the OS Image the Server is deployed with
(`provisioning.deployedImageDefaultUser`, see [provisioning.md](provisioning.md)) and uses
it exclusively. Only a Server without one tries `sshUser` (when set) and then the built-in
candidates `cloud-user` and `ubuntu`, using the first that authenticates.

`install-exporters`, `uninstall-exporters`, `deploy-k8s-exporters`, and `remove-k8s-exporters` are
not accepted as mapped kinds (`400 validation_error`). The host exporter is the Node Exporter,
installed as Managed Software ([software.md](software.md#node-exporter)), and like every software
kind its playbooks are named by the release, not by a Site mapping.

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
installation's Deployment Key ([decision 039](../../../../../docs/decisions/039-ssh-key-management-and-default-user.md) (ssh key management and default user)):

- `deploymentKey` — the Deployment Key exists and is used.
- `none` — no Deployment Key exists (the installation step that creates it has not run).

`hasCredential` reports that the Site stores a become password; it is `false` again once the
password is cleared with `{}`.

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
password; sending `{}` clears it. A request whose `sshPrivateKey` is present and not `null`
is `400 validation_error`: automation always logs in with the Deployment Key, so replace
that key with `PUT /api/v1/ssh-keys/deployment` instead (see [ssh-keys.md](ssh-keys.md)).
`"sshPrivateKey": null` is ignored. A Site without a configuration (create it with `PUT
.../automation` first) is `404 not_found`.

Success is `204 No Content`. No API returns the secret.

## Errors

The common error envelope applies. `GET .../automation` and `PUT .../automation/credential`
answer `not_found` for a Site without a configuration, which includes a Site that does not
exist, and `PUT .../automation` answers `not_found` for a Site that does not exist. Invalid ports, `enabled`
without `knownHosts`, unknown, retired, or `custom` mapped kinds, unregistered playbooks, and a
credential request carrying a non-null `sshPrivateKey` return `validation_error`.

## Compatibility Notes

Decision 039
made `sshUser` optional and added `credentialSource`.
Decision 039 removed
the Site private-key override (breaking): `sshPrivateKey` is refused, `credentialSource`
no longer reports `site`, and a key stored by an earlier release is ignored. Workflows consume
this configuration through the standalone Ansible executor.

Since 2026-10-31 clearing the become password with `{}` removes the stored credential, so
`hasCredential` reads `false`; it stayed `true` before.

Since 2026-10-31 `PUT .../automation` for a Site that does not exist is `404 not_found`;
it was `500 internal_error`.

Since 2026-11-03 (breaking) the `install-exporters` and `uninstall-exporters` mappings are refused
with `400 validation_error`: the host exporter is the Node Exporter's Managed Software. The
schema v11 migration removes both keys from every stored configuration, so a configuration that
had them reads and saves without them.

Since 2026-11-03 (breaking) the `deploy-k8s-exporters` and `remove-k8s-exporters` mappings are
refused with `400 validation_error`: the Kubernetes exporter DaemonSets they applied were retired,
because OS provisioning installs the Node Exporter on every Server. The schema v12 migration removes
both keys from every stored configuration, so a configuration that had them reads and saves
without them.
