# Artifact and Image Delivery

## 1. Purpose

Preserve the long-term design and operational evidence for the Site Local Repository and streaming OS
Image upload. Together these mechanisms remove hidden internet, memory-spooling, and premature-success
assumptions from deployment artifact delivery.

## 2. Source Scope

Consolidated from:

- `docs/plans/manuscripts/20261105-os-image-streaming-upload.md`
- `docs/plans/manuscripts/20261106-local-repo.md`
- Artifact, backup, and deployment findings in `20261102-architecture-review-v2.md`

The architecture-review v3 manuscript was excluded and left untouched.

## 3. Consolidated Background

Deployments previously downloaded packages, release files, Python wheels, Helm charts, and OCI images
directly from the internet on every host. Slow or interrupted downloads made deployment duration
unpredictable and broke offline operation. A Site Local Repository now converges declared and
user-supplied artifacts into Pulp and supplies neutral artifact-source configuration to playbooks.

OS Image upload separately used multipart parsing that spooled the entire file into api-server tmpfs,
rehashed it, then forwarded it to MAAS. It now uses a one-time upload session, client-side hash, and raw
streaming proxy, returning success only after the provider reports a complete image.

## 4. Confirmed Decisions

- Pulp 3 is the current Local Repository provider, used as an unmodified independent service through HTTP
  APIs.
- The core domain uses neutral formats: `deb`, `rpm`, `oci`, `python`, and `file`.
- Modules declare their required artifacts; Sites may add Repository Sources.
- OCI and Python use pull-through; declared images are also prefetched and considered ready only when all
  layers are present.
- Debian repositories synchronize as mirrors with signing; file artifacts are verified by SHA-256.
- Every installation includes Pulp and a repository Integration. Each Site chooses whether deployments use
  it through `serveDeployments`, default false; the development Site defaults true.
- Local Repository selection is not fallback. If selected and not ready, deployment is rejected.
- Deployment-facing names remain provider-neutral: `fetchesArtifacts`, `swallow_artifacts`, and
  `artifact_sources`.
- OS Image upload uses a two-step session and raw streaming PUT; the old multipart route is removed.
- Clients compute size and SHA-256 before session creation. Sessions are one-use and expire after one hour.
- api-server keeps provider credentials and protocol knowledge; clients never upload directly to MAAS.
- Upload succeeds only after the provider reports the image fully staged.

## 5. Architecture and Design Principles

- Swallow owns desired artifact availability; the repository adapter realizes and observes it.
- The Site, not each playbook, owns where deployment artifacts come from.
- Modules declare versions, references, and digests; delivery adapters translate them to repository
  resources.
- Host automation receives a complete trusted artifact-source map and does not invent upstream URLs.
- Selected Local Repository use must prevent accidental fallback to internet sources.
- Streaming preserves backpressure and bounded memory; artifacts are not buffered in api-server memory or
  tmpfs.
- Upload cleanup is transactional in effect: a failed stream removes the incomplete provider resource and
  deletes the upload session.
- Provider readiness is observed, not inferred from resource existence.

## 6. Functional Scope

Implemented Local Repository capabilities:

- Repository Integration, spec/status/Conditions, controller, Pulp adapter, API, and Infrastructure
  dashboard page.
- Module `artifacts` declarations and merged effective content.
- Debian, OCI, Python, and file realization.
- Site choice between upstream and Local Repository, including two-way host-source convergence.
- Artifact-source role for apt, pip, containerd, Docker, and source restoration.
- Deployment admission refusal when selected repository content is not ready.
- Installation, bootstrap, backup of Pulp database/certificates, and restore behavior.
- Lab-proven Kubernetes and Slurm RAM deployments without any repository upstream activity.

Implemented OS Image upload capabilities:

- Create-upload and content-upload routes, TTL sessions, atomic claim, raw streaming, and final provider
  readiness wait.
- MAAS preflight, chunk forwarding, incomplete-resource cleanup, and default-user metadata.
- CLI streaming and dashboard incremental hashing with upload phases.
- General 4 MiB request-body limit, dedicated unbounded streaming location, and API 413 envelope.
- Multipart pre-parsing disabled and old multipart descriptions retired.

Remaining:

- RPM/DNF realization, offline bundle import, Podman and Prometheus module declarations, and arm64.
- Upstream proxy settings for Pulp remotes and file downloads.
- Automatic pulp-content restart after changing existing remote download settings.
- Backup of the separate Boot ISO `boot-media` volume.
- Root-cause investigation for the next custom-image deployment failure, using the new durable diagnostic.

## 7. Constraints and Rules

