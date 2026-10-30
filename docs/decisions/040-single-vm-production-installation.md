# 040. Single-VM production installation co-locates MAAS

- Status: Accepted; "One image package" superseded by [ADR 051](051-ghcr-holds-only-swallow-images.md); backend egress refined by [ADR 052](052-one-command-installation.md)
- Date: 2026-10-01
- 現行規格：[installation.md](../development/installation.md) §2 安裝拓樸、§3 `swallowctl` lifecycle

## Context

The production Compose installation stopped at the swallow control plane. MAAS had to run on
a dedicated host, image synchronization and the provisioner Integration were manual, and the
Compose topology could not have reached MAAS anyway: every backend service sat only on the
internal `control` network, so neither the API nor the worker could call a provisioner and the
Ansible executor could not open SSH to a Server.

The release goal is that one clean Ubuntu 24.04 VM becomes operable with one
`swallowctl install`: the Dashboard, a synced MAAS provisioner Integration, Temporal with a
polling worker, a working Ansible executor, and a deployable official `ubuntu/noble` image.
"Production" names the installation contract (digest-pinned images, generated secrets,
lifecycle tooling), not a multi-host topology.

## Decision

- **One VM.** The production installation runs swallow on Docker Compose and a MAAS 3.6
  region+rack snap on the same host. `swallowctl` installs, initializes, stops, and purges that
  MAAS itself and refuses a MAAS snap it did not install.
- **One PostgreSQL instance, separate databases.** MAAS stores its state in the Compose
  PostgreSQL instance that already serves Temporal, under its own role and database.
  `maas-test-db` is never used. Swallow's own state stays in MongoDB.
- **Backend egress.** The API, worker, and Ansible executor also join a non-internal `egress`
  network to reach the co-located MAAS and the Servers. MongoDB, Temporal, and PostgreSQL stay
  internal.
- **Official image only.** The installation ensures the official `ubuntu/noble` amd64 image and
  waits until MAAS reports it complete. Third-party or custom images and offline image media are
  not part of the installation.
- **Records through the published API.** A post-install bootstrap creates the Site, registers
  MAAS as its provisioner Integration, and writes Site automation defaults only when none exist,
  using the api-server contracts. Ownership is unchanged
  ([ADR 001](001-system-ownership-boundaries.md)): MAAS remains the provisioner that owns machine
  and image facts; co-location changes the topology, not who owns what.
- **Plain HTTP.** The Dashboard and API are served on HTTP port 80. TLS, when required,
  terminates in front of the installation.
- **One image package** (superseded by ADR 051). Every installation image, including mirrored
  upstream runtime images, was to be published to one GHCR package; the release manifest still
  pins digests.

## Alternatives considered

- **Keep MAAS on a dedicated host:** rejected for this release because it doubles the
  machines needed before anything works. A multi-host topology can be added later without
  changing the Integration model.
- **A second PostgreSQL for MAAS (host package or another container):** rejected — two
  database engines on one VM and a second backup path; a separate role and database already
  isolate MAAS inside one instance.
- **Host PostgreSQL shared with Temporal:** rejected — Temporal would leave the internal
  network to reach the host, and testing/CI would need a host database.
- **`maas-test-db`:** rejected — it is a test fixture, not a production datastore.
- **HTTPS with installation-supplied CA material:** deferred; it blocked first-run installs on
  certificate logistics and can be layered in front of plain HTTP.

## Consequences

- MAAS depends on the Compose PostgreSQL: stopping or uninstalling the stack stops MAAS, an
  upgrade that recreates PostgreSQL briefly interrupts it, and backup and restore include the
  MAAS database (boot images re-sync from `images.maas.io`).
- The VM is a single failure domain for swallow and its provisioner.
- Plain HTTP exposes the admin login on the network unless a TLS terminator fronts it.
- Installation needs outbound access to GHCR and `images.maas.io`; air-gapped image media is
  follow-up work.
