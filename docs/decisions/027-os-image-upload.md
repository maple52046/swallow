# 027. Swallow drives provider-owned OS image upload as an optional capability

- Status: Accepted
- Date: 2026-09-13
- 現行規格：[os-provisioning.md](../development/os-provisioning.md) §4 OS Image：目錄、上傳與刪除

## Context

[ADR 069](069-swallow-owns-desired-state.md) settled that swallow integrates a system that
already owns a capability rather than rebuilding it. [ADR 009](009-deployment-template-ownership.md)
applied that to OS provisioning: OS Images are read live from the provider and never mirrored,
and swallow owns only reusable deployment intent. The OS Image glossary term went further and
said swallow "does not own, mirror, upload, or synchronize the artifact".

swallow already **drives** provider-owned image actions it does not itself own the artifact for:
image deletion removes a user-uploaded custom image through an optional provider
capability, refusing the request when the provisioner does not offer it. The missing counterpart
is creation: a user can build a custom OS image (for example a ROCm-enabled Ubuntu root
tarball) but has no way to get it into the site's provisioner through swallow, so they drop to
the provider's own CLI/UI and swallow's catalog only sees the result after the fact.

Two things make this more than "add an endpoint". First, the glossary explicitly forbade
"upload", so allowing it is a deliberate model change, not an implementation detail. Second, a
provider-owned image artifact is large (multi-GB), and MAAS's create protocol needs the content's
`sha256` and `size` up front and then accepts the bytes in chunks — so the transport boundary
(who holds the bytes, who computes the hash) is a real decision.

A further constraint the user raised: **whether an uploaded artifact is a "custom image" is
not the caller's choice — it depends on the site's provisioner, and swallow's backend must decide
it.** For MAAS an uploaded boot resource surfaces as `osSystem: custom`; another provider might
classify it differently or not at all.

## Decision

**swallow may drive a provisioner to upload a new OS image from user-supplied content, as an
optional provider capability, without ever owning, mirroring, or keeping a durable copy of the
artifact. The uploaded image is provider-owned exactly like a synced one, and its classification
(such as "custom") is determined by the provider adapter, not by the caller.**

1. **Upload is an optional provider capability, symmetric to delete.** A provisioner whose
   adapter does not implement upload refuses the request as a provider rejection, never a
   silent success. It joins delete as an operation-side capability of the
   [ADR 071](071-provisioner-realized-desired-state.md) capability model.
2. **The classification is provider-determined.** The API and the dashboard never send or infer a
   "custom" flag; the provider adapter reports it.
3. **The caller declares the content, then streams it through api-server to the provider; swallow
   keeps no copy.** The client (CLI or browser) computes the `size` and `sha256` the provider
   requires up front and records them, with the rest of the intent, as a short-lived, single-use
   OS Image upload; nothing reaches the provider yet. It then sends the raw content to that upload,
   and api-server forwards it to the provider as it arrives — no spool, no second read, never the
   whole artifact in memory, nothing persisted. The provider verifies the content against the
   declared digest.
4. **The content request is synchronous and succeeds only when the provider has staged the
   image; it is not a durable Operation.** Its result is the created image, returned once the
   provider reports it complete (bounded wait), so a success can be deployed. It must not be capped
   by the short provider read timeout used for catalog reads. A request that fails after the
   provider created the image removes the incomplete image on a best-effort basis.

## Alternatives considered

- **Keep the status quo (no upload; use the provider CLI/UI).** Rejected: it leaves a real
  user need unmet and splits image lifecycle across two tools while swallow already owns the
  symmetric delete.
- **Let the caller declare the image "custom".** Rejected per the user constraint: custom-ness
  is a property of the provisioner, so the backend must decide it. A caller-supplied flag would let
  the dashboard and API disagree with what the provider actually does.
- **Have api-server store/mirror the artifact (a swallow image registry).** Rejected: it violates
  ADRs 069 and 009 — the artifact is provider-owned and live-read — and turns swallow into a large
  binary store it has no reason to own.
- **Import from a user-supplied URL instead of streaming a file.** Rejected for the first
  version: it moves fetch responsibility and network trust into api-server for no provider that
  needs it today; MAAS custom-image upload is a content upload, not a source import.
- **Spool the upload in api-server and compute the hash there.** The first version did this:
  the file was written to api-server's temp directory, hashed, and only then forwarded. Replaced:
  the three steps ran one after another, the client waited without a response while the image was
  forwarded, and the temp directory had to hold the whole image — in the production installation it
  is a tmpfs, so the image occupied host memory and an image larger than half of it failed. A hash
  the caller declares is verifiable, because the provider compares it with what it received;
  hashing in the browser is affordable with an incremental hasher.
- **Hand the client the provider's address and credential so it uploads directly (a redirect).**
  Rejected: MAAS authenticates with OAuth PLAINTEXT and has no upload-scoped or pre-signed
  credential, so the client would hold the Integration's administrator key (even a per-upload token
  has the account's full rights until revoked), bypassing swallow's policy and Server Lock; the CLI
  would have to speak the provider's upload protocol; the provider is often reachable only from
  swallow's network; and browsers cannot upload to MAAS, which sends no CORS headers. The upload
  record keeps the door open: a future provider with scoped upload targets could receive the
  content directly without changing the intent step.

## Consequences

- The provisioning API carries a multi-gigabyte raw request body, so api-server reads request
  bodies as a stream; it accepts no multipart body at all, so the framework's pre-handler multipart
  parsing (which would spool to disk before authentication) is off. Uploads are stored briefly
  between their two requests, so any API process can serve the content request.
- The OS Image glossary term changes: swallow may drive a provider-owned upload; the artifact
  stays provider-owned and its custom classification is provider-determined.
- Provider refusals (unsupported provisioner, duplicate name, unsupported file type) reach the
  user in the provider's own wording.

## Revision history

- 2026-11-02: referred to the people who use swallow as users instead of operators; the decision is unchanged.
- 2026-11-05: replaced the single multipart request, spooled and hashed in api-server, with a declared, single-use upload whose content api-server forwards as it arrives, succeeding only once the provider has staged the image; recorded why the client does not upload to the provider directly.
