# Release and publication

[繁體中文](README.zh-TW.md) · [Installation choices](../../docs/en/installation.md)

swallow has no CI: a release is published from a workstation with [`publish.sh`](publish.sh).

## Images

Every installation image lives in one GHCR package, `ghcr.io/<owner>/swallow`, tagged
`<component>-<version>`:

| Component | Source |
| --- | --- |
| `api`, `dashboard`, `cli` | built from this repository (linux/amd64) |
| `mongo`, `temporal-postgres`, `temporal-server` | upstream images pinned in [`runtime-images.env`](runtime-images.env), mirrored for linux/amd64 |

Installations never use tags: `release-manifest.json` pins every image by the digest read back
from the registry. Review and bump `runtime-images.env` deliberately; the Temporal PostgreSQL
image also hosts the co-located MAAS database, so it must stay PostgreSQL 14 or newer. The
optional Temporal UI is not part of a release; an operator who wants it sets its digest in
`production/.env`.

## Publish a release

Publish from an x86_64 Linux machine. Every image is built natively for linux/amd64 with plain
`docker build` and `docker push`, so buildx and emulation are not needed: Docker Engine, or
nerdctl with BuildKit running, is enough. The machine also needs git, jq, zstd, python3,
[crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane) on `PATH`, and a
GitHub token with `write:packages`. Run the component test suites first (see the component
READMEs).

```bash
go install github.com/google/go-containerregistry/cmd/crane@v0.22.1   # once; add Go's bin to PATH
echo "$GHCR_TOKEN" | docker login ghcr.io -u <github-user> --password-stdin
git switch main && git pull           # publish committed code only
deploy/release/publish.sh 0.1.0       # add --github-release to create the GitHub Release (gh)
```

`publish.sh` runs [`validate.sh`](validate.sh) and the documentation check, refuses a version
whose tags already exist, builds and pushes `api`, `dashboard`, and `cli`, mirrors the runtime
images with [`mirror-runtime-images.sh`](mirror-runtime-images.sh), and assembles the
artifacts with [`assemble-release.sh`](assemble-release.sh) in `out/release/<version>/`:

- `swallow-compose-<version>.tar.zst` — the single-VM installation (`production/` with the
  CLI, `testing/`, and the manifest);
- `swallow-linux-amd64` — the CLI;
- `release-manifest.json`, `offline-media-manifest.json`, and `SHA256SUMS`.

Releases carry only the Compose installation
([ADR 050](../../docs/decisions/050-compose-only-release-artifacts.md)): there is no native
bundle until native packaging is complete, and no offline image archive until offline
installation, including MAAS images, is supported.

After the first push, make the GHCR package public, or every installation must
`docker login ghcr.io` before `swallowctl install`. Without `--github-release`, attach the
artifacts to a GitHub Release by hand. Releases carry SHA-256 checksums but no signatures or
SBOMs.
