# Module Package

- Bounded context: swallow-wide (core and extensions).
- Definition: A bundle of one Extension Module that can be installed or updated on an existing installation without upgrading swallow — a stage 2 design; stage 3 extends it to modules owned outside the project.
- Allowed meaning: In stage 2, a package is produced by this project from a module directory, carries its Module Manifest and host automation content, is checked for contract compatibility with the installed core, and can be bundled for offline sites. It cannot change Go hooks, which are compiled into swallow; a Go hook change still needs a swallow upgrade. Stage 2 starts when customers need a module combination or update cadence that differs from swallow's releases. In stage 3, packages may come from outside the project, which adds signing and trust, forbids Go hooks, and relies on out-of-process hooks; it starts when someone outside the project needs to develop and release a module independently. Until then the term names the design only.
- Disallowed meaning: Not how contributors work on swallow — they add and change module directories in this repository (stage 1). Not a release artifact of swallow itself, and not something stage 1 supports: in stage 1 the module directory is the package format and every module ships inside a release.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "A customer needs an updated Slurm playbook before the next swallow release; stage 2 would let the project ship it as a Module Package." / "A hardware vendor maintaining its own Accelerator Module on its own schedule would need stage 3."
- Related terms: Extension Module, Module Manifest, Support Matrix.
- Change note: Added 2026-10-30 for [decision 070](../../../decisions/070-core-providers-and-extensions.md); the same day the stages were reallocated so per-installation lifecycle is stage 2 and signing and outside ownership are stage 3.
