# Validation Suite

- Bounded context: Validation.
- Definition: A named test plan swallow can run against a target to judge whether it works as
  expected — a set of Validation Checks, the kind of target it applies to (`servers`, one or more
  deployed Servers, or `platform`, one Platform entered through its access Servers), its Validation
  Impact, the parameters it accepts, and how long a run may take.
- Allowed meaning: Either swallow-defined, declared by an Extension Module in its Module Manifest
  (`validations`) together with the Workflow Template that runs it — the way swallow expects users to
  validate — or user-defined: a script any User writes and shares with every User, kept as immutable
  revisions, which swallow runs on each target Server and judges by its exit code (one check,
  `script`). Its source is `module` or `user`. A user suite has an author, the User who created it:
  only the author changes it, and the author or an admin retires it. It runs on each target as the
  Linux account of the User who starts the run, never as its author, the automation user, or root;
  it states whether that User runs the script through passwordless `sudo`, and its starter level: the
  least Pool Role (`owner`, the default, or `member`) a User needs on every target to start it. A
  module suite is started by Pool owners and runs as root. A module suite may be run on its own, on a
  Validation Schedule, or composed into a deployment as a gate; a user suite only on its own, never
  as a gate or on a schedule. A module suite may be experimental: offered as
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
  / "`alice` saves a user suite whose script checks that `/scratch` is mounted, with starter level
  `member`; `bob`, a member of another Pool, reviews it and runs it on four of his Pool's Servers,
  where it runs as `bob`."
- Related terms: Validation Check, Validation Run, Validation Impact, Validation Schedule, Validation
  Module, Workflow Template, Extension Module, Pool Role (the values of a starter level), Host Access
  (checked for the User who starts a user suite).
- Change note: Added 2026-11-12 for [decision 081](../../../decisions/081-validation-suites-and-runs.md)
  (validation suites and runs). The user chose "suite" over "test case" and "test suite"; "validation"
  is distinct from `validation_error` and from OS Image verification. The same day the user asked for
  experimental suites, so the checks written beside the deployment's own could be redesigned.
  Changed 2026-11-17: Bill wanted every User to create and share user suites and to run them under
  their own account. The earlier text said an admin writes them, and the specs said only admins run
  them while the code let Pool owners run them; both are replaced by the author, the starter level,
  and the starter's account. The starter level is part of this term rather than a term of its own,
  because its values are Pool Role values.
