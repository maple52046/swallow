# Installation choices

[繁體中文](../zh-TW/installation.md) · [Documentation home](README.md)

swallow is in active development. Choose an installation path based on what you need.

| Goal | Path | Builds source | Persistent data | Status |
| --- | --- | ---: | ---: | --- |
| Run swallow for operators | [Production installation (single VM)](../../deploy/production/README.md) | No | Volumes and backups | Supported installation |
| Evaluate or develop | [Development Compose](../../deploy/dev/README.md) | Yes | Local volumes | Contributor path |
| Verify a release | [Testing Compose](../../deploy/testing/README.md) | No | Isolated volumes | Release validation |
| Install without containers | [Native Ubuntu preview](../../deploy/production/native/README.md) | No | Native paths | Incomplete preview |

## Production installation (single VM)

One command turns a clean Ubuntu 24.04 VM into a working swallow. "Production" names the
installation contract — digest-pinned images, generated secrets, lifecycle tooling — not a
multi-host topology: swallow and its MAAS share the VM.

When `swallowctl install` finishes:

- the Dashboard and API answer on **HTTP port 80** and the `admin` account can sign in;
- MAAS 3.6 (region+rack) runs on the same VM and is the provisioner Integration of the Site
  `default`, already synced;
- the official `ubuntu/noble` amd64 image is complete and listed under
  **Provisioning → Images**;
- Temporal, the worker, and the Ansible executor are connected, the Deployment Key exists,
  and Site automation is enabled;
- the `swallow` CLI is installed at `/usr/local/bin/swallow`.

### Prerequisites

- A published swallow release: its GitHub Release assets (the Compose bundle,
  `release-manifest.json`, and `SHA256SUMS`) and a public GHCR package, or run
  `docker login ghcr.io` on the VM first. The repository's `release-manifest.example.json`
  holds placeholder digests and cannot be installed.
- A clean Ubuntu 24.04 amd64 VM with `sudo`. A practical starting size is 4 vCPU,
  16 GiB RAM, and 100 GiB disk.
- Outbound access to `ghcr.io`, `download.docker.com`, the Snap Store, and
  `images.maas.io`.
- Free ports: 80 (Dashboard), 5240 (MAAS), and loopback 5432 (PostgreSQL for MAAS).
- No MAAS installed beforehand; the installer refuses a MAAS it did not create. Docker is
  installed from Docker's apt repository when missing.

### Install

Download the Compose bundle and checksums from the GitHub release, then run the installer
from its `production/` directory:

```bash
sha256sum --ignore-missing -c SHA256SUMS
tar --zstd -xf swallow-compose-<version>.tar.zst
cd production
sudo ./swallowctl install --profile production --release-manifest ../release-manifest.json
```

The installer writes the release's image digests to `.env`, generates secrets, starts the
stack, creates the Deployment Key, installs MAAS, imports `ubuntu/noble` (several hundred
MB), registers MAAS in swallow, and ends with `swallowctl doctor`. It is safe to rerun.

### After installation

1. Open `http://<vm-address>/` and sign in as `admin`; the password is in
   `production/secrets/bootstrap-admin-password` (`sudo cat` it).
2. Add your own SSH Access Key from the account menu (**SSH keys**). The installer never
   creates one for you.
3. Before commissioning machines, configure the PXE network and DHCP in MAAS
   (`http://<vm-address>:5240/MAAS`, user `admin`, password in
   `production/secrets/maas-admin-password`). DNS, DHCP/PXE routing, and BMC access are
   site-owned.
4. Run `sudo ./swallowctl doctor` at any time to recheck every connection.

Upgrade, backup, restore, and uninstall are described in the
[production installation reference](../../deploy/production/README.md); third-party
components are covered in the [third-party guide](../../deploy/third-party/README.md).

## Development Compose

The shortest path for contributors. Source is bind-mounted, the toolchains run inside
containers, and API/Dashboard changes reload automatically. Its default credentials and
encryption material are intentionally unsafe for shared use.

## Testing Compose

Testing consumes the exact image digests of a release manifest, never source mounts or
locally built tags. It is for validating a release, not for long-lived operator data.

## Native preview

The native bundle targets Ubuntu 24.04 amd64, but native Temporal/PostgreSQL systemd
packaging is not complete. Without the complete Temporal topology, Workflows cannot run.

## Production-safety baseline

- The installation serves plain HTTP. Put a TLS terminator in front of port 80 before
  exposing it beyond a trusted management network.
- Keep the generated secrets: `production/secrets/` is mode 0700 and must never enter source
  control.
- Install only from a release manifest with immutable image digests.
- Schedule `swallowctl backup` and rehearse `restore`. A backup holds MongoDB, the credential
  key, Workflow artifacts, and the MAAS database; the Deployment Key's private key is sealed
  with the credential key, so both must be restored together.
