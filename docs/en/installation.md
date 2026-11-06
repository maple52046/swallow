# Installation choices

[繁體中文](../zh-TW/installation.md) · [Documentation home](README.md)

swallow is in active development. Choose an installation path based on what you need.

| Goal | Path | Builds source | Persistent data | Status |
| --- | --- | ---: | ---: | --- |
| Run swallow for users | [Production installation (single VM)](../../deploy/production/README.md) | No | Volumes and backups | Supported installation |
| Evaluate or develop | [Development Compose](../../deploy/dev/README.md) | Yes | Local volumes | Contributor path |
| Verify a release | [Testing Compose](../../deploy/testing/README.md) | No | Isolated volumes | Release validation |
| Install without containers | [Native Ubuntu preview](../../deploy/production/native/README.md) | No | Native paths | Incomplete preview, not published |

## Production installation (single VM)

One command turns a clean Ubuntu 24.04 VM into a working swallow. "Production" names the
installation contract — digest-pinned images, generated secrets, lifecycle tooling — not a
multi-host topology: swallow and its MAAS share the VM.

When the installer finishes:

- the Dashboard and API answer on **HTTP port 80** and the `admin` account can sign in;
- MAAS 3.6 (region+rack) runs on the same VM and is the provisioner Integration of the Site
  `default`, already synced;
- the official `ubuntu/noble` amd64 image is complete and listed under
  **Provisioning → Images**;
- Temporal, the worker, and the Ansible executor are connected, the Deployment Key exists,
  and Site automation is enabled;
- a Prometheus for the Site `default` runs on the same VM, is that Site's metrics Integration,
  and collects the exporters swallow installs on the Site's Servers (see
  [Monitoring](guides/monitoring.md));
