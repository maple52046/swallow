# Software Module

- Bounded context: swallow-wide (core and extensions).
- Definition: The Extension Module that provides one Managed Software kind — its variants, install methods, and install and uninstall automation.
- Allowed meaning: Each Managed Software kind in the catalog (Docker CE, Podman, NFS, Prometheus, Grafana, the Device Metrics Exporter) is provided by a Software Module. It can be installed on its own onto deployed Servers and composed as a Job into a Platform deployment, sharing the same automation and Software Assignment. It declares conflicting and required software in its Module Manifest.
- Disallowed meaning: Not a Platform Module (its deploy target is one software, not a multi-component runtime). Not the Software Assignment record.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The `podman` Software Module conflicts with `docker-ce`; a Slurm deploy composes it as a Job."
- Related terms: Extension Module, Managed Software, Software Assignment, Platform Module, Module Manifest.
- Change note: Added 2026-10-30 for [decision 070](../../../decisions/070-core-providers-and-extensions.md); the Managed Software catalog becomes Software Modules.
