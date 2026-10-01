# Configuration reference

[繁體中文](../../zh-TW/reference/configuration.md) · [Documentation home](../README.md)

## API configuration

`swallow-api` merges configuration in this order, highest priority first:

1. `SWALLOW_API_*` environment variables.
2. Command flags.
3. The file passed with `--config`.
4. Development-oriented defaults.

The annotated [configuration example](../../../api-server/docs/config-example.yaml)
and the [api-server README](../../../api-server/README.md) are the maintained
field references.

Important production-sensitive values include:

- MongoDB URI/database and migration access;
- JWT signing secret and expiry;
- bootstrap admin username/password;
- base64 32-byte credential-encryption key;
- machine bearer token;
- reconcile, inventory, and SSH key sync intervals (`sshKeySyncInterval`, default 5m);
- Temporal address, namespace, task queue, and start interval;
- Ansible runner command, playbook manifest/directory, runtime/artifact
  directories, retention, and parallelism;
- maximum uploaded OS image size.

Changing the credential key without restoring matching encrypted data makes
stored credentials unreadable.

## Dashboard configuration

`VITE_API_BASE_URL` selects the API origin at build/dev-server start. The
development Compose stack leaves it empty and uses Vite's same-origin proxy.
Restart/rebuild the Dashboard after changing a Vite environment variable.

Production serves Dashboard and API through the same Nginx origin over plain HTTP
(port 80 by default); MongoDB and Temporal stay internal, and the API, worker, and executor
reach MAAS and Servers through the `egress` network. Terminate TLS in front of it when the
installation is exposed beyond a trusted network.

## CLI configuration

The CLI resolves:

1. profile file;
2. `SWALLOW_*` environment variables;
3. global flags.

`$SWALLOW_CONFIG` selects another profile path. Common overrides are
`SWALLOW_ENDPOINT`, `SWALLOW_TOKEN`, `SWALLOW_SITE`,
`SWALLOW_MACHINE_TOKEN`, and `SWALLOW_INSECURE`. Never use insecure TLS
outside a controlled lab.

## Installation secrets

Compose and native assets document their exact secret file contract. Keep secret
directories out of source control and back up the credential key with MongoDB and
artifacts. The production installation generates every secret, including the co-located
MAAS database password, admin password, and API key, under `production/secrets/`.
