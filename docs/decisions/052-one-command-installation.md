# 052. One command installs and upgrades the single-VM installation

- Status: Accepted
- Date: 2026-10-06
- 現行規格：[installation.md](../development/installation.md) §5 One-command installation

## Context

Installing a release ([ADR 040](040-single-vm-production-installation.md)) took several manual
steps before `swallowctl` could run: download the Compose bundle and `SHA256SUMS`, verify,
install `zstd`, extract, and, on a host with more than one network interface, write
`SWALLOW_MAAS_URL` and `SWALLOW_BOOT_MEDIA_BASE_URL` into `.env` by hand. A setting passed only
in the environment was lost on the next `upgrade` or `docker compose up`. Upgrading had no
documented way to put the new `swallowctl` and `compose.yaml` on the host. The provisioner
Integration was registered at `http://host.docker.internal:5240/MAAS`, so the Dashboard
pre-filled a Boot ISO's rack address with a name that BMC-booted machines cannot resolve.

## Decision

- **Each release ships `install.sh`.** It checks the host, downloads the release's Compose
  bundle, verifies it against the release's checksums, extracts it into the installation
  directory, and hands over to `swallowctl`.
- **`swallowctl` stays the only lifecycle tool.** `install.sh` runs `swallowctl upgrade` when the
  installed-release marker records an earlier release, and `swallowctl install` otherwise: a first
  install, a rerun, or an install that stopped part way. `swallowctl` writes the marker only after
  `doctor` passes. Extracting a new bundle replaces the shipped files and keeps the operator's
  settings, secrets, backups, and state.
- **One address setting.** `SWALLOW_ADDRESS` is the address machines and BMCs use to reach the
  host; the MAAS URL and the Boot Media base URL default from it.
- **Settings persist.** `install` and `upgrade` write the operator settings set in the
  environment into `.env`, with the environment taking precedence as in Compose.
- **The co-located MAAS is registered at `SWALLOW_MAAS_URL`**, not `host.docker.internal`, so the
  Dashboard pre-fills a Boot ISO's rack address with the host's own address. This refines
  ADR 040's backend egress.

## Alternatives considered

- **Publish `swallowctl` on its own and let it download its bundle:** rejected. `swallowctl`
  lives inside the bundle and operates on its own directory; downloading and re-executing itself
  would make one tool both the bootstrapper and the lifecycle tool.
- **Choose upgrade whenever `.env` has a release version:** rejected. `install` writes the
  version early, so a first install that stopped (for example while MAAS imported images) would
  be resumed as an upgrade and skip the MAAS and bootstrap steps.
- **Keep `host.docker.internal` and fix the pre-fill in the Dashboard:** rejected. The Dashboard
  does not know the host's address; the Integration endpoint is the one value it already reads.

## Consequences

- Installing is one `curl … | sudo bash` of the release's `install.sh`, adding `--address` on a
  host with several interfaces; upgrading is the same command with a newer release.
- An installation made before this decision has no installed-release marker; its first
  `install.sh` run takes the `install` path, which converges the existing installation, and
  records the marker.
- An Integration registered before this decision keeps its `host.docker.internal` endpoint.
- Changing the address after the first install is not supported: MAAS keeps the URL it was
  initialized with.
- MAAS DHCP and PXE configuration stays outside the installation.
