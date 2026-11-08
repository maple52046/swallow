# Release and publication

[繁體中文](README.zh-TW.md) · [Installation choices](../../docs/en/installation.md)

swallow has no CI: a release is published from a workstation with [`publish.sh`](publish.sh).

## Images

The GHCR package `ghcr.io/<owner>/swallow` holds only the images built from this repository,
tagged `<component>-<version>`. The runtime services use official images as they are:

| Image | Source |
| --- | --- |
| `api`, `dashboard`, `cli` | built from this repository (linux/amd64) and pushed to GHCR |
| MongoDB, PostgreSQL, Temporal Server, Prometheus | the official `mongo`, `postgres`, `temporalio/auto-setup`, and `prom/prometheus` images on Docker Hub, pinned in [`runtime-images.env`](runtime-images.env) |
| prom-label-proxy | the Prometheus community's `prometheuscommunity/prom-label-proxy` image on Quay, pinned in [`runtime-images.env`](runtime-images.env) |
| Pulp (the Site's Local Repository) | the Pulp project's `pulp/pulp-minimal` and `pulp/pulp-web` images on Docker Hub, pinned in [`runtime-images.env`](runtime-images.env) |

Installations never use tags: `release-manifest.json` pins every image by digest. Review and
bump `runtime-images.env` deliberately; the PostgreSQL image also hosts the co-located MAAS
database, so it must stay PostgreSQL 14 or newer, and the Prometheus image must keep
`scrape_config_files` and busybox `wget` (the Site's exporter discovery and its health checks
rely on them). The prom-label-proxy image must keep `-header-name` (it takes the `platform_id`
from a request header), `-enable-label-apis`, and busybox `wget` (its healthcheck). Pulp uses the
shared PostgreSQL, so it is `pulp-minimal` (the all-in-one image always starts its own database);
anonymous container pull-through needs `pulp_container` 2.30.1 (or 2.29.3) or later, and
`pulp-web` is bumped together with `pulp-minimal`. Releases reference the Pulp images by their
upstream digest and never redistribute them. The optional Temporal UI is not part of a
release; a user who wants it sets its digest in `production/.env`.

## Publish a release

Publish from an x86_64 Linux machine. Every image is built natively for linux/amd64 with plain
`docker build` and `docker push`, so buildx and emulation are not needed: Docker Engine, or
nerdctl with BuildKit running, is enough. The machine also needs git, jq, zstd, curl, python3,
[crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane) on `PATH`, and a
GitHub token with `write:packages`. Run the component test suites first (see the component
READMEs).

```bash
go install github.com/google/go-containerregistry/cmd/crane@v0.22.1   # once; add Go's bin to PATH
echo "$GHCR_TOKEN" | docker login ghcr.io -u <github-user> --password-stdin
git switch main && git pull           # publish committed code only
deploy/release/publish.sh 0.1.0       # add --github-release to create the GitHub Release (gh)
```

`publish.sh` runs [`validate.sh`](validate.sh) and the documentation check, checks with
[`check-declared-files.sh`](check-declared-files.sh) that every file the modules declare answers at its
URL, refuses a version whose tags already exist, checks that each official runtime image resolves to linux/amd64,
builds and pushes `api`, `dashboard`, and `cli`, and assembles the artifacts with
[`assemble-release.sh`](assemble-release.sh) in `out/release/<version>/`:

- `swallow-compose-<version>.tar.zst` — the single-VM installation (`production/` with the
  CLI, `testing/`, and the manifest);
- `install.sh` — the one-command installer and upgrader for this version, generated from
  [`install.sh`](install.sh) with the version filled in;
- `swallow-linux-amd64` — the CLI;
- `release-manifest.json`, `offline-media-manifest.json`, and `SHA256SUMS`.

The manifest's database schema version and the schemas it upgrades from come from
`swallow-api version --json` of the pushed API image, which must report the version and commit
being published. `install.sh` and `swallowctl upgrade` use them, together with the version, to
refuse a downgrade.

Releases carry only the Compose installation
([decision 040](../../docs/decisions/040-single-vm-production-installation.md) (single vm production installation)): there is no native
bundle until native packaging is complete, and no offline image archive until offline
installation, including MAAS images, is supported.

After the first push, make the GHCR package public, or every installation must
`docker login ghcr.io` before `swallowctl install`. Without `--github-release`, attach the
artifacts to a GitHub Release by hand. Releases carry SHA-256 checksums but no signatures or
SBOMs.

## Host binaries swallow builds

A module may need a host binary that no upstream publishes. swallow builds it with its own tools
and publishes it under its own GitHub Release, separate from swallow's releases. The module then
declares it as a file artifact
([decision 079](../../docs/decisions/079-release-built-host-artifacts.md) (release-built host artifacts)).
Today that is Omnistat's ROCProfiler-SDK extension, which the Slurm Platform's hardware counters load,
built by the tools in [`omnistat-rocprof/`](omnistat-rocprof/):

```bash
deploy/release/omnistat-rocprof/build.sh          # every ROCm minor in matrix.json; or name some: build.sh 10.0
deploy/release/omnistat-rocprof/upload.sh         # on the release machine, with gh
```

- [`matrix.json`](omnistat-rocprof/matrix.json) is the build matrix and the compatibility table: the
  Omnistat source (the one the Slurm module declares), the revision, the builder image
  (`manylinux_2_28`: glibc 2.28, CPython 3.12) and the Ubuntu 24.04 check image, both pinned by
  digest, AMD's package repositories with the fingerprint of their signing key, and, for each ROCm
  minor, its package version, its ROCProfiler-SDK version, and whether it was verified on hardware
  (`lab-verified`) or only built and checked (`build-only`).
- `build.sh` builds one asset per ROCm minor in a builder with that minor's Core SDK. It builds with
  Omnistat's own `omnistat-build-extras` and without network access. It imports the module in the
  builder and again with Ubuntu 24.04's own `python3.12`. It fails when any file needs a newer
  `GLIBC_` than the builder's glibc, or when the SDK is not the version the table lists. Its output is
  in `out/omnistat-rocprof/<omnistat>-r<revision>/`: the
  `omnistat-rocprof-<omnistat>-rocm<X.Y>-cp312-x86_64.tar.gz` assets, `SHA256SUMS`, and
  `declarations.json`.
- `upload.sh` publishes them as the prerelease `omnistat-rocprof-<omnistat>-r<revision>`, never
  marked latest, and refuses a revision that is already published. A rebuild has a new sha256, so it
  is a new revision.
- Upload first, then declare: copy `declarations.json` into the Slurm module's `artifacts.files`
  only after the upload. Until then the URL answers 404, and a Site that uses its Local Repository
  would refuse every deployment.
