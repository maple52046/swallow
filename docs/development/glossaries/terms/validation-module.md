# Validation Module

- Bounded context: swallow-wide (core and extensions).
- Definition: The Extension Module kind (`validation`) that offers Validation Suites belonging to no
  Platform, GPU vendor, or piece of software — checks of hosts and of the network between them — and
  the runner of user-defined suites.
- Allowed meaning: Declares its suites, their Workflow Templates, and the playbooks that judge them in
  its Module Manifest, like any module; it manages no resource of its own and has no spec. A suite of a
  Platform runtime, a GPU vendor, or a piece of software belongs to that Platform, Accelerator, or
  Software Module instead.
- Disallowed meaning: Not the Validation bounded context of the core (which accepts runs, records
  their results, and runs schedules), and not a place for Platform- or vendor-specific checks.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The `host` Validation Module checks time sync and that the observed CPU, memory, and disks
  match the inventory." / "User-defined suites run through the `script` Validation Module's playbook."
- Related terms: Extension Module, Validation Suite, Module Manifest, Platform Module, Accelerator
  Module, Software Module.
- Change note: Added 2026-11-12 for [decision 081](../../../decisions/081-validation-suites-and-runs.md)
  (validation suites and runs), which added it as the fourth module kind.
