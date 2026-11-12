# Workflow Template

- Bounded context: Automation.
- Definition: A module-declared, data-only description of Jobs and Tasks that the core compiles,
  with the targets and inputs of one request, into the plan of a Workflow.
- Allowed meaning: Declared in a Module Manifest (`templates`) under a name, with a version. It lists
  Jobs (name, label, retry granularity) and Tasks: an id, a name, a Job, dependencies and
  `continueOn`, a timeout, static variables, which of the request's targets the Task acts on (the
  target Servers, a Platform's access Servers, or its members), and its content as a type plus a
  reference (a `host-automation` content names a playbook in the release's allowlist and runs as
  the host automation Runner's Task; a `task` content names a registered Task kind). Compiling never runs anything: the resulting plan is accepted
  like any other Workflow (the plan catalog checks every Task kind, Runner, and Job), and a compiled
  plan can be built without being accepted. A Workflow compiled from a template records the module's
  definition and the template's version.
- Disallowed meaning: Not a Workflow (a template is compiled into one or into Jobs of one). Not a
  Workflow kind. Not host automation content (it references content). Not mutable database content.
- Synonyms: Template, when the context is clear.
- Deprecated terms: None.
- Examples: "The `slurm-acceptance` template compiles into one Job whose Task runs the
  `validate-slurm` playbook on the Platform's login node." / "A deployment composes a compiled
  template's Jobs after its own install Job."
- Related terms: Workflow, Job, Task, Module Manifest, Validation Suite.
- Change note: Added 2026-11-12 for [decision 081](../../../decisions/081-validation-suites-and-runs.md)
  (validation suites and runs); Validation Suites are its first users, and the existing deployment
  compositions written in Go can move to templates later.
