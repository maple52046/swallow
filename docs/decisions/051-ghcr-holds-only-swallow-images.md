# 051. GHCR holds only the images swallow builds

- Status: Accepted
- Date: 2026-10-05

## Context

[ADR 040](040-single-vm-production-installation.md) put every installation image in one GHCR
package: the images built from this repository, and copies of the upstream MongoDB, PostgreSQL,
and Temporal Server images, mirrored with their approved digests. The copies are the official
images unchanged; the PostgreSQL one, for instance, has the same config as `postgres:16`. They
were copied so an installation would pull from one registry, partly with offline installation
in mind.

Mirroring cost a publication step, and it hid the images' origin: a mirrored image gets a new
digest because only its linux/amd64 manifest is copied, and the mirror tags were named after
their first user. The shared PostgreSQL was published as `temporal-postgres` and run as the
Compose service `temporal-postgresql`, although it also hosts the co-located MAAS database.

## Decision

- **GHCR holds only what swallow builds:** `api`, `dashboard`, and `cli`, tagged
  `<component>-<version>`. This supersedes the "One image package" part of ADR 040.
- **Official images are used as they are.** `deploy/release/runtime-images.env` pins
  `docker.io/library/mongo`, `docker.io/library/postgres`, and `docker.io/temporalio/auto-setup`
  by their upstream index digests. `publish.sh` checks that each resolves to a linux/amd64 image
  and writes it unchanged into `release-manifest.json`, and the installation pulls it from
  Docker Hub. Installations still never use tags.
- **The shared PostgreSQL has a neutral name.** The manifest key is `postgres`, the variable
  `SWALLOW_POSTGRES_IMAGE`, the Compose service `postgres`, and its volume `postgres-data`. Its
  superuser stays `temporal` and its password secret `temporal-db-password`, because Temporal
  logs in with that account.
- **Offline installation is a separate concern.** When it is built, it ships its own media; it
  is not a reason to copy third-party images into GHCR.

## Alternatives considered

- **Keep mirroring every image into GHCR:** rejected. It duplicates official images under
  swallow's name, needs a mirror step on every release, and does not make offline installation
  work by itself.
- **Mirror but rename only the PostgreSQL tag:** rejected. It fixes the name but keeps the copy
  and the second digest.
- **Rename the PostgreSQL superuser and secret as well:** deferred. It changes Temporal's
  database login and is not needed to make the name neutral.

## Consequences

- Installation hosts need outbound access to Docker Hub as well as GHCR, and Docker Hub limits
  anonymous pulls; an installation pulls three official images.
- The release manifest records upstream digests, so each runtime image can be traced to its
  official source directly.
- Renaming the Compose service and volume is not migrated: an installation of an earlier
  release would start with an empty PostgreSQL volume. No such installation existed when this
  was decided.
- The mirror tags already in GHCR for `0.1.0-rc.1` and `0.1.0-rc.2` stay, because the
  `0.1.0-rc.2` manifest refers to them.

## Current status

Implemented in `deploy/release` (`runtime-images.env`, `publish.sh`, `assemble-release.sh`,
`validate.sh`), `deploy/production` (`compose.yaml`, `swallowctl`, `local-maas.sh`), and the
testing environment. No release has been published under it yet.
