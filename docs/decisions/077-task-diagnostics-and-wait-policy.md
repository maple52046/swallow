# 077. Task diagnostics and wait policy: durable, redacted, actionable failures and bounded, classified waits

- Status: Accepted
- Date: 2026-11-05
- 現行規格：[workflows 契約](../../api-server/docs/development/api-contracts/api-server/workflows.md)（Task errors and diagnostics）；[workflow-execution.md](../development/workflow-execution.md) §5.5；glossary [Task Diagnostic](../development/glossaries/terms/task-diagnostic.md)、[Failure Class](../development/glossaries/terms/failure-class.md)

## Context

A Task's failure was a normalized error and nothing else: a code, a message, whether it may be
retried, and an optional stage. Runners and provider adapters turned every cause into a fixed
sentence — a Platform health check reported that it could not reach the cluster API, without saying
which endpoint it called, what answered, or whether it would ever answer — so neither the user nor an
administrator could tell what to do next without reading the worker's host. Tasks that wait on
something slow (an OS deployment, a host coming up, a cluster becoming healthy) kept what they
observed in local variables, so a Task that waited thirty minutes showed nothing until it failed. The
worker logged no Task outcomes, so a failure could not be followed from the API request that started
its Workflow. The dashboard could show only the code and the message.

Redaction existed in one place: the OS provisioning adapter dropped every installation-log line
that mentioned a credential and removed URL userinfo. Dropping a line erases the cause with the
secret. The Workflow's intent snapshot drops sensitive keys, which is right for intent but not a way
to explain a failure.

Waits had no common rule: each Runner chose its own budget, polled until the budget ended whatever it
saw, and reported the same message for a provider that was still working and one that had rejected
the request for good (inferred from the Runners' code at the time).

## Decision

1. **A failure carries a Task Diagnostic, and it is part of the product contract.** The Task's
   normalized error gains a structured diagnostic in two layers. The user layer says what happened
   (the error's message), who is affected (the target among the Task's resources), and what to do
   next (the remediation). The admin layer is the evidence: the Failure Class, the endpoint and the
   provider or platform operation that failed, the cause as a causal chain, the attempts, elapsed
   time, and budget of the wait that gave up, its last observation, and the events the provider
   recorded about the target. The code and `retryable` stay the only values a client or a policy
   branches on.
2. **Redaction removes secrets, not causes.** Secrets are masked by their shape — URL userinfo and
   query values, bearer tokens and JSON Web Tokens, Authorization headers, credential key-value
   pairs, PEM private keys — by one shared helper (`internal/shared/redact`) that every layer may
   use, and the text around them is kept. The core Workflow engine, the only writer of execution
   state, redacts and bounds every error and every wait observation before it stores it, so no Runner
   can store a secret by forgetting to redact. A rule that knows one vendor's secret format stays in
   that adapter or module, in addition to the shared one.
3. **Diagnostics are durable.** The diagnostic is stored with the Task in the store and served by the
   API on every read of the Workflow, not only logged. The Task keeps its latest error; the timeline
   keeps each attempt's code, message, Failure Class, and endpoint. Records written before have no
   diagnostic and read as none, so no migration is needed.
4. **Outcomes are correlated in structured logs.** The engine logs every Task outcome once, after its
   transition committed, with the Workflow, Task, attempt, kind, Runner, status, and for a failure its
   code, Failure Class, and endpoint, plus the Workflow's request correlation (the ID of the API request
   that accepted it). The API's request log records the status the client actually received.
