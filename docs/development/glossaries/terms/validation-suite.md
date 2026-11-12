# Validation Suite

- Bounded context: Validation.
- Definition: A named test plan swallow can run against a target to judge whether it works as
  expected — a set of Validation Checks, the kind of target it applies to (`servers`, one or more
  deployed Servers, or `platform`, one Platform entered through its access Servers), its Validation
  Impact, the parameters it accepts, and how long a run may take.
- Allowed meaning: Either swallow-defined, declared by an Extension Module in its Module Manifest
  (`validations`) together with the Workflow Template that runs it — the way swallow expects users to
  validate — or user-defined: a script an admin writes, kept as immutable revisions, which swallow runs
  on each target Server and judges by its exit code (one check, `script`). Its source is `module` or
  `user`. A module suite may be run on its own, on a Validation Schedule, or composed into a
  deployment as a gate; a user suite never as a gate. A module suite may be experimental: offered as
  a preview whose checks may still change, run like any other, and hidden by the dashboard unless the
  user shows experimental suites. "Test plan" (測試方案) in the dashboard is this term.
- Disallowed meaning: Not a Workflow or a Workflow kind (a suite is run by one). Not a Validation Run
  (one execution of a suite). Not OS Image verification (proving a provider can deploy an image), not
  the Time Sync Check of a Platform deployment, not a deployment's own postcondition (a readiness
  check inside its Tasks), and not the provisioner's hardware testing. Not request validation
  (`validation_error`). Not host automation content: a user suite's script is data, never added to the
  release's playbook allowlist.
- Synonyms: Suite, when the context is clear; test plan (UI).
- Deprecated terms: Test case, test suite (as names for this concept).
- Examples: "The Slurm module's `slurm-acceptance` suite runs a job across every compute node, two
  jobs sharing a node, a rootless container step, MPI across nodes, and the GPU probes through Slurm."
  / "An admin saves a user suite whose script checks that `/scratch` is mounted, and runs it on four
  Servers."
- Related terms: Validation Check, Validation Run, Validation Impact, Validation Schedule, Validation
  Module, Workflow Template, Extension Module.
- Change note: Added 2026-11-12 for [decision 081](../../../decisions/081-validation-suites-and-runs.md)
  (validation suites and runs). The user chose "suite" over "test case" and "test suite"; "validation"
  is distinct from `validation_error` and from OS Image verification. The same day the user asked for
  experimental suites, so the checks written beside the deployment's own could be redesigned.
