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
- JWT signing secret and Session lifetimes (`accessTokenTTL` 15m, `refreshTokenTTL` 168h,
  `sessionMaxAge` 720h; the old `jwtExpiryHours` is ignored);
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

### Boot Media

Boot Media (an iPXE ISO the BMC mounts for Servers on networks without
provisioner DHCP; see [OS provisioning](../guides/os-provisioning.md)) is
optional and off until both values are set:

| Setting | Environment variable | Meaning |
| --- | --- | --- |
| `api.bootMedia.isoPath` | `SWALLOW_API_BOOT_MEDIA_ISO_PATH` | The iPXE ISO file the API serves. |
| `api.bootMedia.baseURL` | `SWALLOW_API_BOOT_MEDIA_BASE_URL` | The address BMCs reach swallow at, for example `http://10.0.0.5`, without a path. |
| `api.redfishProbeInterval` | `SWALLOW_API_REDFISH_PROBE_INTERVAL` | How often new or stale Servers get a Redfish probe (default 10m). |
| `api.redfishProbeMaxAge` | `SWALLOW_API_REDFISH_PROBE_MAX_AGE` | How long a probe stays current (default 24h). |

BMCs mount `<baseURL>/boot-media/ipxe/swallow-ipxe.iso` without credentials and
re-read it at every boot. Many BMCs accept only plain `http://` on port 80, so
the production Nginx forwards `/boot-media/` on port 80 (it is not redirected to
HTTPS). The production Compose file reads `SWALLOW_BOOT_MEDIA_BASE_URL` and
mounts `SWALLOW_BOOT_MEDIA_DIR` (default `./boot-media`, containing
`swallow-ipxe.iso`). The ISO typically embeds the provisioner rack's address, so
it is specific to the installation.

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
