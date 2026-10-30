# 027. Swallow drives provider-owned OS image upload as an optional capability

- Status: Accepted
- Date: 2026-09-13
- 現行規格：[os-provisioning.md](../development/os-provisioning.md) §4 OS Image：目錄、上傳與刪除

## Context

[ADR 001](001-system-ownership-boundaries.md) settled that swallow integrates a system that
already owns a capability rather than rebuilding it. [ADR 009](009-deployment-template-ownership.md)
applied that to OS provisioning: OS Images are read live from the provider and never mirrored,
and swallow owns only reusable deployment intent. The OS Image glossary term went further and
said swallow "does not own, mirror, upload, or synchronize the artifact".

swallow already **drives** provider-owned image actions it does not itself own the artifact for:
image deletion removes an operator-uploaded custom image through an optional provider
capability, refusing the request when the provisioner does not offer it. The missing counterpart
is creation: an operator can build a custom OS image (for example a ROCm-enabled Ubuntu root
tarball) but has no way to get it into the site's provisioner through swallow, so they drop to
the provider's own CLI/UI and swallow's catalog only sees the result after the fact.

Two things make this more than "add an endpoint". First, the glossary explicitly forbade
"upload", so allowing it is a deliberate model change, not an implementation detail. Second, a
provider-owned image artifact is large (multi-GB), and MAAS's create protocol needs the content's
`sha256` and `size` up front and then accepts the bytes in chunks — so the transport boundary
(who holds the bytes, who computes the hash) is a real decision.

A further constraint the operator raised: **whether an uploaded artifact is a "custom image" is
not the caller's choice — it depends on the site's provisioner, and swallow's backend must decide
it.** For MAAS an uploaded boot resource surfaces as `osSystem: custom`; another provider might
classify it differently or not at all.

## Decision

**swallow may drive a provisioner to upload a new OS image from operator-supplied content, as an
optional provider capability, without ever owning, mirroring, or keeping a durable copy of the
artifact. The uploaded image is provider-owned exactly like a synced one, and its classification
(such as "custom") is determined by the provider adapter, not by the caller.**

1. **Upload is an optional provider capability, symmetric to delete.** A provisioner whose
   adapter does not implement upload refuses the request as a provider rejection, never a
   silent success. It joins delete as an operation-side capability of the
   [ADR 025](025-provider-data-overlay.md) capability model.
2. **The classification is provider-determined.** The API and the dashboard never send or infer a
   "custom" flag; the provider adapter reports it.
3. **The bytes stream browser → api-server → provider; swallow keeps no copy.** api-server spools
   the file only long enough to compute the `size` and `sha256` the provider requires up front,
   then streams it to the provider in chunks and deletes the spool. swallow never holds the whole
   artifact in memory and never persists it.
4. **One synchronous provider-backed request, not a durable Operation.** Its result is the created
   image. It must not be capped by the short provider read timeout used for catalog reads.

## Alternatives considered

- **Keep the status quo (no upload; use the provider CLI/UI).** Rejected: it leaves a real
  operator need unmet and splits image lifecycle across two tools while swallow already owns the
  symmetric delete.
- **Let the caller declare the image "custom".** Rejected per the operator constraint: custom-ness
  is a property of the provisioner, so the backend must decide it. A caller-supplied flag would let
  the dashboard and API disagree with what the provider actually does.
- **Have api-server store/mirror the artifact (a swallow image registry).** Rejected: it violates
  ADR 001/009 — the artifact is provider-owned and live-read — and turns swallow into a large
  binary store it has no reason to own.
- **Import from an operator-supplied URL instead of streaming a file.** Rejected for the first
  version: it moves fetch responsibility and network trust into api-server for no provider that
  needs it today; MAAS custom-image upload is a content upload, not a source import.
- **Trust a browser-computed sha256 and stream straight through without spooling.** Rejected as
  the default: hashing a multi-GB file in the browser is heavy and unverifiable; spooling to a
  temp file lets api-server compute the hash it sends and stream chunks deterministically. (The
  provider still verifies the hash on completion.)

## Consequences

- The provisioning API gains its first `multipart/form-data` endpoint, and api-server must accept
  multi-GB request bodies as a stream.
- The OS Image glossary term changes: swallow may drive a provider-owned upload; the artifact
  stays provider-owned and its custom classification is provider-determined.
- Provider refusals (unsupported provisioner, duplicate name, unsupported file type) reach the
  operator in the provider's own wording.
