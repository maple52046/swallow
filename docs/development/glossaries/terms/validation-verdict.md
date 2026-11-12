# Validation Verdict

- Bounded context: Validation.
- Definition: The outcome of a Validation Check on one target, and the outcome a Validation Run
  derives from all of its checks.
- Allowed meaning: For a check on a target:
  - `passed` — the check ran and its expectation holds.
  - `failed` — the check ran and its expectation does not hold.
  - `error` — the check could not be judged: the host could not be reached, a tool it needs is
    missing, it timed out, or its Task ended without a result.
  - `skipped` — the check does not apply to this target (for example a GPU check on a Server without
    a GPU, or a feature the Platform did not enable).
  For a run, derived when it completes: `failed` when any check failed, else `error` when any check
  is `error`, else `passed` (a run whose every check was skipped is `passed`). A run has no verdict
  until it completes, and a canceled run has none.
- Disallowed meaning: Not a Workflow or Task status: a Validation Run whose checks failed still has a
  Workflow that succeeded. Not a Failure Class. Never guessed: what swallow could not judge is
  `error`, not `failed` or `passed`.
- Synonyms: Verdict, when the context is clear.
- Deprecated terms: None.
- Examples: "The script exited 3, so its verdict is `failed`." / "The run's verdict is `error`: one
  Server was unreachable, and every check on the others passed."
- Related terms: Validation Check, Validation Run, Failure Class, Task.
- Change note: Added 2026-11-12 for [decision 081](../../../decisions/081-validation-suites-and-runs.md)
  (validation suites and runs).