- `serveDeployments=false` means controller does not sync or fetch; existing Pulp content is retained.
- A disabled repository Integration is paused, not an automatic switch to upstream mode.
- Upstream Debian signing keys use HTTPS and pinned fingerprints; host roles verify before use.
- Local Repository mode disables other active apt sources and configures direct access to the repository.
  Upstream mode restores only sources/configuration previously changed by Swallow.
- Declared file paths must map to identical URL and SHA-256 across modules; conflicts fail registry
  construction.
- Repository hooks and adapters must not leak Pulp/apt/dnf terminology into core.
- Artifact content tests reject unregistered internet URLs in deployment roles.
- Upload content requires exact `Content-Length`; session content is consumed at most once.
- Every upload request except the content stream stays within the ordinary request-body limit.
- Pulp GPL redistribution obligations require source-availability compliance and legal review before
  commercial redistribution; this is not legal advice.

## 8. Data Model and Format Notes

- Local Repository spec contains `generation`, `serveDeployments`, and user Repository Sources.
- Effective sources merge module and user declarations and report source, format, declaring modules,
  synchronization state, message, counts, and observation time.
- Module artifacts include sources, files, images, and Python requirements. Debian sources include signing
  key URL and fingerprint.
- `swallow_artifacts` identifies `local_repository` or `upstream` and carries Debian sources, signing data,
  registry/mirror mappings, Python index, file URL mappings, and image-reference mappings.
- Upload session stores integration, metadata, size, SHA-256, state, and expiry. Content is not stored in
  MongoDB.
- Pulp database and `pulp-certs` are backed up; `pulp-data` is reproducible and refetched. This assumption
  requires offline bundle import for disconnected restore.

## 9. CLI / API / Config Notes

- Local Repository API supports summary/status, paginated content browsing, generation-checked spec update,
  and immediate reconciliation.
- There is no Local Repository CLI yet.
- `serveDeployments` is required in spec writes and is surfaced neutrally in the dashboard.
- OS Image upload API:
  - `POST /api/v1/provisioning/image-uploads`
  - `PUT /api/v1/provisioning/image-uploads/{id}/content`
- CLI upload flags and output remain stable while transport changed.
- Dashboard distinguishes hashing and uploading phases.
- Pulp remote timeout is converged to a long bounded value, but running pulp-content workers may retain old
  settings until restart.
- Pulp does not honor ordinary `HTTP_PROXY`; each remote requires `proxy_url`, and file downloads require
  matching downloader configuration.

## 10. Implementation Plan

1. Add repository upstream-proxy settings and sealed credentials. Converge proxy configuration to every
   ordinary remote, per-image Pulp remote, and file downloader.
2. Add an installation-level mechanism that restarts pulp-content after existing remote downloader
   settings change; include it in upgrade/bootstrap sequencing.
3. Implement RPM/DNF through the existing neutral format boundary and add RHEL-family acceptance coverage.
4. Implement signed offline bundle export/import and disconnected restore behavior.
5. Add Podman and Prometheus Software artifact declarations and source configuration.
6. Extend artifact declarations and repository realization to arm64.
7. Add `boot-media` to backup/restore or implement deterministic Boot ISO reconstruction, then verify in
   the restore drill.
8. On the next custom-image deployment failure, use Task diagnostic provider events and installation error
   lines to determine whether validation belongs at upload, deployment admission, or documentation.
9. Add a Local Repository CLI only after stable API use cases are clear.

## 11. Non-goals

- Direct client upload to MAAS.
- Exposing MAAS OAuth credentials or protocol details to CLI/dashboard.
- Treating Local Repository as an automatic fallback cache.
- Hard-coding every product artifact in core.
- Backing up reproducible Pulp artifact blobs before offline import exists.
- Implementing Podman, Prometheus module artifacts, RPM/DNF, arm64, or offline bundles in the completed
  first phase.

## 12. Open Questions

- What is the external contract and media format for offline bundles?
- Should disconnected restore require a colocated bundle, a prior export, or a retained artifact volume?
- Which repository proxy fields belong in Integration settings versus sealed credentials?
- What installation signal safely restarts pulp-content after adapter-driven remote changes?
- Should Boot ISO files be backed up directly or rebuilt from durable records?
- What caused the reported custom-image failure inside MAAS: rack synchronization, file type,
  `base_image`, or image contents?
- When should Local Repository management gain CLI commands?

## 13. Future Work

- Fully disconnected installation, upgrade, and restore.
- Multi-architecture artifacts and support-matrix generation from the same release configuration.
- Repository capacity planning, garbage collection, and retention policy.
- Integrity attestations beyond SHA-256, including release metadata and signatures.
- Additional repository provider adapters behind the neutral interface.
- End-to-end observability for repository synchronization and upload throughput.