- the Site's Local Repository (Pulp) runs on the same VM on **HTTP port 8080** and is the Site's
  `repository` Integration, with the Ubuntu archive as its first source. It starts off: deployments
  fetch their artifacts from the upstream sources swallow's modules declare. Once you turn it on
  for the Site (see [Initial setup](guides/initial-setup.md#4-choose-where-deployments-fetch-from)),
  every Server swallow deploys takes its OS packages, container images, Python packages, and files
  from it instead of the Internet;
- the VM serves NTP on UDP 123 (MAAS's own time service), and every Server swallow deploys
  uses it (see [Time synchronization](guides/os-provisioning.md#time-synchronization));
- the `swallow` CLI is installed at `/usr/local/bin/swallow`.

### Prerequisites

- A clean Ubuntu 24.04 amd64 VM with `sudo`. A practical starting size is 4 vCPU,
  16 GiB RAM, and 100 GiB disk, plus room for the Local Repository's cache when the Site uses it:
  it keeps every package, image, and file the Servers fetched, which a GPU Platform deployment
  grows by tens of GiB.
- Outbound access to `github.com` (the release), `ghcr.io` (swallow's own images), Docker Hub
  (the official MongoDB, PostgreSQL, Temporal, Prometheus, and Pulp images), `quay.io` (the
  prom-label-proxy image), `download.docker.com`, the Snap Store, and `images.maas.io`. To deploy,
  the sources, registries, PyPI, and download sites the installed modules declare must be
  reachable: from the Servers, or, when the Site uses its Local Repository, from the VM, together
  with `archive.ubuntu.com`.
- Free ports: 80 (Dashboard), 8080 (Local Repository), 5240 (MAAS), and loopback 5432
  (PostgreSQL for MAAS). Machines reach the VM's NTP on UDP 123, port 8080, where they fetch
  everything they install when the Site uses its Local Repository, and port 80, where a Platform's
  own Grafana reads its metrics through swallow. Set `SWALLOW_PULP_PORT` before the first install to
  use another port than 8080.
- A reachable NTP server for the VM itself. The VM follows `ntp.ubuntu.com` by default; on a
  network that cannot reach it, pass another one with `SWALLOW_NTP_UPSTREAM`, for example
  `curl … | sudo SWALLOW_NTP_UPSTREAM=10.0.0.123 bash`, or change it later with
  `sudo /opt/swallow/production/swallowctl ntp upstream set 10.0.0.123`.
- No MAAS installed beforehand; the installer refuses a MAAS it did not create. Docker is
  installed from Docker's apt repository when missing.

### Install

Run the installer of the release you want. On a VM with one network interface:

```bash
curl -fsSL https://github.com/maple52046/swallow/releases/download/v<version>/install.sh | sudo bash
```

If machines and BMCs reach the VM through an interface other than the one with the default
route, give that address:

```bash
curl -fsSL https://github.com/maple52046/swallow/releases/download/v<version>/install.sh \
  | sudo bash -s -- --address <address>
```

The installer verifies the release against its checksums, extracts it into `/opt/swallow`
(`--dir` changes the directory), and runs `swallowctl install`: it installs Docker when
missing, writes the release's image digests and your settings to `.env`, generates secrets,
starts the stack, creates the Deployment Key, installs MAAS, imports `ubuntu/noble` (several
hundred MB), registers MAAS, the Site's Prometheus, and the Site's Local Repository in swallow, and
ends with `swallowctl doctor`. It is safe to rerun:
an installation that stopped part way resumes. `--help` lists the options. Once a stable
release exists, `https://github.com/maple52046/swallow/releases/latest/download/install.sh`
always installs the newest one.

### Upgrade

Run the same command with the newer release's `install.sh`. It keeps `.env`, secrets, backups,
and state, and runs `swallowctl upgrade`, which backs up first and refuses while Workflows are
active. An installation whose MongoDB still runs standalone is converted to the single-node
replica set swallow now requires during this upgrade; its data is kept.

### After installation

1. Open `http://<vm-address>/` and sign in as `admin`; the password is in
   `/opt/swallow/production/secrets/bootstrap-admin-password` (`sudo cat` it).
2. Add your own SSH Access Key from the account menu (**SSH keys**). The installer never
   creates one for you.
3. Before network-booting machines, configure the PXE network and DHCP in MAAS
   (`http://<vm-address>:5240/MAAS`, user `admin`, password in
   `/opt/swallow/production/secrets/maas-admin-password`). DNS, DHCP/PXE routing, and BMC
   access are site-owned. Then add machines from **Servers → Add servers**: swallow inspects
   network-booted machines itself, and hosts that keep their OS enroll with one command (see
   [Add servers](guides/servers-and-infrastructure.md#add-servers)).
4. Run `sudo /opt/swallow/production/swallowctl doctor` at any time to recheck every
   connection. After a power loss, a reboot, or a `FAIL` line, run
   `sudo /opt/swallow/production/swallowctl ensure` to converge the installation back.

Backup, restore, uninstall, and installing from a downloaded bundle are described in the
[production installation reference](../../deploy/production/README.md); third-party
components are covered in the [third-party guide](../../deploy/third-party/README.md).

## Development Compose

The shortest path for contributors. Source is bind-mounted, the toolchains run inside
containers, and API/Dashboard changes reload automatically. Its default credentials and
encryption material are intentionally unsafe for shared use.

## Testing Compose

Testing consumes the exact image digests of a release manifest, never source mounts or
locally built tags. It is for validating a release, not for long-lived user data.

## Native preview

The native bundle targets Ubuntu 24.04 amd64, but native Temporal/PostgreSQL systemd
packaging is not complete. Without the complete Temporal topology, Workflows cannot run, so
releases do not publish the native bundle yet.

## Production-safety baseline

- The installation serves plain HTTP. Put a TLS terminator in front of port 80 before
  exposing it beyond a trusted management network. The Local Repository on port 8080 is plain
  HTTP for the machine networks; its OS packages are signed with the installation's own key and
  its files are checked against their declared sha256.
- Keep the generated secrets: `/opt/swallow/production/secrets/` is mode 0700 and must never enter source
  control.
- Install only from a release manifest with immutable image digests.
- Schedule `swallowctl backup` and rehearse `restore`. A backup holds MongoDB, the credential
  key, Workflow artifacts, the Local Repository's database and keys, and the MAAS database; the
  Deployment Key's private key is sealed with the credential key, so both must be restored
  together. Metrics in the Site's Prometheus are not backed up: losing them loses usage history
  only. The Local Repository's cached packages and images are not backed up either: after a
  restore they are downloaded again from their upstreams.
