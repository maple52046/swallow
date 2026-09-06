# Server Status

- Bounded context: swallow-wide, authoritative for `api-server` and `dashboard`.
- Definition: A server has **no single status**. Its condition is **three independent axes**, each owned by a different external system, each carrying its own `observedAt`, and each absent until its owner has been observed at least once.
- Allowed meaning: The three axes are:

  | Axis | Owner | Answers |
  | --- | --- | --- |
  | `provisioning` | Provisioner (MAAS) | Can it be deployed, is a deployment running, did it fail; also power, ephemerality, lock, and commissioning/testing status |
  | `membership` | Kubernetes API, Slurm | Is it in a platform, in what role, is it draining |
  | `health` | Central TSDB (Prometheus) | Is it up |

  An axis that has never been observed is **absent, not defaulted**: "we do not know" must never be presentable as "we know it is bad". A provisioner being unreachable does not make its servers unhealthy, and a server with no metrics is not down — it may simply not be scraped yet. The normalized `provisioning.state` is the value to branch on (`new | commissioning | ready | allocated | deploying | deployed | releasing | testing | rescue | broken | failed | retired | unknown`); the provider's own label is display-only. The `health` axis is `up` or `down`, and null when unobserved.
- Disallowed meaning: A single collapsed value. Collapsing the three axes into one badge is disallowed: a server that is `deployed`, in no platform, and reporting no metrics is either a spare awaiting allocation or a broken host, and no rule can tell which. Do not use one axis to express another — provisioning state is not power state, membership is not health.
- Synonyms: None. "Health" refers specifically to the `health` axis, not to the whole of a server's condition.
- Deprecated terms: The closed six-value set `live | warning | error | maintain | offline | unknown` — a single operational status — is **superseded**. It predates the refoundation and never matched what the backend implements; the three axes replace it. A UI may still summarise the axes for a glance, but must not persist or filter on a single combined status.
- Examples: "A newly reconciled server has a `provisioning` axis but null `membership` and `health`, because no platform or metrics store has reported on it yet." / "Filter the server list by `provisioningState=deployed`; there is no single `status` filter."
- Related terms: Server（the entity these axes describe）, Server Lock（a provider-owned protection fact carried by the provisioning axis）, Alert（a different concept, read from Alertmanager）.
- Change note: Rewritten from the previous single closed six-value set to the three-axis model of [decision 002](../../../decisions/002-server-identity.md) and [decision 003](../../../decisions/003-metrics-label-contract.md), which is what `api-server` and `dashboard` implement.
