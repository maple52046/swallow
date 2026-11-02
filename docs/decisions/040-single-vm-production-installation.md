# 040. Single-VM installation with co-located MAAS, Compose-only releases, and one-command install

- Status: Accepted
- Date: 2026-10-01
- 現行規格：[installation.md](../development/installation.md) §2 安裝拓樸、§3 `swallowctl` lifecycle、§4 Release artifacts 與 image provenance、§5 One-command installation

## Context

The production Compose installation used to stop at swallow itself: MAAS needed a dedicated host,
image synchronization and the provisioner Integration were manual, and every backend service sat on
an internal network, so nothing could reach a provisioner or a Server. The goal is that one clean
Ubuntu 24.04 VM becomes operable with one command: the Dashboard, a synced MAAS Integration,
Temporal with a polling worker, a working Ansible executor, and a deployable `ubuntu/noble` image.
"Production" names the installation contract (digest-pinned images, generated secrets, lifecycle
tooling), not a multi-host topology.

Releases also carried artifacts that installation did not use: a native bundle that could not run
Workflows, an offline image archive (about 900 MB) that could not make a host installable because
MAAS images come from `images.maas.io`, and GHCR copies of upstream images that hid their origin.
Installing a release took several manual steps before `swallowctl` could run, and a setting passed
only in the environment was lost on the next upgrade.

## Decision

### Topology

- **One VM.** swallow runs on Docker Compose and a MAAS 3.6 region+rack snap runs on the same host.
  `swallowctl` installs, initializes, stops, and purges that MAAS and refuses a MAAS it did not install.
- **One PostgreSQL instance, separate databases.** MAAS stores its state in the Compose PostgreSQL that
  serves Temporal, under its own role and database; `maas-test-db` is never used.
- **swallow's own state is in MongoDB, as a single-node replica set** in every environment, because the
  store needs transactions and change streams
  ([decision 069](069-swallow-owns-desired-state.md) (swallow owns desired state)); every process
  refuses to start on a standalone server.
- **Backend egress.** The API, worker, and Ansible executor also join a non-internal network to reach
  the co-located MAAS and the Servers; MongoDB, Temporal, and PostgreSQL stay internal. The co-located
  MAAS is registered at the host's own address, not `host.docker.internal`, so the address the
  Dashboard pre-fills for a Boot ISO's rack is one machines can reach.
- **Official image only.** The installation ensures the official `ubuntu/noble` amd64 image; custom
  images and offline image media are not part of it.
- **Records through the published API.** A post-install bootstrap creates the Site, registers MAAS as
  its provisioner Integration, and writes Site automation defaults only when none exist. Co-location
  changes the topology, not the model: MAAS is an external provisioner behind its Integration.
- **Plain HTTP** on port 80; TLS, when required, terminates in front of the installation.

### Releases

- **A release ships only the Compose installation**: the Compose bundle, `install.sh`, the CLI binary,
  the release manifest, the offline media manifest, and checksums. No native bundle until native
  Temporal and PostgreSQL packaging is complete, no offline image archive until offline installation
  (MAAS images included) is supported, and no mirrored Temporal UI: its image is the user's choice and
  must be an exact digest.
- **GHCR holds only the images swallow builds** (`api`, `dashboard`, `cli`). MongoDB, PostgreSQL, and
  Temporal Server are used unchanged from Docker Hub, pinned by their upstream index digests in the
  release manifest. Installations never use tags.
- **The shared PostgreSQL has a neutral name**, `postgres`, wherever swallow names it; its superuser
  and password secret keep their Temporal names because Temporal logs in with that account.
- **Publishing needs crane on `PATH`**, with no container fallback.
- **Air-gapped installation is deferred, not dropped.** When built, it ships its own media; it is not
  a reason to copy third-party images into GHCR.

### Lifecycle

- **`swallowctl` is the only lifecycle tool.** Each release's `install.sh` checks the host, downloads
  and verifies the bundle, extracts it (replacing shipped files, keeping settings, secrets, backups,
  and state), and runs `swallowctl upgrade` when the installed-release marker records an earlier
  release and `swallowctl install` otherwise. `swallowctl` writes the marker only after `doctor` passes.
- **Downgrades are refused**, by release version and by database schema: a binary serves only its
  own schema.
- **One address setting**, `SWALLOW_ADDRESS`, from which the MAAS URL and the Boot Media base URL
  default; settings given in the environment persist to `.env`.
- **Restore is store-first.** Backups include the MAAS database but not Temporal; a restore ends
  swallow's Temporal runs and the Workflow dispatcher re-dispatches every unfinished Workflow from
  the restored store ([decision 072](072-store-driven-workflow-execution.md) (store-driven Workflow
  execution)).

## Alternatives considered

- **Keep MAAS on a dedicated host:** rejected for now; it doubles the machines needed before anything
  works. A multi-host topology can be added later without changing the Integration model.
- **A second PostgreSQL for MAAS, or host PostgreSQL shared with Temporal:** rejected; two database
  engines and backup paths on one VM, or Temporal leaving the internal network and tests needing a
  host database. `maas-test-db` is a test fixture, not a datastore.
- **HTTPS with installation-supplied CA material:** deferred; certificate logistics blocked first-run
  installs, and TLS can be layered in front.
- **Keep the offline archive or the native bundle:** rejected for now; a host without network still
  cannot install, and the native bundle cannot run Workflows and tied publishing to a Python version.
- **Mirror every image, the Temporal UI included, into GHCR:** rejected; it duplicates official images
  under swallow's name with a second digest, adds a release step, and does not make offline
  installation work by itself.
- **Rename the PostgreSQL superuser and secret too:** deferred; it changes Temporal's database login.
- **A crane container fallback:** rejected; under rootless nerdctl it could not read the registry
  login and hung on streamed output.
- **Publish `swallowctl` alone and let it download its bundle:** rejected; one tool would be both the
  bootstrapper and the lifecycle tool.
- **Choose upgrade whenever `.env` has a release version:** rejected; `install` writes it early, so an
  install that stopped part way would resume as an upgrade and skip the MAAS and bootstrap steps.

## Consequences

- MAAS depends on the Compose PostgreSQL: stopping or uninstalling the stack stops MAAS, an upgrade
  that recreates PostgreSQL briefly interrupts it, and boot images re-sync after a restore.
- The VM is a single failure domain for swallow and its provisioner, and plain HTTP exposes the admin
  login unless a TLS terminator fronts it.
- Installation needs outbound access to GHCR, Docker Hub (anonymous pull limits apply),
  `images.maas.io`, and the snap store; an air-gapped site cannot install from a release.
- Each runtime image can be traced to its official source by the digest in the manifest.
- The address cannot change after the first install, because MAAS keeps the URL it was initialized
  with. MAAS DHCP and PXE configuration stays outside the installation.

## Revision history

- 2026-10-01: decision 040 made the production installation one VM running swallow on Compose beside a `swallowctl`-installed MAAS that shares the Compose PostgreSQL instance.
- 2026-10-05: decision 040 limited releases to the Compose installation, deferring the native bundle and the offline image archive.
- 2026-10-05: decision 040 kept only swallow-built images in GHCR and used official runtime images unchanged by their upstream digests.
- 2026-10-06: decision 040 added a per-release `install.sh` that installs or upgrades in one command, keeping `swallowctl` the only lifecycle tool.
- 2026-10-30: decision 069 required MongoDB to run as a single-node replica set in every installation.
- 2026-11-02: merged decision 040 into this ADR; the body states the current decision.
