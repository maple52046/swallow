# Extension Module

- Bounded context: swallow-wide (core and extensions).
- Definition: A unit that adds something swallow can manage — a Platform runtime, a GPU vendor's accelerators, or a piece of host software — and plugs into the core through the module contract.
- Allowed meaning: One of three kinds: Platform Module, Accelerator Module, or Software Module. A module declares itself in a Module Manifest (with a contract version), provides its spec schema, Job and Task templates, status rules, and host automation content, and may add Go hooks only where data cannot express the logic, each with a stated reason. Modules depend on each other only through the contract. In the first stage modules live in this repository, one directory per module with a named owner, the directory is the module's package format, and the team and outside contributors develop them together. Stage 2 adds per-installation module lifecycle through Module Packages; stage 3, only needed if a module is owned outside the project, adds signing and out-of-process hooks.
- Disallowed meaning: Not a Provider Adapter (that says how swallow talks to an external system). Not a Go package that the core imports, and not a place for code the core depends on. Not a component of the repository in the `codebase-structure` sense.
- Synonyms: Module.
- Deprecated terms: None.
- Examples: "Slurm is a Platform Module; AMD is an Accelerator Module; Docker CE is provided by a Software Module." / "An outside developer adds a storage Platform by creating a new module directory in this repository, reviewed by its owner."
- Related terms: Platform Module, Accelerator Module, Software Module, Managed Software, Module Manifest, Module Package, Support Matrix, Provider Interface.
- Change note: Added 2026-10-30 for [decision 070](../../../decisions/070-core-providers-and-extensions.md), which moves Platform-specific and GPU-vendor-specific logic out of the core.
