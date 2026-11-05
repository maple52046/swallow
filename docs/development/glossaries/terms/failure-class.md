# Failure Class

- Bounded context: Automation (swallow-owned intent and execution).
- Definition: The classification of a Task failure, or of a condition a waiting Task observes, by whether doing the same thing again can succeed. It is a closed value set.
- Allowed meaning: Exactly these values, used verbatim in code and API contracts:
  - `transient` — the condition is expected to clear by itself (a host still booting, an API briefly unreachable, a provider still working); waiting longer or retrying the Task can succeed without any change.
  - `permanent` — the condition repeats until something changes (rejected credentials, a missing image, invalid input); a wait that observes it fails at once instead of spending its budget, and a retry succeeds only after the cause was fixed.
  - `unknown` — the Runner could not classify the failure, including an attempt whose outcome cannot be proven (its executor stopped before recording it).
  Every Task Diagnostic has one; a Runner that cannot tell uses `unknown`.
- Disallowed meaning: Not retryability: whether a user may retry a Task is the normalized error's `retryable`, and a `permanent` failure can still be retryable once the user fixed its cause. Not a severity, and not a Task status.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The Kubernetes API refused the Platform's credential: a `permanent` failure, so the health check failed at once instead of retrying for ten minutes." "The provisioner did not answer for a minute: `transient`; the wait kept polling within its budget."
- Related terms: Task Diagnostic (carries it), Task (its normalized error), Runner (classifies it).
- Change note: Added 2026-11-05 ([decision 077](../../../decisions/077-task-diagnostics-and-wait-policy.md) (Task diagnostics and wait policy)), so waits can fail fast on permanent conditions and users can tell a failure that may clear from one that needs a change.
