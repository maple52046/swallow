# Deploy Target

- Bounded context: OS Provisioning.
- Definition: Where an OS Deployment runs — `disk` (installed onto the machine's
  disk) or `ram` (run from memory, leaving the disks untouched).
- Allowed meaning: The canonical operator-facing vocabulary for the deployment
  mode. `ram` is exactly the fact the wire, MAAS adapter, MongoDB, durable
  snapshot, and the k0s var still call `ephemeral`; Deploy Target and `ephemeral`
  map one-to-one at every boundary (`disk ↔ ephemeral=false`,
  `ram ↔ ephemeral=true`). Deploy requests accept an optional `deployTarget`
  (preferred) that maps onto the internal `ephemeral` flag, with `ephemeral` kept
  as a still-accepted deprecated alias. The dashboard label for `ram` is
  "RAM deploy (ephemeral)". A custom OS Image is verified (or not) per Deploy
  Target: an image can be trusted for `ram` but not `disk`.
- Disallowed meaning: A new provider concept, a rename of the stored/wire
  `ephemeral` fields, a Server placement (Zone/Pool), or a Deployment Template's
  network intent. It is the deployment mode only.
- Synonyms: Deploy mode; disk deploy; RAM deploy.
- Deprecated terms: "Ephemeral" as the sole operator-facing name for the
  memory-backed mode (still the wire/BSON/MAAS/k0s field name, and the diagnostic
  meaning in Rescue Mode is separate).
- Examples: An operator verifies a custom image for the `ram` target, then for
  the `disk` target; the OS Images list shows Disk and RAM verified badges. A
  deploy wizard offers "Disk deploy" and "RAM deploy (ephemeral)" and submits
  `deployTarget`.
- Related terms: OS Deployment, OS Image, Deployment Template, Rescue Mode.
- Change note: Added 2026-09-21 with the OS image verification mechanism per
  [decision 035](../../../decisions/035-os-image-verification-and-deploy-target.md),
  introducing `disk | ram` as the canonical deploy-mode vocabulary layered over
  the existing `ephemeral` boundary fields.