5. **Every wait follows one policy.** A Runner that waits declares, for each wait:
   - its observation source — what it reads, and from where;
   - a classifier that maps each observation and each error to a Failure Class;
   - a budget — how long it may wait before the Task fails.
   While it waits it publishes each observation as the Task's wait observation, through the core, so
   the API shows what the Task waits for. A `permanent` observation fails the Task at once instead of
   spending the budget. A `transient` one keeps the wait going within the budget; `unknown` is waited
   on like `transient` and reported as `unknown`. When the budget ends, the Task fails with a
   diagnostic that carries the wait's attempts, elapsed time, budget, and last observation.
   Postcondition checks — confirming that a state an earlier Task produced holds, such as a
   Platform's health after its deployment — get short budgets: a postcondition that does not hold
   soon is a failure to report, not progress to wait for.
   A failed read of the observation source is not an observation of what the Task waits for. It is
   classified the same way: a `permanent` one ends the wait at once, and the others are tolerated
   within a budget of their own that a successful read resets. A wait whose external work keeps
   running after it ends — a provider still deploying a Server — ends as `requires_attention`
   rather than `failed`, because that work's outcome is not proven.
6. **Publication is bound to the attempt.** The activity that runs an attempt hands its Runner a
   publisher bound to the Workflow, dispatch generation, Task, and attempt; the write is a targeted
   update guarded like the Ansible Runner's live progress, so a superseded run or an ended attempt
   writes nothing, and every transition that ends, retries, begins, or cancels the attempt clears it.
7. **A plan may bound its own time and its waits for the user.** A Task may declare a timeout: an
   Ansible Task that exceeds it is stopped like a canceled one and fails with `task_budget_exceeded`,
   its diagnostic carrying the budget and the elapsed time. A Workflow may declare a wall-clock
   budget, after which swallow cancels it, and may declare itself unattended: a failure, retryable or
   not, and an outcome that cannot be proven settle at once, so the Workflow ends and releases its
   resources instead of parking in `requires_attention` for a user who is not watching. Validation
   runs ([decision 081](081-validation-suites-and-runs.md) (validation suites and runs)) declare all
   three; a deployment declares none and parks as before.
8. **A module classifies its own failures.** A module declares the codes its host automation fails
   with (`failureCodes`: the `[code]` a role's failure message starts with), each with a Failure Class,
   a remediation, and optionally that a retry cannot help; the Ansible Runner applies the declaration
   to the diagnostic of a failed run whose message carries the code. The error's code stays
   `ansible_failed`.

## Alternatives considered

- **Keep the detail in logs only.** Rejected: logs are per process, rotate, and are not a product
  surface; the user deciding whether to retry needs the explanation on the Task.
- **Return the provider's raw error text.** Rejected: it carries credentials and internal detail and
  is rarely actionable; the cause is kept, redacted, in the admin layer instead.
- **Drop any line that mentions a secret.** Rejected as the general rule because it erases the cause
  with the secret; an adapter may still drop a line its own format makes unsafe to keep.
- **A separate diagnostics collection.** Rejected: a second record of a Task's failure needs its own
  writer and transaction scope, while the Task already carries its error; bounding the diagnostic's
  size keeps the Workflow document small.
- **Keep every observation of a wait.** Rejected: unbounded growth for little value; the latest
  observation is published while waiting, the final numbers go into the diagnostic, and the provider's
  own events supply the history.
- **Retry automatically on a transient failure.** Rejected: a Task is retried only when the user asks
  (decision 072); the Failure Class decides how a wait behaves within one attempt and informs the user.

## Consequences

- The workflows contract gains `error.diagnostic` and Task `waiting`, both additive and `null` when
  absent; the dashboard shows the user layer first and the evidence behind a disclosure, and its
  timeline shows each attempt's failure.
- Runners and provider adapters must classify their failures and build diagnostics instead of fixed
  sentences, and their waits must declare an observation source, a classifier, and a budget. Existing
  Runners adopt this one by one; until then their errors read without a diagnostic.
- Error messages are stored redacted, so a message that used to echo a credential shows a mask.
- A published observation is one targeted store write per poll of a wait; Runners publish at most
  once per observation, so the write rate follows their poll intervals.

## Revision history

- 2026-11-05: a failed read of a wait's observation source is classified and tolerated within its own budget, and a wait whose external work keeps running ends as `requires_attention`.
- 2026-11-12: a Task may declare a timeout and a Workflow a budget and unattended execution; modules declare the Failure Class and remediation of their failure codes (decision 081).
