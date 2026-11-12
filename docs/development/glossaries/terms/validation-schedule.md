# Validation Schedule

- Bounded context: Validation.
- Definition: Desired state that a Validation Suite runs on its own against given targets: when a
  cron expression in a time zone comes due, or after a deployment of the target succeeds.
- Allowed meaning: Its spec names the suite, the targets (deployed Servers or a Platform), the
  parameters, the trigger (`cron` with a five-field expression and an IANA time zone, or
  `after-deployment`), and whether it is enabled; its status records when it runs next, its latest
  run, and its Conditions. Its Controller starts each Validation Run in `report` mode; a time missed
  while the worker was down runs once, a target still busy with other work skips that occurrence, and
  a schedule never has two runs at once. `after-deployment` follows a Platform target's deployments
  (each Platform generation applied) or a Server target's OS deployments.
- Disallowed meaning: Not a Validation Run, and not a gate: a deployment that composes a suite does so
  through its Platform module, not through a schedule. Not a Workflow schedule: swallow has no
  schedule for other Workflows.
- Synonyms: Schedule, when the context is clear.
- Deprecated terms: None.
- Examples: "A schedule runs `host` on every GPU Server at 02:00 Asia/Taipei." / "A schedule runs
  `kubernetes-smoke` after each deployment of the Platform."
- Related terms: Validation Suite, Validation Run, Desired State, Controller.
- Change note: Added 2026-11-12 for [decision 081](../../../decisions/081-validation-suites-and-runs.md)
  (validation suites and runs).
