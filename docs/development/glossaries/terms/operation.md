# Operation

An **Operation** is a durable record of an operator's intent to run one versioned
playbook against a target set frozen at creation time. Swallow owns both the intent and
the execution lifecycle.

An operation's execution status is exactly one of `pending`, `running`, `succeeded`,
`failed`, `canceled`, or `indeterminate`. `indeterminate` means the API or host
stopped while a run held a lease and its outcome cannot be proven. Such a run is never
retried automatically.

Operations targeting one site execute one at a time. Operations for different sites may
run concurrently. Logs are retained in local persistent job artifacts; operation
metadata remains in MongoDB.
