# Platform Module

- Bounded context: swallow-wide (core and extensions).
- Definition: The Extension Module that makes one kind of Platform runtime manageable — today Kubernetes (k0s) and Slurm.
- Allowed meaning: Provides the Platform's spec schema, topology and Node Roles, the Job and Task composition for deploy, uninstall, and day-2 work, status read-back (membership, connectivity, feature states such as GPU Operator State), the runtime's live reads, and which Destructive Changes its spec fields cause. It obtains GPU settings from an Accelerator Module and composes Software Modules as Jobs.
- Disallowed meaning: Not the Platform itself (a Platform is a managed runtime instance; the module is what makes that kind of runtime possible). Not where GPU-vendor logic lives.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The Slurm Platform Module asks the AMD Accelerator Module for the GRES configuration instead of knowing AMD itself."
- Related terms: Extension Module, Platform, Accelerator Module, Managed Software, Module Manifest, Node Role.
- Change note: Added 2026-10-30 for [decision 070](../../../decisions/070-core-providers-and-extensions.md).
