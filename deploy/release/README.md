# Release and promotion

[繁體中文](README.zh-TW.md) · [Installation choices](../../docs/en/installation.md)

Pull requests run documentation validation, Go tests/vet, Dashboard lint/build,
Compose and release-contract validation, production image builds, and Trivy scanning.
Merging to `main` builds and signs one candidate set. Branch names never select an
environment.

Before enabling the Candidate workflow, configure repository variables
`SWALLOW_MONGO_IMAGE`, `SWALLOW_TEMPORAL_POSTGRES_IMAGE`,
`SWALLOW_TEMPORAL_SERVER_IMAGE`, and `SWALLOW_TEMPORAL_UI_IMAGE` with approved exact
digests. The workflow records them alongside the API and Dashboard digests, saves all six
images into the air-gap archive, and deploys those exact candidate digests to an
ephemeral testing stack.

A `vX.Y.Z` tag is accepted only when the same commit has a successful Candidate
workflow. Promotion uses `crane tag` on the verified digests and does not rebuild. The
release packages:

- `swallow-oci-<version>.tar` for `docker load`;
- native and Compose `.tar.zst` bundles;
- release and third-party compatibility manifests;
- API and Dashboard SPDX SBOMs;
- SHA-256 checksums and a Sigstore checksum bundle.

Shared testing consumes the candidate's `release-manifest.json`. Production consumes
the promoted manifest and SemVer release artifacts.
