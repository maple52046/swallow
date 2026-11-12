# Validation Run

- Bounded context: Validation.
- Definition: One execution of a Validation Suite against its targets, executed by a Workflow, and
  swallow's durable record of what was asked, what ran, and every Validation Check's Validation
  Verdict on every target.
- Allowed meaning: An action, not desired state: its request — the suite (for a user suite, the
  revision, its sha256, and the script it ran), the targets, the parameters, the mode, and the trigger
  — is written once when it is accepted; each check's verdict on each target is observed from the
  Task that judged it; its status and verdict are derived. Status: `pending` (accepted, not started),
  `running`, `completed` (every check has a verdict), or `canceled`. Mode: `report` — its own Workflow
  (kind `run-validation`), unattended, with a budget, and preemptible unless the suite is
  `disruptive` — or `gate` — Jobs composed into a deployment's Workflow, whose final Task fails the
  deployment when a gating check does not pass. Trigger: `manual` (a user, from the dashboard or the
  CLI), `schedule` (a Validation Schedule, with the deployment that set it off when its trigger is
  `after-deployment`), or `deployment` (a deployment that composed the suite as a gate). Kept for a
  retention period (90 days by default).
- Disallowed meaning: Not the Workflow itself: a run's verdict can be `failed` while its Workflow
  succeeded, and a gate run shares its deployment's Workflow. Not a Workflow's Task output (the
  Workflow keeps none). Not a record of drift.
- Synonyms: Run, when the context is clear.
- Deprecated terms: Test run.
- Examples: "The run of `kubernetes-smoke` on Platform `prod-k8s` completed with verdict `passed` in
  two minutes." / "A Release of one of its Servers preempted the run, which ended `canceled`." / "The
  Slurm deployment's gate run failed `gpu-gres` on one node, so the deployment parked."
- Related terms: Validation Suite, Validation Check, Validation Verdict, Validation Schedule,
  Workflow, Task.
- Change note: Added 2026-11-12 for [decision 081](../../../decisions/081-validation-suites-and-runs.md)
  (validation suites and runs).
