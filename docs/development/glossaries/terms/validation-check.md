# Validation Check

- Bounded context: Validation.
- Definition: One named judgement a Validation Suite makes about each of its targets, recorded in a
  Validation Run as a Validation Verdict for every target it was judged on.
- Allowed meaning: Declared by its suite with an id, a title, and whether it is `gating`: in a
  deployment that composes the suite as a gate, a gating check that does not pass fails the
  deployment, while a non-gating one only reports. A result may carry a summary, measurements (a name,
  a value, a unit, and the expected value), and evidence (a bounded, redacted excerpt of what the host
  printed). A user suite has the single check `script`.
- Disallowed meaning: Not a Task (a check is judged inside a Task, and one Task may judge several
  checks on several targets). Not a Condition of a resource.
- Synonyms: Check, when the context is clear.
- Deprecated terms: None.
- Examples: "The `gpu-gres` check failed on two of the eight compute nodes; the others passed." / "The
  `srun-all-nodes` check is not gating: a deployment still succeeds when it fails."
- Related terms: Validation Suite, Validation Verdict, Validation Run.
- Change note: Added 2026-11-12 for [decision 081](../../../decisions/081-validation-suites-and-runs.md)
  (validation suites and runs).
