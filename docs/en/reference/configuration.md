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

Boot Media lets a Server's BMC mount a swallow-built Boot ISO on networks
without provisioner DHCP; see [OS provisioning](../guides/os-provisioning.md).
The builder needs a base URL, a writable storage directory, and its iPXE assets
and packaging tools:

| Setting | Environment variable | Meaning |
| --- | --- | --- |
| `api.bootMedia.baseURL` | `SWALLOW_API_BOOT_MEDIA_BASE_URL` | The address BMCs reach swallow at, for example `http://10.0.0.5`, without a path. |
| `api.bootMedia.dir` | `SWALLOW_API_BOOT_MEDIA_DIR` | Writable Boot ISO storage; default `/var/lib/swallow/boot-media`. Files are stored as `<dir>/<isoId>/swallow-ipxe.iso`. |
| `api.bootMedia.ipxeDir` | `SWALLOW_API_IPXE_DIR` | Prebuilt iPXE assets (`ipxe.lkrn`, `ipxe.efi`, `genfsimg`, `VERSION`); default `/usr/share/swallow/ipxe`. |
| `api.redfishProbeInterval` | `SWALLOW_API_REDFISH_PROBE_INTERVAL` | How often new or stale Servers get a Redfish probe (default 10m). |
| `api.redfishProbeMaxAge` | `SWALLOW_API_REDFISH_PROBE_MAX_AGE` | How long a probe stays current (default 24h). |

Each Boot ISO is served without authentication and with HTTP byte ranges at
`<baseURL>/boot-media/ipxe/<isoId>/swallow-ipxe.iso`. Many BMCs accept only
plain `http://` on port 80, so production Nginx forwards `/boot-media/` there.
Production Compose reads `SWALLOW_BOOT_MEDIA_BASE_URL`, sets the API storage
directory, and mounts the named `boot-media` volume at
`/var/lib/swallow/boot-media`. The installer writes `SWALLOW_BOOT_MEDIA_BASE_URL`
into `.env` as `http://<SWALLOW_ADDRESS>` (`install.sh --address`) unless you set it,
so Boot Media is available after installation.

The container images include the pinned iPXE assets and the required packaging
tools. A native installation creates the Boot Media directory through tmpfiles,
but does not install those assets. To enable building natively, provide
`ipxe.lkrn`, `ipxe.efi`, `genfsimg`, and `VERSION` under
`api.bootMedia.ipxeDir`, and install the `mtools`, `xorriso`, `isolinux`, and
`syslinux-common` packages.

When the base URL is empty, the storage directory is not writable, or an asset
or tool is missing, the Dashboard explains why building is unavailable and
disables **Build ISO**. Existing Boot ISOs remain listed and deletable.

`api.bootMedia.isoPath` and `SWALLOW_API_BOOT_MEDIA_ISO_PATH` are retired and
ignored with a startup warning. The fixed
`/boot-media/ipxe/swallow-ipxe.iso` route is removed; production Compose no
longer reads `SWALLOW_BOOT_MEDIA_DIR`, and a hand-made file at that path is not
served.

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
