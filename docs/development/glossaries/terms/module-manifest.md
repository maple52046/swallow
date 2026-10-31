# Module Manifest

- Bounded context: swallow-wide (core and extensions).
- Definition: The data declaration by which an Extension Module describes itself to the core, which loads modules by reading it.
- Allowed meaning: Declares the module's kind, id, version, and contract version; its spec schema and feature switches; Job and Task templates; status rules; required provider capabilities; compatible Accelerator Modules (for a Platform Module) or conflicting and required software (for a Software Module); the destructive classification of its spec fields; and its host automation content as a content type plus a reference, which the release allowlist must also list. The core refuses a manifest whose contract version it does not support.
- Disallowed meaning: Not the release's playbook allowlist (`api-server/modules/manifest.json`), which it references. Not executable code, and not mutable database content.
- Synonyms: Manifest, when the context is clear.
- Deprecated terms: None.
- Examples: "The Slurm Module Manifest references its playbook as host automation content, which the Ansible adapter runs; the manifest itself does not assume Ansible."
- Related terms: Extension Module, Module Package, Platform Module, Accelerator Module, Software Module.
- Change note: Added 2026-10-30 for [decision 070](../../../decisions/070-core-providers-and-extensions.md); the contract version exists from stage 1.
