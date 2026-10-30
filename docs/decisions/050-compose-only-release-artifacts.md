# 050. Releases ship only the Compose installation

- Status: Accepted
- Date: 2026-10-05
- 現行規格：[installation.md](../development/installation.md) §4 Release artifacts 與 image provenance

## Context

The single-VM installation ([ADR 040](040-single-vm-production-installation.md)) needs three
things from a release: the Compose bundle, its `release-manifest.json`, and the images that
manifest pins. Each release also produced a native preview bundle, an offline OCI archive of the
runtime images (about 900 MB), and a mirrored Temporal UI image, and `publish.sh` ran crane from a
container when crane was not installed.

None of those serve the installation that works today. The native installer cannot run
Workflows until native Temporal and PostgreSQL packaging lands, yet its Python wheelhouse made
every publication depend on pip and on the publishing host's Python version. The offline
archive cannot make a host installable without network access, because the installation also
imports `ubuntu/noble` from `images.maas.io`. The Temporal UI is a diagnostics service the
installer never pulls or starts. Publishing `0.1.0-rc.2` from a workstation whose `docker` is
rootless nerdctl also showed that the crane container could not read a mode-0600 registry login
and did not return after streaming its output.

## Decision

- A release contains only the Compose installation: the Compose bundle, the CLI binary, the
  release manifest, the offline media manifest, and checksums. The manifest no longer lists the
  Temporal UI image, an offline archive, or a native bundle; its `schemaVersion` stays 1 because no
  consumer reads those fields.
- The native bundle is not published until native Temporal and PostgreSQL packaging is
  complete.
- No offline image archive is published until offline installation, including MAAS images, is
  supported.
- The Temporal UI is not mirrored. Its image is the operator's choice, and it must still be an
  exact digest when set.
- The publishing host needs crane on `PATH`; there is no container fallback. Release assembly
  reads the CLI binary from the pushed image with crane instead of a local container engine.

This defers the air-gap goal stated in [ADR 006](006-embedded-ansible-execution.md) and
[ADR 016](016-temporal-operation-orchestration.md); it does not drop it.

## Alternatives considered

- **Keep the offline archive:** rejected for now. A host without network still cannot install,
  because MAAS images come from `images.maas.io`, and the archive added about 900 MB and a full
  local pull of every runtime image to each release.
- **Keep publishing the native bundle:** rejected. It cannot run Workflows, and its wheelhouse
  tied the publishing host to the target's Python version.
- **Keep mirroring the Temporal UI:** rejected. The installer never uses it, and an operator who
  needs it can pin the upstream digest.
- **Keep the crane container fallback:** rejected. Under rootless nerdctl it could not read the
  caller's registry login and hung on streamed output; installing one static binary is simpler.

## Consequences

- Publishing needs neither pip nor a matching Python, and no longer pulls every runtime image
  back to the workstation.
- Releases up to `0.1.0-rc.2` keep their native bundle, offline archive, and Temporal UI
  mirror; later releases do not have them.
- An air-gapped site cannot install from a release. An offline archive returns together with
  offline MAAS image support.
- Operators who start the diagnostics UI choose and pin its image themselves.
