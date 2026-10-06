# 052. One command installs and upgrades the single-VM installation

- Status: Accepted
- Date: 2026-10-06

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

- **Each release ships `install.sh`.** Its default version is the release it came with. It
  checks for root, Ubuntu 24.04, and x86_64, installs `curl` and `zstd` when missing, downloads
  the Compose bundle, verifies it against the release's `SHA256SUMS`, extracts it into
  `/opt/swallow` (`--dir`), and hands over to `swallowctl`. Options are flags or environment
  variables: `--address`/`SWALLOW_ADDRESS`, `--dir`/`SWALLOW_INSTALL_DIR`,
  `--version`/`SWALLOW_VERSION`.
- **`swallowctl` stays the only lifecycle tool.** `install.sh` runs `swallowctl upgrade` when
  `state/installed` records an earlier release, and `swallowctl install` otherwise: a first
  install, a rerun, or an install that stopped part way. `swallowctl` writes `state/installed`
  only after `doctor` passes, and `uninstall` removes it. Extracting a new bundle replaces the
  shipped files and keeps `.env`, `secrets/`, `backups/`, and `state/`.
- **One address setting.** `SWALLOW_ADDRESS` is the address machines and BMCs use to reach the
  host; it defaults to the host of the URL MAAS was initialized with, else the primary IPv4.
  `SWALLOW_MAAS_URL` and `SWALLOW_BOOT_MEDIA_BASE_URL` default from it.
- **Settings persist.** `install` and `upgrade` write the operator settings set in the
  environment into `.env`, with the environment taking precedence as in Compose, and fill in
  the address-derived values `.env` lacks.
- **The co-located MAAS is registered at `SWALLOW_MAAS_URL`**, not `host.docker.internal`. The
  API containers reach the host's own address through the `egress` network, and the Dashboard
  pre-fills a Boot ISO's rack address with it. This refines ADR 040's backend egress.

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

- Installing is `curl -fsSL <release>/install.sh | sudo bash`, adding `--address` on a host with
  several interfaces; upgrading is the same command with a newer release.
- An installation made before this decision has no `state/installed`; its first `install.sh` run
  takes the `install` path, which converges the existing installation, and records the marker.
- An Integration registered before this decision keeps its `host.docker.internal` endpoint.
- Changing the address after the first install is not supported: MAAS keeps the URL it was
  initialized with.
- MAAS DHCP and PXE configuration stays outside the installation.

## Current status

Implemented in `deploy/release` (`install.sh`, `assemble-release.sh`, `publish.sh`) and
`deploy/production` (`swallowctl`, `bootstrap.sh`). No release has been published under it yet.
