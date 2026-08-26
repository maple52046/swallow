# Operation

An **Operation** is a durable record of an operator's intent to run one versioned
playbook against a target set frozen at creation time. Swallow owns both the intent and
the execution lifecycle.

An operation's execution status is exactly one of `pending`, `running`, `succeeded`,
`failed`, `canceled`, or `indeterminate`. `indeterminate` means the API or host
stopped while a run held a lease and its outcome cannot be proven. Such a run is never
retried automatically.

Operations targeting one site execute one at a time. Operations for different sites may
run concurrently. Logs are retained in local persistent job artifacts; a run's task-level
events are derived from the runner's own record, so a long multi-phase operation can be
followed beyond a single status word. Operation metadata remains in MongoDB.

A failed operation may be retried only by an operator. A retry is a new operation with the
same kind, targets, and variables, linked to the original by `retryOfOperationId`. The
original is kept. Rerunning is safe because the mapped playbook is idempotent, which is a
property of the playbook rather than of swallow.
