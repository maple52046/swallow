# Release and promotion

Pull requests run Go tests/vet, Dashboard lint/build, Compose and contract validation,
production image builds, and Trivy scanning. Merging to `main` builds and signs one
candidate set. Branch names never select an environment.

Before enabling the Candidate workflow, configure repository variable
`SWALLOW_MONGO_IMAGE` with an exact `mongo@sha256:...` reference approved for the
release. The workflow records that digest alongside the API and Dashboard digests, saves
all three images into the air-gap archive, and deploys those exact candidate digests to
an ephemeral testing stack.

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
