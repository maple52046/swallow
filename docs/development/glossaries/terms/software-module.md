# Software Module

- Bounded context: swallow-wide (core and extensions).
- Definition: The Extension Module that provides one Managed Software kind — its variants, install methods, and install and uninstall automation.
- Allowed meaning: Each Managed Software kind in the catalog is provided by a Software Module; the catalog (`GET /api/v1/software/catalog`) is the only list of kinds. It can be installed on its own onto deployed Servers and composed as a Job into a Platform deployment, sharing the same automation and Software Assignment. Its id is its kind; its Module Manifest declares the kind's Workflow kinds and playbooks, and its Go package provides the kind's catalog entry (including conflicting and required software) through the `software` hook.
- Disallowed meaning: Not a Platform Module (its deploy target is one software, not a multi-component runtime). Not the Software Assignment record.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The `podman` Software Module conflicts with `docker-ce`; a Slurm deploy composes it as a Job."
- Related terms: Extension Module, Managed Software, Software Assignment, Platform Module, Module Manifest.
- Change note: Added 2026-10-30 for [decision 070](../../../decisions/070-core-providers-and-extensions.md) (core providers and extensions); the Managed Software catalog becomes Software Modules. 2026-11-01 ([decision 075](../../../decisions/075-module-contract-v1.md) (module contract v1)): the catalog is assembled from the Software Modules' `software` hooks; the core keeps no list of kinds.
