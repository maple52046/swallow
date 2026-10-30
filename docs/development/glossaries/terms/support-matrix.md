# Support Matrix

- Bounded context: swallow Delivery.
- Definition: The per-release record of which Provider Adapters and Extension Modules a swallow release includes, at which versions, and which combinations were validated.
- Allowed meaning: What a customer receives is the core plus the adapters and modules in the Support Matrix; only the combinations it lists as validated (for example the Slurm Platform Module with the AMD Accelerator Module and the MAAS adapter) may be claimed as supported. It is produced from the same configuration that selects a release's modules, never written by hand separately.
- Disallowed meaning: Not a compatibility guess or a roadmap. Not the release manifest's image digests.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "Kubernetes with an NVIDIA Accelerator Module is not in this release's Support Matrix, so it is not supported."
- Related terms: Extension Module, Provider Adapter, Installation, Module Package.
- Change note: Added 2026-10-30 for [decision 070](../../../decisions/070-core-providers-and-extensions.md): the product is a distribution of the generic core.
